package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/gmail"
	"github.com/davitostes/maily/ui/theme"
)

type inboxColumn int

const (
	colFlags inboxColumn = iota
	colFrom
	colSubject
	colDate
)

const inboxColumnCount = 4

type Mailbox int

const (
	MailboxInbox Mailbox = iota
	MailboxSent
)

func (mb Mailbox) Label() string {
	switch mb {
	case MailboxSent:
		return "Sent"
	default:
		return "Inbox"
	}
}

func (mb Mailbox) QueryPrefix() string {
	switch mb {
	case MailboxSent:
		return "in:sent"
	default:
		return "in:inbox"
	}
}

type InboxModel struct {
	theme       theme.Theme
	width       int
	height      int
	messages    []gmail.MessageSummary
	cursorRow   int
	cursorCol   inboxColumn
	scrollTop   int
	search      textinput.Model
	searching   bool
	activeQuery string
	mailbox     Mailbox
}

func NewInbox(t theme.Theme) InboxModel {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "Gmail query (e.g. is:unread from:boss@corp.com)"
	ti.CharLimit = 512
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorAccent)).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorFG))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMuted))
	return InboxModel{
		theme:  t,
		search: ti,
	}
}

func (m *InboxModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.search.Width = w - 4
}

func (m *InboxModel) SetMessages(ms []gmail.MessageSummary) {
	m.messages = ms
	if m.cursorRow >= len(ms) {
		m.cursorRow = max0(len(ms) - 1)
	}
	m.scrollTop = 0
}

// MergeMessages replaces the list while keeping the cursor on the same
// message (by ID) and preserving scroll offset when possible.
func (m *InboxModel) MergeMessages(ms []gmail.MessageSummary) {
	var selID string
	if sel, ok := m.Selected(); ok {
		selID = sel.ID
	}
	m.messages = ms
	if selID != "" {
		for i := range ms {
			if ms[i].ID == selID {
				m.cursorRow = i
				return
			}
		}
	}
	if m.cursorRow >= len(ms) {
		m.cursorRow = max0(len(ms) - 1)
	}
}

func (m *InboxModel) Messages() []gmail.MessageSummary { return m.messages }
func (m *InboxModel) Selected() (gmail.MessageSummary, bool) {
	if m.cursorRow < 0 || m.cursorRow >= len(m.messages) {
		return gmail.MessageSummary{}, false
	}
	return m.messages[m.cursorRow], true
}
func (m *InboxModel) UnreadIDs() []string {
	var ids []string
	for _, msg := range m.messages {
		if !msg.IsRead {
			ids = append(ids, msg.ID)
		}
	}
	return ids
}

