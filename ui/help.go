package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/ui/theme"
)

type HelpModel struct {
	theme  theme.Theme
	width  int
	height int
	for_   AppState
}

func NewHelp(t theme.Theme) HelpModel {
	return HelpModel{theme: t}
}

func (m *HelpModel) SetSize(w, h int) { m.width, m.height = w, h }
func (m *HelpModel) SetFor(s AppState) { m.for_ = s }

type helpEntry struct {
	keys string
	desc string
}

func (m HelpModel) entries() []helpEntry {
	common := []helpEntry{
		{"?", "toggle this help"},
		{"q / Ctrl+C", "quit"},
	}
	switch m.for_ {
	case StateInbox, StateSearch:
		return append([]helpEntry{
			{"↑/↓ or j/k", "move cursor"},
			{"h/l or ←/→", "move cursor column"},
			{"Enter", "open selected email"},
			{"c", "compose new message"},
			{"r", "reply to selected"},
			{"d", "trash selected"},
			{"R", "refresh inbox"},
			{"A", "mark all visible as read"},
			{"/", "search (Gmail query)"},
			{"Esc", "clear search"},
		}, common...)
	case StateReader:
		return append([]helpEntry{
			{"j/k or ↑/↓", "scroll body"},
			{"g / G", "top / bottom"},
			{"r", "reply"},
			{"d", "trash"},
			{"i", "open images in external viewer"},
			{"Esc", "back to inbox"},
		}, common...)
	case StateCompose:
		return append([]helpEntry{
			{"Tab / Shift+Tab", "cycle fields"},
			{"Ctrl+S", "send"},
			{"Ctrl+D", "discard"},
			{"Esc", "discard and return"},
		}, common...)
	}
	return common
}

func (m HelpModel) View(under string) string {
	if m.width <= 0 || m.height <= 0 {
		return under
	}

	entries := m.entries()
	keyW := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.keys); w > keyW {
			keyW = w
		}
	}

	keyStyle := m.theme.Accent.Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorFG))

	var rows []string
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorHeaderFG)).
		Background(lipgloss.Color(theme.ColorHeaderBG)).
		Bold(true).
		Padding(0, 1).
		Render("Keybindings")
	rows = append(rows, title, "")

	for _, e := range entries {
		k := keyStyle.Render(padRight(e.keys, keyW))
		rows = append(rows, k+"  "+descStyle.Render(e.desc))
	}
	rows = append(rows, "", m.theme.Muted.Render("Press ? or Esc to close."))

	card := m.theme.HelpCard.Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(theme.ColorBorder)),
	)
}

func padRight(s string, w int) string {
	if lipgloss.Width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}
