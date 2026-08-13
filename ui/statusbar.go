package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/ui/theme"
)

type StatusBarModel struct {
	theme   theme.Theme
	width   int
	state   AppState
	email   string
	hints   string
	message string // transient toast (error/success)
	level   MessageLevel
}

type MessageLevel int

const (
	MsgNone MessageLevel = iota
	MsgInfo
	MsgSuccess
	MsgWarning
	MsgError
)

func NewStatusBar(t theme.Theme) StatusBarModel {
	return StatusBarModel{theme: t}
}

func (m *StatusBarModel) SetWidth(w int)               { m.width = w }
func (m *StatusBarModel) SetState(s AppState)          { m.state = s }
func (m *StatusBarModel) SetEmail(e string)            { m.email = e }
func (m *StatusBarModel) SetHints(h string)            { m.hints = h }
func (m *StatusBarModel) SetMessage(s string, l MessageLevel) {
	m.message = s
	m.level = l
}
func (m *StatusBarModel) ClearMessage() {
	m.message = ""
	m.level = MsgNone
}

func (m StatusBarModel) modeBadge() string {
	switch m.state {
	case StateInbox:
		return m.theme.Badge("INBOX", theme.ColorInk, theme.ColorAccent)
	case StateReader:
		return m.theme.Badge("READ", theme.ColorInk, theme.ColorAccentSoft)
	case StateCompose:
		return m.theme.Badge("COMPOSE", theme.ColorInk, theme.ColorWarning)
	case StateSearch:
		return m.theme.Badge("SEARCH", theme.ColorInk, theme.ColorSuccess)
	case StateHelp:
		return m.theme.Badge("HELP", theme.ColorInk, theme.ColorAccentSoft)
	}
	return m.theme.Badge("?", theme.ColorInk, theme.ColorMuted)
}

func (m StatusBarModel) messageBadge() string {
	if m.message == "" {
		return ""
	}
	switch m.level {
	case MsgError:
		return m.theme.Badge(m.message, theme.ColorInk, theme.ColorDanger)
	case MsgSuccess:
		return m.theme.Badge(m.message, theme.ColorInk, theme.ColorSuccess)
	case MsgWarning:
		return m.theme.Badge(m.message, theme.ColorInk, theme.ColorWarning)
	default:
		return m.theme.Badge(m.message, theme.ColorFG, theme.ColorBorder)
	}
}

func (m StatusBarModel) View() string {
	if m.width <= 0 {
		return ""
	}
	mode := m.modeBadge()
	account := ""
	if m.email != "" {
		account = m.theme.Badge(m.email, theme.ColorFG, theme.ColorBorder)
	}
	toast := m.messageBadge()

	left := lipgloss.JoinHorizontal(lipgloss.Top, mode, " ", account)
	if toast != "" {
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", toast)
	}

	right := m.theme.Muted.Render(m.hints)

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return m.theme.StatusBar.Width(m.width).Render(line)
}
