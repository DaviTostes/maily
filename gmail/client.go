package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type MessageSummary struct {
	ID            string
	ThreadID      string
	From          string
	Subject       string
	Date          time.Time
	IsRead        bool
	HasAttachment bool
	Snippet       string
}

type FullMessage struct {
	MessageSummary
	To          string
	MessageID   string // RFC 822 Message-Id header value
	References  string
	Body        string
	Attachments []string
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

	out := make([]MessageSummary, 0, len(list.Messages))
	for _, stub := range list.Messages {
		msg, err := c.svc.Users.Messages.Get("me", stub.Id).
			Format("metadata").
			MetadataHeaders("From", "Subject", "Date").
			Do()
		if err != nil {
			continue
		}
		out = append(out, summaryFromMessage(msg))
	}
	return out, nil
}

func summaryFromMessage(msg *gmailapi.Message) MessageSummary {
	s := MessageSummary{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Snippet:  msg.Snippet,
		IsRead:   !hasLabel(msg.LabelIds, "UNREAD"),
	}
	if msg.Payload != nil {
		s.From = decodeHeader(headerValue(msg.Payload.Headers, "From"))
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

		plain := pickBodyPart(msg.Payload, "text/plain")
		if plain == "" {
			html := pickBodyPart(msg.Payload, "text/html")
			plain = stripHTML(html)
		}
		full.Body = normalizeNewlines(plain)
		full.Attachments = collectAttachments(msg.Payload)
	}
	return full, nil
}

func pickBodyPart(p *gmailapi.MessagePart, mimeType string) string {
	if p == nil {
		return ""
	}
	if strings.EqualFold(p.MimeType, mimeType) && p.Body != nil && p.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(p.Body.Data)
		if err != nil {
			data, err = base64.StdEncoding.DecodeString(p.Body.Data)
			if err != nil {
				return ""
			}
		}
		return string(data)
	}
	for _, sub := range p.Parts {
		if got := pickBodyPart(sub, mimeType); got != "" {
			return got
		}
	}
	return ""
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
// In-Reply-To and References headers.
func (c *Client) SendMessage(to, subject, body, inReplyTo string) error {
	var buf strings.Builder
	fmt.Fprintf(&buf, "From: %s\r\n", c.email)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", encodeHeaderIfNeeded(subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: 8bit\r\n")
	if inReplyTo != "" {
		fmt.Fprintf(&buf, "In-Reply-To: %s\r\n", inReplyTo)
		fmt.Fprintf(&buf, "References: %s\r\n", inReplyTo)
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)

	raw := base64.URLEncoding.EncodeToString([]byte(buf.String()))
	msg := &gmailapi.Message{Raw: raw}
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
