package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/gmail"
	"github.com/davitostes/maily/ui/theme"
)

func wlCopy(s string) error {
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func openExternal(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

type AppState int

const (
	StateInbox AppState = iota
	StateReader
	StateCompose
	StateSearch
	StateHelp
)

// Msgs

type inboxLoadedMsg struct {
	mailbox  Mailbox
	messages []gmail.MessageSummary
	err      error
}

type messageLoadedMsg struct {
	msg gmail.FullMessage
	err error
}

type sendResultMsg struct {
	err error
}

type trashResultMsg struct {
	id  string
	err error
}

type clearStatusMsg struct{}

type markAllReadDoneMsg struct {
	ids []string
	err error
}

type pollTickMsg struct{}

type pollResultMsg struct {
	mailbox  Mailbox
	messages []gmail.MessageSummary
	err      error
}

type mailboxBucket struct {
	messages []gmail.MessageSummary
	seen     map[string]bool
	loaded   bool
}

const pollInterval = 15 * time.Second

type Model struct {
	state     AppState
	prevState AppState

	inbox     InboxModel
	reader    ReaderModel
	compose   ComposeModel
	statusbar StatusBarModel
	help      HelpModel

	theme  theme.Theme
	width  int
	height int

	loading  bool
	sending  bool
	spinner  spinner.Model
	client   *gmail.Client
	ctx      context.Context
	email    string
	lastErr  error
	lastHint string

	buckets     [2]mailboxBucket
	pollStarted bool
}

func New(ctx context.Context, client *gmail.Client) Model {
	t := theme.New()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorAccentSoft)).Bold(true)

	email := ""
	if client != nil {
		email = client.Email()
	}

	m := Model{
		state:     StateInbox,
		theme:     t,
		inbox:     NewInbox(t),
		reader:    NewReader(t),
		compose:   NewCompose(t),
		statusbar: NewStatusBar(t),
		help:      NewHelp(t),
		spinner:   sp,
		client:    client,
		ctx:       ctx,
		email:     email,
		loading:   true,
	}
	m.statusbar.SetState(StateInbox)
	m.help.SetFor(StateInbox)
	m.updateHints()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadInbox(MailboxInbox, MailboxInbox.QueryPrefix()),
		m.loadInbox(MailboxSent, MailboxSent.QueryPrefix()),
	)
}

func (m *Model) propagateSize() {
	bodyH := m.height - 2 // status bar takes 2 rows (border + content)
	if bodyH < 3 {
		bodyH = 3
	}
	m.inbox.SetSize(m.width, bodyH)
	m.reader.SetSize(m.width, bodyH)
	m.compose.SetSize(m.width, bodyH)
	m.statusbar.SetWidth(m.width)
	m.help.SetSize(m.width, m.height)
}

func pollTickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollTickMsg{} })
}

func (m Model) pollFetch(mb Mailbox, query string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return pollResultMsg{mailbox: mb, err: fmt.Errorf("gmail client not initialized")}
		}
		msgs, err := m.client.ListInbox(50, query)
		return pollResultMsg{mailbox: mb, messages: msgs, err: err}
	}
}

func notifyNewMail(items []gmail.MessageSummary) {
	if len(items) == 0 {
		return
	}
	title := fmt.Sprintf("Maily: %d new email(s)", len(items))
	var bodyLines []string
	const maxShown = 3
	for i, it := range items {
		if i >= maxShown {
			break
		}
		from := it.From
		subj := it.Subject
		if subj == "" {
			subj = "(no subject)"
		}
		bodyLines = append(bodyLines, truncMsg(from, 40)+" — "+truncMsg(subj, 60))
	}
	if len(items) > maxShown {
		bodyLines = append(bodyLines, fmt.Sprintf("…and %d more", len(items)-maxShown))
	}
	cmd := exec.Command("notify-send", "-a", "maily", "-i", "mail-message-new", title, strings.Join(bodyLines, "\n"))
	_ = cmd.Start()
}

func (m Model) effectiveQuery() string {
	prefix := m.inbox.Mailbox().QueryPrefix()
	q := strings.TrimSpace(m.inbox.ActiveQuery())
	if q == "" {
		return prefix
	}
	return prefix + " " + q
}