func (m *InboxModel) MarkAllReadLocal(ids []string) {
	if len(ids) == 0 {
		return
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	for i := range m.messages {
		if _, ok := set[m.messages[i].ID]; ok {
			m.messages[i].IsRead = true
		}
	}
}

func (m *InboxModel) RemoveSelected() {
	if m.cursorRow < 0 || m.cursorRow >= len(m.messages) {
		return
	}
	m.messages = append(m.messages[:m.cursorRow], m.messages[m.cursorRow+1:]...)
	if m.cursorRow >= len(m.messages) {
		m.cursorRow = max0(len(m.messages) - 1)
	}
}

func (m *InboxModel) StartSearch() {
	m.searching = true
	m.search.SetValue(m.activeQuery)
	m.search.Focus()
}
func (m *InboxModel) CancelSearch() {
	m.searching = false
	m.search.Blur()
}
func (m *InboxModel) IsSearching() bool { return m.searching }
func (m *InboxModel) SearchValue() string {
	return strings.TrimSpace(m.search.Value())
}
func (m *InboxModel) CommitSearch() string {
	q := m.SearchValue()
	m.activeQuery = q
	m.searching = false
	m.search.Blur()
	return q
}
func (m *InboxModel) ActiveQuery() string { return m.activeQuery }
func (m *InboxModel) Mailbox() Mailbox    { return m.mailbox }
func (m *InboxModel) SetMailbox(mb Mailbox) {
	m.mailbox = mb
	m.cursorRow = 0
	m.scrollTop = 0
}
func (m *InboxModel) CycleMailbox(delta int) {
	const n = 2
	m.SetMailbox(Mailbox(((int(m.mailbox)+delta)%n + n) % n))
}
func (m *InboxModel) ClearQuery() {
	m.activeQuery = ""
	m.search.SetValue("")
}

func (m *InboxModel) UpdateSearch(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return cmd
}

func (m *InboxModel) MoveCursor(dr, dc int) {
	if len(m.messages) == 0 {
		return
	}
	m.cursorRow = clamp(m.cursorRow+dr, 0, len(m.messages)-1)
	m.cursorCol = inboxColumn(clamp(int(m.cursorCol)+dc, 0, inboxColumnCount-1))
	m.ensureCursorVisible()
}

func (m *InboxModel) ensureCursorVisible() {
	visible := m.visibleRows()
	if visible <= 0 {
		return
	}
	if m.cursorRow < m.scrollTop {
		m.scrollTop = m.cursorRow
	} else if m.cursorRow >= m.scrollTop+visible {
		m.scrollTop = m.cursorRow - visible + 1
	}
	if m.scrollTop < 0 {
		m.scrollTop = 0
	}
}

func (m InboxModel) visibleRows() int {
	// tab row + header row + search shell + bottom padding
	reserved := 3
	if m.searching || m.activeQuery != "" {
		reserved += 3
	}
	v := m.height - reserved
	if v < 1 {
		v = 1
	}
	return v
}

// column widths sum to width
func (m InboxModel) columnWidths() (fromW, subjW, dateW, flagW int) {
	total := m.width
	if total < 40 {
		total = 40
	}
	fromW = 22
	dateW = 14
	flagW = 1
	// account for 2 gaps (1 space each); no gap after flag column
	subjW = total - fromW - dateW - flagW - 2
	if subjW < 10 {
		subjW = 10
	}
	return
}

func (m InboxModel) renderTabs() string {
	tabs := []Mailbox{MailboxInbox, MailboxSent}
	active := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorInk)).
		Background(lipgloss.Color(theme.ColorAccent)).
		Bold(true).
		Padding(0, 2)
	idle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorMuted)).
		Padding(0, 2)
	var cells []string
	for _, mb := range tabs {
		label := mb.Label()
		if mb == m.mailbox {
			cells = append(cells, active.Render(label))
		} else {
			cells = append(cells, idle.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

func (m InboxModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	fromW, subjW, dateW, flagW := m.columnWidths()

	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	fromLabel := "FROM"
	if m.mailbox == MailboxSent {
		fromLabel = "TO"
	}
	// Header
	header := m.theme.HeaderCell.Width(flagW).Render("") + joinCells(
		m.theme.HeaderCell.Width(fromW).Render(fromLabel),
		m.theme.HeaderCell.Width(subjW).Render("SUBJECT"),
		m.theme.HeaderCell.Width(dateW).Render("DATE"),
	)
	b.WriteString(header)
	b.WriteString("\n")

	visible := m.visibleRows()
	end := m.scrollTop + visible
	if end > len(m.messages) {
		end = len(m.messages)
	}

	for i := m.scrollTop; i < end; i++ {
		msg := m.messages[i]
		isCursorRow := i == m.cursorRow

		personRaw := msg.From
		if m.mailbox == MailboxSent {
			personRaw = msg.To
		}
		from := truncate(displayFrom(personRaw), fromW-2)
		subj := truncate(displaySubject(msg), subjW-2)
		date := truncate(humanDate(msg.Date), dateW-2)
		flags := " "
		switch {
		case isCursorRow:
			flags = "▌"
		case msg.IsStarred:
			flags = "*"
		case msg.IsImportant:
			flags = "!"
		case !msg.IsRead:
			flags = "•"
		case msg.HasAttachment:
			flags = "@"
		}

		flagCell := m.renderCell(flags, flagW, i, colFlags, isCursorRow, msg)
		rest := joinCells(
			m.renderCell(from, fromW, i, colFrom, isCursorRow, msg),
			m.renderCell(subj, subjW, i, colSubject, isCursorRow, msg),
			m.renderCell(date, dateW, i, colDate, isCursorRow, msg),
		)
		b.WriteString(flagCell + rest)
		b.WriteString("\n")
	}

	// pad remaining lines
	for i := end - m.scrollTop; i < visible; i++ {
		blank := m.theme.Cell.Width(m.width).Render(" ")
		b.WriteString(blank)
		b.WriteString("\n")
	}

	// search / query footer
	if m.searching {
		b.WriteString("\n")
		b.WriteString(m.theme.PromptShell.Width(m.width - 2).Render(m.search.View()))
	} else if m.activeQuery != "" {
		b.WriteString("\n")
		label := m.theme.Muted.Render("query: ") + m.theme.Accent.Render(m.activeQuery) + m.theme.Muted.Render("  (Esc to clear)")
		b.WriteString(m.theme.PromptShell.Width(m.width - 2).Render(label))
	}

	return b.String()
}

func (m InboxModel) renderCell(text string, w int, row int, col inboxColumn, isCursorRow bool, msg gmail.MessageSummary) string {
	// base style: selected row > zebra
	var style lipgloss.Style
	switch {
	case isCursorRow:
		style = m.theme.CursorCell.Width(w)
	case row%2 == 0:
		style = m.theme.Cell.Width(w)
	default:
		style = m.theme.AltCell.Width(w)
	}

	// emphasize unread / muted on From column (only when not selected, so the
	// selection style stays consistent across the whole row)
	if !isCursorRow && col == colFrom {
		if !msg.IsRead {
			style = style.Foreground(lipgloss.Color(theme.ColorAccentSoft)).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color(theme.ColorMuted))
		}
	}
	if col == colFlags {
		switch {
		case isCursorRow && hasAnyFlag(msg):
			style = style.Foreground(lipgloss.Color(theme.ColorAccent)).Bold(true)
		case isCursorRow:
			style = style.Foreground(lipgloss.Color(theme.ColorFG)).Bold(true)
		case hasAnyFlag(msg):
			style = style.Foreground(lipgloss.Color(theme.ColorAccent)).Bold(true)
		}
	}

	return style.Render(text)
}

func joinCells(cells ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, interleave(cells, " ")...)
}

func interleave(cells []string, sep string) []string {
	if len(cells) == 0 {
		return cells
	}
	out := make([]string, 0, 2*len(cells)-1)
	for i, c := range cells {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, c)
	}
	return out
}

func displayFrom(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(unknown)"
	}
	if idx := strings.Index(raw, "<"); idx > 0 {
		name := strings.TrimSpace(strings.Trim(raw[:idx], `"`))
		if name != "" {
			return name
		}
	}
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		return strings.Trim(raw, "<>")
	}
	return raw
}

func displaySubject(m gmail.MessageSummary) string {
	s := strings.TrimSpace(m.Subject)
	if s == "" {
		s = "(no subject)"
	}
	if m.Snippet != "" {
		s = fmt.Sprintf("%s  —  %s", s, m.Snippet)
	}
	return s
}

func humanDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	now := time.Now()
	if sameDay(t, now) {
		return t.Local().Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Local().Format("Jan 02")
	}
	return t.Local().Format("2006-01-02")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func hasAnyFlag(m gmail.MessageSummary) bool {
	return m.IsStarred || m.IsImportant || !m.IsRead || m.HasAttachment
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	// simple rune truncate
	runes := []rune(s)
	out := make([]rune, 0, w)
	used := 0
	for _, r := range runes {
		rw := lipgloss.Width(string(r))
		if used+rw > w-1 {
			break
		}
		out = append(out, r)
		used += rw
	}
	return string(out) + "…"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
