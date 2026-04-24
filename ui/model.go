package ui

import (
	"context"
	"fmt"
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
	return tea.Batch(m.spinner.Tick, m.loadInbox(""))
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

func (m Model) loadInbox(query string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return inboxLoadedMsg{err: fmt.Errorf("gmail client not initialized")}
		}
		msgs, err := m.client.ListInbox(50, query)
		return inboxLoadedMsg{messages: msgs, err: err}
	}
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

func (m Model) sendCmd(to, subject, body, inReplyTo string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.SendMessage(to, subject, body, inReplyTo)
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
		h = "↑/↓ move · Enter open · c compose · r reply · d trash · A read all · R refresh · / search · ? help · q quit"
	case StateReader:
		h = "j/k scroll · r reply · d trash · i images · Esc back · ? help"
	case StateCompose:
		h = "Tab next · Ctrl+S send · Ctrl+D/Esc cancel"
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
		m.loading = false
		if msg.err != nil {
			m.lastErr = msg.err
			m.statusbar.SetMessage(truncMsg(msg.err.Error(), 60), MsgError)
			cmds = append(cmds, clearStatusAfter(4*time.Second))
		} else {
			m.inbox.SetMessages(msg.messages)
			m.statusbar.SetEmail(m.email)
			m.statusbar.SetMessage(fmt.Sprintf("Loaded %d", len(msg.messages)), MsgSuccess)
			cmds = append(cmds, clearStatusAfter(2*time.Second))
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
			return tea.Batch(m.loadInbox(""), m.spinner.Tick)
		}
	case "R":
		m.loading = true
		m.statusbar.SetMessage("Refreshing…", MsgWarning)
		return tea.Batch(m.loadInbox(m.inbox.ActiveQuery()), m.spinner.Tick)
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

func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.inbox.CancelSearch()
		m.enter(StateInbox)
		return nil
	case tea.KeyEnter:
		q := m.inbox.CommitSearch()
		m.enter(StateInbox)
		m.loading = true
		m.statusbar.SetMessage("Searching…", MsgWarning)
		return tea.Batch(m.loadInbox(q), m.spinner.Tick)
	}
	return m.inbox.UpdateSearch(msg)
}

func (m *Model) handleReaderKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "esc":
		m.enter(StateInbox)
		return nil
	case "?":
		m.enter(StateHelp)
		return nil
	case "j", "down":
		m.reader.ScrollLines(1)
		return nil
	case "k", "up":
		m.reader.ScrollLines(-1)
		return nil
	case "pgdown", " ":
		m.reader.ScrollLines(10)
		return nil
	case "pgup":
		m.reader.ScrollLines(-10)
		return nil
	case "g":
		m.reader.GotoTop()
		return nil
	case "G":
		m.reader.GotoBottom()
		return nil
	case "r":
		if m.reader.HasMessage() {
			m.compose.PrepareReply(m.reader.Message())
			m.enter(StateCompose)
		}
		return nil
	case "d":
		if m.reader.HasMessage() {
			return m.trashCmd(m.reader.Message().ID)
		}
		return nil
	case "i":
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
			m.sendCmd(m.compose.To(), m.compose.Subject(), m.compose.Body(), m.compose.InReplyTo()),
			m.spinner.Tick,
		)
	case tea.KeyTab:
		m.compose.CycleFocus(1)
		return nil
	case tea.KeyShiftTab:
		m.compose.CycleFocus(-1)
		return nil
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