func (m Model) loadInbox(mb Mailbox, query string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return inboxLoadedMsg{mailbox: mb, err: fmt.Errorf("gmail client not initialized")}
		}
		msgs, err := m.client.ListInbox(50, query)
		return inboxLoadedMsg{mailbox: mb, messages: msgs, err: err}
	}
}

func (m *Model) showCachedMailbox() {
	b := m.buckets[m.inbox.Mailbox()]
	m.inbox.SetMessages(b.messages)
	if b.loaded {
		m.statusbar.SetMessage(m.inbox.Mailbox().Label()+": "+fmt.Sprint(len(b.messages)), MsgSuccess)
	} else {
		m.statusbar.SetMessage("Loading "+m.inbox.Mailbox().Label()+"…", MsgWarning)
	}
}

func (m Model) queryFor(mb Mailbox) string {
	if mb == m.inbox.Mailbox() {
		return m.effectiveQuery()
	}
	return mb.QueryPrefix()
}

func (m Model) loadMessage(id string) tea.Cmd {
	return func() tea.Msg {
		full, err := m.client.GetMessage(id)
		if err == nil {
			_ = m.client.MarkRead(id)
		}
		return messageLoadedMsg{msg: full, err: err}
	}
}

func (m Model) sendCmd(to, subject, body, inReplyTo, references, threadID string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.SendMessage(to, subject, body, inReplyTo, references, threadID)
		return sendResultMsg{err: err}
	}
}

func (m Model) markAllReadCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.MarkAllRead(ids)
		return markAllReadDoneMsg{ids: ids, err: err}
	}
}

func (m Model) trashCmd(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.TrashMessage(id)
		return trashResultMsg{id: id, err: err}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m *Model) enter(state AppState) {
	if m.state != StateHelp {
		m.prevState = m.state
	}
	m.state = state
	m.statusbar.SetState(state)
	m.help.SetFor(state)
	m.updateHints()
}

