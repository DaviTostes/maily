package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/gmail"
	"github.com/davitostes/maily/ui/theme"
)

type composeField int

const (
	fieldTo composeField = iota
	fieldSubject
	fieldBody
	composeFieldCount
)

type ComposeModel struct {
	theme      theme.Theme
	width      int
	height     int
	to         textinput.Model
	subject    textinput.Model
	body       string
	bodyH      int
	focus      composeField
	inReplyTo  string
	references string
	threadID   string
	replyCtx   bool
}

func NewCompose(t theme.Theme) ComposeModel {
	mkInput := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 1024
		ti.Prompt = ""
		ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorFG))
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMuted))
		return ti
	}

	m := ComposeModel{
		theme:   t,
		to:      mkInput("recipient@example.com"),
		subject: mkInput("Subject"),
		focus:   fieldTo,
	}
	m.refreshFocus()
	return m
}

func (m *ComposeModel) Reset() {
	m.to.SetValue("")
	m.subject.SetValue("")
	m.body = ""
	m.inReplyTo = ""
	m.references = ""
	m.threadID = ""
	m.replyCtx = false
	m.focus = fieldTo
	m.refreshFocus()
}

// PrepareNew resets to an empty new message.
func (m *ComposeModel) PrepareNew() {
	m.Reset()
}

// PrepareReply populates fields for replying to src.
func (m *ComposeModel) PrepareReply(src gmail.FullMessage) {
	m.Reset()
	m.to.SetValue(src.From)
	subj := src.Subject
	if !strings.HasPrefix(strings.ToLower(subj), "re:") {
		subj = "Re: " + subj
	}
	m.subject.SetValue(subj)
	quoted := quoteBody(src.Body, src.From)
	m.body = "\n\n" + quoted
	m.inReplyTo = src.MessageID
	refs := strings.TrimSpace(src.References)
	if refs == "" {
		m.references = src.MessageID
	} else {
		m.references = refs + " " + src.MessageID
	}
	m.threadID = src.ThreadID
	m.replyCtx = true
	m.focus = fieldBody
	m.refreshFocus()
}

func quoteBody(body, from string) string {
	if body == "" {
		return ""
	}
	from = strings.TrimSpace(from)
	prefix := "> "
	var out strings.Builder
	out.WriteString("On previous message from ")
	out.WriteString(from)
	out.WriteString(":\n")
	for _, line := range strings.Split(body, "\n") {
		out.WriteString(prefix)
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func (m *ComposeModel) SetSize(w, h int) {
	m.width, m.height = w, h
	inputW := w - 4
	if inputW < 20 {
		inputW = 20
	}
	m.to.Width = inputW
	m.subject.Width = inputW
	bodyH := h - 10
	if bodyH < 5 {
		bodyH = 5
	}
	m.bodyH = bodyH
}

func (m *ComposeModel) CycleFocus(delta int) {
	n := int(composeFieldCount)
	m.focus = composeField(((int(m.focus) + delta) % n + n) % n)
	m.refreshFocus()
}

func (m *ComposeModel) refreshFocus() {
	m.to.Blur()
	m.subject.Blur()
	switch m.focus {
	case fieldTo:
		m.to.Focus()
	case fieldSubject:
		m.subject.Focus()
	}
}

func (m *ComposeModel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.focus {
	case fieldTo:
		m.to, cmd = m.to.Update(msg)
	case fieldSubject:
		m.subject, cmd = m.subject.Update(msg)
	}
	return cmd
}

func (m *ComposeModel) SetBody(s string) { m.body = s }
func (m ComposeModel) BodyFocused() bool { return m.focus == fieldBody }

func (m ComposeModel) To() string         { return strings.TrimSpace(m.to.Value()) }
func (m ComposeModel) Subject() string    { return strings.TrimSpace(m.subject.Value()) }
func (m ComposeModel) Body() string       { return m.body }
func (m ComposeModel) InReplyTo() string  { return m.inReplyTo }
func (m ComposeModel) References() string { return m.references }
func (m ComposeModel) ThreadID() string   { return m.threadID }

func (m ComposeModel) Validate() error {
	if m.To() == "" {
		return fmt.Errorf("recipient required")
	}
	if !strings.Contains(m.To(), "@") {
		return fmt.Errorf("recipient must be an email address")
	}
	return nil
}

func (m ComposeModel) bodyView() string {
	if strings.TrimSpace(m.body) == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorMuted)).
			Render("(empty — press w to edit in $EDITOR)")
	}
	lines := strings.Split(m.body, "\n")
	if m.bodyH > 0 && len(lines) > m.bodyH {
		lines = lines[:m.bodyH]
		lines = append(lines, lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorMuted)).
			Render("…"))
	}
	return strings.Join(lines, "\n")
}

func (m ComposeModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	title := "New Message"
	if m.replyCtx {
		title = "Reply: " + m.subject.Value()
	}
	header := m.theme.HeaderCell.Width(m.width).Render(title)

	field := func(label string, content string, focused bool) string {
		labelStyle := m.theme.Accent.Bold(true)
		if !focused {
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMuted)).Bold(true)
		}

		borderColor := theme.ColorBorder
		if focused {
			borderColor = theme.ColorAccent
		}
		box := lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderColor)).
			Foreground(lipgloss.Color(theme.ColorFG)).
			Padding(0, 1).
			Width(m.width - 4).
			Render(content)

		return lipgloss.JoinVertical(lipgloss.Left,
			labelStyle.Render(label),
			box,
		)
	}

	bodyLabel := "Body"
	if m.focus == fieldBody {
		bodyLabel = "Body  (w: edit in $EDITOR)"
	}

	toBox := field("To", m.to.View(), m.focus == fieldTo)
	subjBox := field("Subject", m.subject.View(), m.focus == fieldSubject)
	bodyBox := field(bodyLabel, m.bodyView(), m.focus == fieldBody)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		toBox,
		"",
		subjBox,
		"",
		bodyBox,
	)
}
