package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html/charset"
	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type MessageSummary struct {
	ID            string
	ThreadID      string
	From          string
	To            string
	Subject       string
	Date          time.Time
	IsRead        bool
	IsImportant   bool
	IsStarred     bool
	HasAttachment bool
	Snippet       string
}

type FullMessage struct {
	MessageSummary
	To          string
	MessageID   string // RFC 822 Message-Id header value
	References  string
	Body        string
	PlainText   bool // Body is the sender's text/plain part, not converted HTML
	Attachments []string
	Images      []string // remote http(s) <img src> URLs from HTML body
}

type Client struct {
	svc   *gmailapi.Service
	email string
}

// New builds a Gmail client given an authorized *http.Client.
func New(ctx context.Context, httpClient *http.Client) (*Client, error) {
	svc, err := gmailapi.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("gmail service: %w", err)
	}
	profile, err := svc.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &Client{svc: svc, email: profile.EmailAddress}, nil
}

// Email returns the authenticated user's address.
func (c *Client) Email() string { return c.email }

// ListInbox returns summaries for up to maxResults messages matching query.
// Query defaults to "in:inbox" when empty.
func (c *Client) ListInbox(maxResults int64, query string) ([]MessageSummary, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := strings.TrimSpace(query)
	if q == "" {
		q = "in:inbox"
	}

	list, err := c.svc.Users.Messages.List("me").
		Q(q).
		MaxResults(maxResults).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	const parallelism = 15
	results := make([]*MessageSummary, len(list.Messages))
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup

	for i, stub := range list.Messages {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			msg, err := c.svc.Users.Messages.Get("me", id).
				Format("metadata").
				MetadataHeaders("From", "To", "Subject", "Date").
				Do()
			if err != nil {
				return
			}
			s := summaryFromMessage(msg)
			results[i] = &s
		}(i, stub.Id)
	}
	wg.Wait()

	out := make([]MessageSummary, 0, len(results))
	for _, r := range results {
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, nil
}