func (m *Model) updateHints() {
	var h string
	switch m.state {
	case StateInbox:
		h = "↑/↓ move · Enter open · Tab switch tabs · c compose · r reply · d trash · A read all · R refresh · / search · ? help · q quit"
	case StateReader:
		h = "hjkl · w/b/e · v/V visual · viw · yy/yiw · o open link · g/G · r reply · d trash · I images · Esc · ?"
	case StateCompose:
		h = "Tab next · w edit body in $EDITOR · Ctrl+S send · Ctrl+D/Esc cancel"
	case StateSearch:
		h = "Enter run · Esc cancel"
	case StateHelp:
		h = "? / Esc close"
	}
	m.statusbar.SetHints(h)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.propagateSize()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading || m.sending {
			cmds = append(cmds, cmd)
		}

	case inboxLoadedMsg:
		b := &m.buckets[msg.mailbox]
		if msg.err != nil {
			if msg.mailbox == m.inbox.Mailbox() {
				m.loading = false
				m.lastErr = msg.err
				m.statusbar.SetMessage(truncMsg(msg.err.Error(), 60), MsgError)
				cmds = append(cmds, clearStatusAfter(4*time.Second))
			}
			break
		}
		b.messages = msg.messages
		b.loaded = true
		if b.seen == nil {
			b.seen = make(map[string]bool, len(msg.messages))
			for _, it := range msg.messages {
				b.seen[it.ID] = true
			}
		}
		if msg.mailbox == m.inbox.Mailbox() {
			m.loading = false
			m.inbox.SetMessages(msg.messages)
			m.statusbar.SetEmail(m.email)
			m.statusbar.SetMessage(fmt.Sprintf("Loaded %d", len(msg.messages)), MsgSuccess)
			cmds = append(cmds, clearStatusAfter(2*time.Second))
		}
		if !m.pollStarted {
			m.pollStarted = true
			cmds = append(cmds, pollTickCmd())
		}

	case pollTickMsg:
		// Always reschedule first so a transient error doesn't kill polling.
		cmds = append(cmds, pollTickCmd())
		// Skip refresh while editing/searching.
		if m.state == StateCompose || m.state == StateSearch {
			break
		}
		cmds = append(cmds,
			m.pollFetch(MailboxInbox, m.queryFor(MailboxInbox)),
			m.pollFetch(MailboxSent, m.queryFor(MailboxSent)),
		)

	case pollResultMsg:
		if msg.err != nil {
			break
		}
		b := &m.buckets[msg.mailbox]
		if b.seen == nil {
			b.seen = make(map[string]bool, len(msg.messages))
		}
		var fresh []gmail.MessageSummary
		for _, it := range msg.messages {
			if !b.seen[it.ID] {
				fresh = append(fresh, it)
			}
			b.seen[it.ID] = true
		}
		b.messages = msg.messages
		b.loaded = true
		if msg.mailbox == m.inbox.Mailbox() {
			m.inbox.MergeMessages(msg.messages)
		}
		if len(fresh) > 0 && msg.mailbox == MailboxInbox {
			notifyNewMail(fresh)
			m.statusbar.SetMessage(fmt.Sprintf("%d new email(s)", len(fresh)), MsgSuccess)
			cmds = append(cmds, clearStatusAfter(3*time.Second))
		}

	case messageLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.statusbar.SetMessage(truncMsg(msg.err.Error(), 60), MsgError)
			cmds = append(cmds, clearStatusAfter(4*time.Second))
			break
		}
		m.reader.SetMessage(msg.msg)
		// mark current inbox entry as read visually
		if sel, ok := m.inbox.Selected(); ok && sel.ID == msg.msg.ID {
			msgs := m.inbox.Messages()
			for i := range msgs {
				if msgs[i].ID == sel.ID {
					msgs[i].IsRead = true
				}
			}
			m.inbox.SetMessages(msgs)
		}
		m.enter(StateReader)

	case sendResultMsg:
		m.sending = false
		if msg.err != nil {
			m.statusbar.SetMessage("Send failed: "+truncMsg(msg.err.Error(), 50), MsgError)
			cmds = append(cmds, clearStatusAfter(5*time.Second))
			break
		}
		m.statusbar.SetMessage("Sent ✓", MsgSuccess)
		cmds = append(cmds, clearStatusAfter(2*time.Second))
		m.compose.Reset()
		m.enter(StateInbox)

	case trashResultMsg:
		if msg.err != nil {
			m.statusbar.SetMessage("Trash failed: "+truncMsg(msg.err.Error(), 50), MsgError)
			cmds = append(cmds, clearStatusAfter(4*time.Second))
			break
		}
		m.statusbar.SetMessage("Moved to trash", MsgSuccess)
		cmds = append(cmds, clearStatusAfter(2*time.Second))
		// remove from local inbox if present
		msgs := m.inbox.Messages()
		for i := range msgs {
			if msgs[i].ID == msg.id {
				msgs = append(msgs[:i], msgs[i+1:]...)
				break
			}
		}
		m.inbox.SetMessages(msgs)
		m.buckets[m.inbox.Mailbox()].messages = msgs
		if m.state == StateReader {
			m.enter(StateInbox)
		}

	case markAllReadDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.statusbar.SetMessage("Mark all read failed: "+truncMsg(msg.err.Error(), 50), MsgError)
			cmds = append(cmds, clearStatusAfter(4*time.Second))
			break
		}
		m.inbox.MarkAllReadLocal(msg.ids)
		m.statusbar.SetMessage(fmt.Sprintf("Marked %d as read", len(msg.ids)), MsgSuccess)
		cmds = append(cmds, clearStatusAfter(2*time.Second))

	case clearStatusMsg:
		m.statusbar.ClearMessage()

	case tea.KeyMsg:
		if cmd := m.handleKey(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// route sub-model updates for non-key messages too (viewport animations etc.)
	switch m.state {
	case StateReader:
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			if c := m.reader.Update(msg); c != nil {
				cmds = append(cmds, c)
			}
		}
	case StateCompose:
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			if c := m.compose.Update(msg); c != nil {
				cmds = append(cmds, c)
			}
		}
	case StateSearch:
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			if c := m.inbox.UpdateSearch(msg); c != nil {
				cmds = append(cmds, c)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Help overlay intercepts first
	if m.state == StateHelp {
		switch msg.String() {
		case "?", "esc", "q":
			m.state = m.prevState
			m.statusbar.SetState(m.state)
			m.help.SetFor(m.state)
			m.updateHints()
		}
		return nil
	}

	// Global: Ctrl+C always quits
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}

	switch m.state {
	case StateInbox:
		return m.handleInboxKey(msg)
	case StateSearch:
		return m.handleSearchKey(msg)
	case StateReader:
		return m.handleReaderKey(msg)
	case StateCompose:
		return m.handleComposeKey(msg)
	}
	return nil
}

func (m *Model) handleInboxKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q":
		return tea.Quit
	case "?":
		m.enter(StateHelp)
	case "j", "down":
		m.inbox.MoveCursor(1, 0)
	case "k", "up":
		m.inbox.MoveCursor(-1, 0)
	case "h", "left":
		m.inbox.MoveCursor(0, -1)
	case "l", "right":
		m.inbox.MoveCursor(0, 1)
	case "g":
		m.inbox.MoveCursor(-1<<30, 0)
	case "G":
		m.inbox.MoveCursor(1<<30, 0)
	case "enter":
		if sel, ok := m.inbox.Selected(); ok {
			m.loading = true
			m.statusbar.SetMessage("Loading…", MsgWarning)
			return tea.Batch(m.loadMessage(sel.ID), m.spinner.Tick)
		}
	case "c":
		m.compose.PrepareNew()
		m.enter(StateCompose)
	case "r":
		if sel, ok := m.inbox.Selected(); ok {
			// need full message for reply context; fetch then switch
			m.loading = true
			m.statusbar.SetMessage("Preparing reply…", MsgWarning)
			id := sel.ID
			return tea.Batch(func() tea.Msg {
				full, err := m.client.GetMessage(id)
				if err != nil {
					return messageLoadedMsg{err: err}
				}
				return replyLoadedMsg{msg: full}
			}, m.spinner.Tick)
		}
	case "d":
		if sel, ok := m.inbox.Selected(); ok {
			return m.trashCmd(sel.ID)
		}
	case "/":
		m.inbox.StartSearch()
		m.enter(StateSearch)
	case "esc":
		if m.inbox.ActiveQuery() != "" {
			m.inbox.ClearQuery()
			m.loading = true
			m.statusbar.SetMessage("Refreshing…", MsgWarning)
			return tea.Batch(m.loadInbox(m.inbox.Mailbox(), m.effectiveQuery()), m.spinner.Tick)
		}
	case "R":
		m.loading = true
		m.statusbar.SetMessage("Refreshing…", MsgWarning)
		return tea.Batch(m.loadInbox(m.inbox.Mailbox(), m.effectiveQuery()), m.spinner.Tick)
	case "tab":
		m.inbox.CycleMailbox(1)
		m.inbox.ClearQuery()
		m.showCachedMailbox()
	case "shift+tab":
		m.inbox.CycleMailbox(-1)
		m.inbox.ClearQuery()
		m.showCachedMailbox()
	case "A":
		ids := m.inbox.UnreadIDs()
		if len(ids) == 0 {
			m.statusbar.SetMessage("Nothing unread", MsgWarning)
			return clearStatusAfter(2 * time.Second)
		}
		m.loading = true
		m.statusbar.SetMessage(fmt.Sprintf("Marking %d as read…", len(ids)), MsgWarning)
		return tea.Batch(m.markAllReadCmd(ids), m.spinner.Tick)
	}
	return nil
}

type replyLoadedMsg struct {
	msg gmail.FullMessage
}

type editBodyDoneMsg struct {
	body string
	err  error
}