func summaryFromMessage(msg *gmailapi.Message) MessageSummary {
	s := MessageSummary{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Snippet:  msg.Snippet,
		IsRead:      !hasLabel(msg.LabelIds, "UNREAD"),
		IsImportant: hasLabel(msg.LabelIds, "IMPORTANT"),
		IsStarred:   hasLabel(msg.LabelIds, "STARRED"),
	}
	if msg.Payload != nil {
		s.From = decodeHeader(headerValue(msg.Payload.Headers, "From"))
		s.To = decodeHeader(headerValue(msg.Payload.Headers, "To"))
		s.Subject = decodeHeader(headerValue(msg.Payload.Headers, "Subject"))
		if d := headerValue(msg.Payload.Headers, "Date"); d != "" {
			if t, err := mail.ParseDate(d); err == nil {
				s.Date = t
			}
		}
		s.HasAttachment = payloadHasAttachment(msg.Payload)
	}
	if s.Date.IsZero() && msg.InternalDate > 0 {
		s.Date = time.UnixMilli(msg.InternalDate)
	}
	return s
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func headerValue(hs []*gmailapi.MessagePartHeader, name string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

func payloadHasAttachment(p *gmailapi.MessagePart) bool {
	if p == nil {
		return false
	}
	if p.Filename != "" && p.Body != nil && (p.Body.AttachmentId != "" || p.Body.Size > 0) {
		return true
	}
	for _, sub := range p.Parts {
		if payloadHasAttachment(sub) {
			return true
		}
	}
	return false
}

// GetMessage fetches a full message.
func (c *Client) GetMessage(id string) (FullMessage, error) {
	msg, err := c.svc.Users.Messages.Get("me", id).Format("full").Do()
	if err != nil {
		return FullMessage{}, fmt.Errorf("get message: %w", err)
	}
	full := FullMessage{MessageSummary: summaryFromMessage(msg)}
	if msg.Payload != nil {
		full.To = decodeHeader(headerValue(msg.Payload.Headers, "To"))
		full.MessageID = headerValue(msg.Payload.Headers, "Message-Id")
		full.References = headerValue(msg.Payload.Headers, "References")

		var markdown string
		if html := pickBodyPart(msg.Payload, "text/html"); html != "" {
			markdown = htmlToMarkdown(html)
			full.Images = extractImageURLs(html)
		}
		plain := pickBodyPart(msg.Payload, "text/plain")
		if preferPlain(plain, markdown) {
			full.Body, full.PlainText = normalizeNewlines(plain), true
		} else {
			full.Body = normalizeNewlines(markdown)
		}
		full.Attachments = collectAttachments(msg.Payload)
	}
	return full, nil
}

func pickBodyPart(p *gmailapi.MessagePart, mimeType string) string {
	if p == nil {
		return ""
	}
	if strings.EqualFold(p.MimeType, mimeType) && p.Body != nil && p.Body.Data != "" {
		data, err := decodeBody(p.Body.Data)
		if err != nil {
			return ""
		}
		return toUTF8(data, headerValue(p.Headers, "Content-Type"))
	}
	for _, sub := range p.Parts {
		if got := pickBodyPart(sub, mimeType); got != "" {
			return got
		}
	}
	return ""
}

var urlWordRe = regexp.MustCompile(`\S*(?:https?://|www\.)\S*`)

// visibleWords counts words a reader would actually see: URLs are dropped so a
// wall of tracking links can't make a stub look substantial.
func visibleWords(s string) int {
	return len(strings.Fields(urlWordRe.ReplaceAllString(s, " ")))
}

// preferPlain decides whether to show the sender's own text/plain part instead
// of the HTML converted to markdown. Terminal mail clients traditionally
// prefer text/plain, and it is the only rendering that survives table-based
// layouts: converting those gives one cell per line, while the sender's plain
// part keeps the columns they aligned by hand.
//
// The exception is the stub plain part ("view this message in your browser"),
// so plain has to carry at least half the words of the HTML to win. That ratio
// is the knob to turn if some mail picks the wrong side.
func preferPlain(plain, markdown string) bool {
	if strings.TrimSpace(plain) == "" {
		return false
	}
	if strings.TrimSpace(markdown) == "" {
		return true
	}
	return visibleWords(plain)*2 >= visibleWords(markdown)
}

// decodeBody handles the base64 variants Gmail has been seen to emit: URL-safe
// (documented), standard, and either without padding.
func decodeBody(s string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.URLEncoding, base64.RawURLEncoding,
		base64.StdEncoding, base64.RawStdEncoding,
	}
	var err error
	for _, enc := range encodings {
		var data []byte
		if data, err = enc.DecodeString(s); err == nil {
			return data, nil
		}
	}
	return nil, err
}

// toUTF8 transcodes a part body to UTF-8. Gmail hands back the original bytes
// untouched, so a windows-1252 or ISO-8859-1 mail is mojibake until converted.
// contentType is the part's raw Content-Type header; charset.NewReader also
// falls back to a BOM or an HTML <meta charset> when it carries no charset.
func toUTF8(data []byte, contentType string) string {
	r, err := charset.NewReader(bytes.NewReader(data), contentType)
	if err != nil {
		return string(data)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return string(data)
	}
	return string(out)
}

func collectAttachments(p *gmailapi.MessagePart) []string {
	var out []string
	var walk func(part *gmailapi.MessagePart)
	walk = func(part *gmailapi.MessagePart) {
		if part == nil {
			return
		}
		if part.Filename != "" {
			out = append(out, part.Filename)
		}
		for _, sub := range part.Parts {
			walk(sub)
		}
	}
	walk(p)
	return out
}

var (
	tagRe     = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	anyTagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
	spacesRe  = regexp.MustCompile(`[ \t]+`)
	blankLine = regexp.MustCompile(`\n{3,}`)
)

func htmlToMarkdown(s string) string {
	if s == "" {
		return ""
	}
	md, err := htmltomarkdown.ConvertString(s)
	if err != nil || strings.TrimSpace(md) == "" {
		return stripHTML(s)
	}
	return cleanupMarkdown(md)
}

var imgSrcRe = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

// extractImageURLs pulls remote http(s) image URLs from raw HTML, skipping
// data: URIs and de-duplicating while preserving order.
func extractImageURLs(html string) []string {
	matches := imgSrcRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		u := strings.TrimSpace(m[1])
		if u == "" || strings.HasPrefix(strings.ToLower(u), "data:") {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

var (
	mdImageRe        = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdEmptyLinkRe    = regexp.MustCompile(`\[\s*\]\([^)]*\)`)
	mdLinkRe         = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdEmptyParenRe   = regexp.MustCompile(`\(\s*\)`)
	mdEmptyBracketRe = regexp.MustCompile(`\[\s*\]`)
	mdManyBlanksRe   = regexp.MustCompile(`\n{3,}`)
)

// cleanupMarkdown strips image noise typical of marketing HTML mail and
// collapses dead artifacts. Markdown links [text](url) are preserved so the
// reader can render them as clickable hyperlinks.
func cleanupMarkdown(s string) string {
	s = mdImageRe.ReplaceAllString(s, "")
	s = mdEmptyLinkRe.ReplaceAllString(s, "")
	// Drop self-referential links where text == url (visual noise, no extra info).
	s = mdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdLinkRe.FindStringSubmatch(m)
		text := strings.TrimSpace(sub[1])
		url := strings.TrimSpace(sub[2])
		if text == "" {
			return ""
		}
		if text == url {
			return text
		}
		return m
	})
	s = mdEmptyParenRe.ReplaceAllString(s, "")
	s = mdEmptyBracketRe.ReplaceAllString(s, "")
	s = mdManyBlanksRe.ReplaceAllString(s, "\n\n")
	return s
}

func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = tagRe.ReplaceAllString(s, "")
	s = anyTagRe.ReplaceAllString(s, "")
	r := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	s = r.Replace(s)
	s = spacesRe.ReplaceAllString(s, " ")
	s = blankLine.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// SendMessage sends a plaintext email. If inReplyTo is non-empty, adds
// In-Reply-To and References headers; threadID groups the message into an
// existing Gmail thread so the reply shows up as a conversation, not a new one.
func (c *Client) SendMessage(to, subject, body, inReplyTo, references, threadID string) error {
	var buf strings.Builder
	fmt.Fprintf(&buf, "From: %s\r\n", c.email)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", encodeHeaderIfNeeded(subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: 8bit\r\n")
	if inReplyTo != "" {
		fmt.Fprintf(&buf, "In-Reply-To: %s\r\n", inReplyTo)
		refs := strings.TrimSpace(references)
		if refs == "" {
			refs = inReplyTo
		}
		fmt.Fprintf(&buf, "References: %s\r\n", refs)
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)

	raw := base64.URLEncoding.EncodeToString([]byte(buf.String()))
	msg := &gmailapi.Message{Raw: raw}
	if threadID != "" {
		msg.ThreadId = threadID
	}
	_, err := c.svc.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

func encodeHeaderIfNeeded(s string) string {
	for _, r := range s {
		if r > 127 {
			return mime.BEncoding.Encode("UTF-8", s)
		}
	}
	return s
}

// TrashMessage moves a message to the trash.
func (c *Client) TrashMessage(id string) error {
	_, err := c.svc.Users.Messages.Trash("me", id).Do()
	if err != nil {
		return fmt.Errorf("trash: %w", err)
	}
	return nil
}

// MarkRead clears the UNREAD label.
func (c *Client) MarkRead(id string) error {
	_, err := c.svc.Users.Messages.Modify("me", id, &gmailapi.ModifyMessageRequest{
		RemoveLabelIds: []string{"UNREAD"},
	}).Do()
	return err
}

// MarkAllRead clears the UNREAD label on every given message ID in one batch.
func (c *Client) MarkAllRead(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return c.svc.Users.Messages.BatchModify("me", &gmailapi.BatchModifyMessagesRequest{
		Ids:            ids,
		RemoveLabelIds: []string{"UNREAD"},
	}).Do()
}