func openEditorCmd(initial string) tea.Cmd {
	f, err := os.CreateTemp("", "maily-*.md")
	if err != nil {
		return func() tea.Msg { return editBodyDoneMsg{err: err} }
	}
	path := f.Name()
	if initial != "" {
		_, _ = f.WriteString(initial)
	}
	_ = f.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editBodyDoneMsg{err: err}
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return editBodyDoneMsg{err: rerr}
		}
		return editBodyDoneMsg{body: string(data)}
	})
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.inbox.CancelSearch()
		m.enter(StateInbox)
		return nil
	case tea.KeyEnter:
		_ = m.inbox.CommitSearch()
		m.enter(StateInbox)
		m.loading = true
		m.statusbar.SetMessage("Searching…", MsgWarning)
		return tea.Batch(m.loadInbox(m.inbox.Mailbox(), m.effectiveQuery()), m.spinner.Tick)
	}
	return m.inbox.UpdateSearch(msg)
}

func (m *Model) handleReaderKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Multi-key sequences (operator/text-object pending).
	if p := m.reader.Pending(); p != "" {
		return m.handleReaderPending(p, key)
	}

	switch key {
	case "q":
		m.enter(StateInbox)
		return nil
	case "esc":
		if m.reader.VisualMode() != VisualNone {
			m.reader.ExitVisual()
			return nil
		}
		m.enter(StateInbox)
		return nil
	case "?":
		m.enter(StateHelp)
		return nil
	case "j", "down":
		m.reader.MoveCursor(1, 0)
		return nil
	case "k", "up":
		m.reader.MoveCursor(-1, 0)
		return nil
	case "h", "left":
		m.reader.MoveCursor(0, -1)
		return nil
	case "l", "right":
		m.reader.MoveCursor(0, 1)
		return nil
	case "pgdown", " ":
		m.reader.MoveCursor(10, 0)
		return nil
	case "pgup":
		m.reader.MoveCursor(-10, 0)
		return nil
	case "ctrl+d":
		m.reader.MoveCursor(10, 0)
		return nil
	case "ctrl+u":
		m.reader.MoveCursor(-10, 0)
		return nil
	case "0", "home":
		m.reader.GotoLineStart()
		return nil
	case "$", "end":
		m.reader.GotoLineEnd()
		return nil
	case "g":
		m.reader.CursorTop()
		return nil
	case "G":
		m.reader.CursorBottom()
		return nil
	case "w":
		m.reader.WordForward(false)
		return nil
	case "W":
		m.reader.WordForward(true)
		return nil
	case "b":
		m.reader.WordBackward(false)
		return nil
	case "B":
		m.reader.WordBackward(true)
		return nil
	case "e":
		m.reader.WordEnd(false)
		return nil
	case "E":
		m.reader.WordEnd(true)
		return nil
	case "v":
		m.reader.StartVisual(false)
		return nil
	case "V":
		m.reader.StartVisual(true)
		return nil
	case "i", "a":
		// Text-object selection inside visual mode.
		if m.reader.VisualMode() != VisualNone {
			m.reader.SetPending(key)
		}
		return nil
	case "y":
		if m.reader.VisualMode() != VisualNone {
			return m.yankAndCopy(false)
		}
		// Operator-pending: wait for next key (yy, yiw, yaw, …).
		m.reader.SetPending("y")
		return nil
	case "r":
		if m.reader.HasMessage() {
			m.compose.PrepareReply(m.reader.Message())
			m.enter(StateCompose)
			return openEditorCmd(m.compose.Body())
		}
		return nil
	case "d":
		if m.reader.HasMessage() {
			return m.trashCmd(m.reader.Message().ID)
		}
		return nil
	case "o":
		url := m.reader.LinkUnderCursor()
		if url == "" {
			m.statusbar.SetMessage("No link under cursor", MsgWarning)
			return clearStatusAfter(2 * time.Second)
		}
		openExternal(url)
		m.statusbar.SetMessage("Opened "+truncMsg(url, 60), MsgSuccess)
		return clearStatusAfter(2 * time.Second)
	case "I":
		if !m.reader.HasMessage() {
			return nil
		}
		urls := m.reader.Message().Images
		if len(urls) == 0 {
			m.statusbar.SetMessage("No images in this message", MsgWarning)
			return clearStatusAfter(2 * time.Second)
		}
		const maxOpen = 5
		n := len(urls)
		if n > maxOpen {
			n = maxOpen
		}
		for _, u := range urls[:n] {
			openExternal(u)
		}
		extra := ""
		if len(urls) > maxOpen {
			extra = fmt.Sprintf(" (of %d, capped at %d)", len(urls), maxOpen)
		}
		m.statusbar.SetMessage(fmt.Sprintf("Opened %d image(s)%s", n, extra), MsgSuccess)
		return clearStatusAfter(2 * time.Second)
	}
	return m.reader.Update(msg)
}

func (m *Model) yankAndCopy(exitVisual bool) tea.Cmd {
	text, ok := m.reader.Yank()
	if !ok {
		return nil
	}
	if err := wlCopy(text); err != nil {
		m.statusbar.SetMessage("Copy failed: "+truncMsg(err.Error(), 50), MsgError)
		return clearStatusAfter(4 * time.Second)
	}
	if exitVisual || m.reader.VisualMode() != VisualNone {
		m.reader.ExitVisual()
	}
	m.statusbar.SetMessage(fmt.Sprintf("Yanked %d bytes", len(text)), MsgSuccess)
	return clearStatusAfter(2 * time.Second)
}

// applyTextObject sets a visual selection over the requested object.
// Returns true if the noun was recognized.
func (m *Model) applyTextObject(noun string, around bool) bool {
	switch noun {
	case "w":
		m.reader.SelectWord(false, around)
	case "W":
		m.reader.SelectWord(true, around)
	case "\"":
		m.reader.SelectQuote('"', around)
	case "'":
		m.reader.SelectQuote('\'', around)
	case "`":
		m.reader.SelectQuote('`', around)
	case "(", ")", "b":
		m.reader.SelectBracket('(', ')', around)
	case "[", "]":
		m.reader.SelectBracket('[', ']', around)
	case "{", "}", "B":
		m.reader.SelectBracket('{', '}', around)
	case "p":
		m.reader.SelectParagraph(around)
	default:
		return false
	}
	return true
}

func (m *Model) handleReaderPending(p, key string) tea.Cmd {
	switch p {
	case "y":
		m.reader.ClearPending()
		switch key {
		case "y":
			// yy — yank current line
			text, ok := m.reader.YankLine()
			if !ok {
				return nil
			}
			if err := wlCopy(text); err != nil {
				m.statusbar.SetMessage("Copy failed: "+truncMsg(err.Error(), 50), MsgError)
				return clearStatusAfter(4 * time.Second)
			}
			m.statusbar.SetMessage(fmt.Sprintf("Yanked %d bytes", len(text)), MsgSuccess)
			return clearStatusAfter(2 * time.Second)
		case "i":
			m.reader.SetPending("yi")
			return nil
		case "a":
			m.reader.SetPending("ya")
			return nil
		case "esc":
			return nil
		}
		return nil
	case "yi", "ya":
		m.reader.ClearPending()
		around := p == "ya"
		if key == "esc" {
			return nil
		}
		hadVisual := m.reader.VisualMode() != VisualNone
		if !hadVisual {
			m.reader.StartVisual(false)
		}
		if !m.applyTextObject(key, around) {
			if !hadVisual {
				m.reader.ExitVisual()
			}
			return nil
		}
		return m.yankAndCopy(true)
	case "i", "a":
		m.reader.ClearPending()
		if key == "esc" {
			return nil
		}
		around := p == "a"
		if m.reader.VisualMode() == VisualNone {
			m.reader.StartVisual(false)
		}
		m.applyTextObject(key, around)
		return nil
	}
	m.reader.ClearPending()
	return nil
}

func (m *Model) handleComposeKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlD:
		m.compose.Reset()
		m.enter(StateInbox)
		return nil
	case tea.KeyCtrlS:
		if err := m.compose.Validate(); err != nil {
			m.statusbar.SetMessage(err.Error(), MsgError)
			return clearStatusAfter(3 * time.Second)
		}
		m.sending = true
		m.statusbar.SetMessage("Sending…", MsgWarning)
		return tea.Batch(
			m.sendCmd(m.compose.To(), m.compose.Subject(), m.compose.Body(), m.compose.InReplyTo(), m.compose.References(), m.compose.ThreadID()),
			m.spinner.Tick,
		)
	case tea.KeyTab:
		m.compose.CycleFocus(1)
		return nil
	case tea.KeyShiftTab:
		m.compose.CycleFocus(-1)
		return nil
	}
	if m.compose.BodyFocused() && msg.String() == "w" {
		return openEditorCmd(m.compose.Body())
	}
	// special-case "?" toggles help only if not in a text field? keep help explicit via other means.
	return m.compose.Update(msg)
}

// Special routing: replyLoadedMsg handled above
func (m Model) handleReplyLoaded(rl replyLoadedMsg) Model {
	m.loading = false
	m.compose.PrepareReply(rl.msg)
	m.enter(StateCompose)
	return m
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "initializing…"
	}

	var body string
	switch m.state {
	case StateInbox, StateSearch:
		if m.loading && len(m.inbox.Messages()) == 0 {
			body = m.renderInitOverlay()
			break
		}
		body = m.inbox.View()
	case StateReader:
		body = m.reader.View()
	case StateCompose:
		body = m.compose.View()
	case StateHelp:
		// render previous-view underneath, overlayed with help card
		var under string
		switch m.prevState {
		case StateReader:
			under = m.reader.View()
		case StateCompose:
			under = m.compose.View()
		default:
			under = m.inbox.View()
		}
		body = m.help.View(under)
	default:
		body = m.inbox.View()
	}

	// Pad / trim body area to height-2
	body = fitHeight(body, m.height-2, m.width)

	status := m.statusbar.View()
	if m.loading || m.sending {
		// inject spinner into status bar's hints area
		spin := m.spinner.View()
		status = injectSpinner(status, spin)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, status)
}

func (m Model) renderInitOverlay() string {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	label := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorAccentSoft)).
		Bold(true).
		Render("Loading inbox…")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorMuted)).
		Render("fetching messages from Gmail")
	block := lipgloss.JoinVertical(lipgloss.Center, m.spinner.View()+" "+label, "", hint)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, block,
		lipgloss.WithWhitespaceChars(" "))
}

func fitHeight(s string, h, w int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	// pad short lines (no-op visually but consistent)
	_ = w
	return strings.Join(lines, "\n")
}

func injectSpinner(status, spin string) string {
	// put spinner at start; statusbar already has padding
	return strings.Replace(status, " ", spin+" ", 1)
}

func truncMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Public Update override to handle replyLoadedMsg
// (extended Update via custom msg routing)
// Go doesn't allow us to override Update easily with methods, so inject in main Update:
var _ = (*Model)(nil)

// Intercept replyLoadedMsg — add to Update switch:
// Implemented via type-assert inside Update below. We re-expose through a wrapper.

// Wrap the main Update to also handle replyLoadedMsg.
func (m Model) updateExtra(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if rl, ok := msg.(replyLoadedMsg); ok {
		m.loading = false
		m.compose.PrepareReply(rl.msg)
		m.enter(StateCompose)
		return m, openEditorCmd(m.compose.Body()), true
	}
	if eb, ok := msg.(editBodyDoneMsg); ok {
		if eb.err != nil {
			m.statusbar.SetMessage("Editor: "+truncMsg(eb.err.Error(), 50), MsgError)
			return m, clearStatusAfter(4 * time.Second), true
		}
		m.compose.SetBody(eb.body)
		return m, nil, true
	}
	return m, nil, false
}

// Root returns a wrapped Model that handles custom msgs first.
type rootModel struct{ Model }

func Root(m Model) tea.Model { return rootModel{m} }

func (r rootModel) Init() tea.Cmd { return r.Model.Init() }

func (r rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := r.Model.updateExtra(msg); handled {
		r.Model = newM.(Model)
		return r, cmd
	}
	nm, cmd := r.Model.Update(msg)
	r.Model = nm.(Model)
	return r, cmd
}

func (r rootModel) View() string { return r.Model.View() }
