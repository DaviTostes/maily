package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/ui/theme"
)

// Every rendered body line must occupy exactly fullW cells. Wide runes (emoji,
// CJK) count as two, so a rune-counting pad overflows the terminal and wraps,
// which is what garbled mail with emoji in it.
func TestComposeViewLineWidth(t *testing.T) {
	const fullW = 40

	lines := []string{
		"plain ascii line",
		"emoji 🎉🎉🎉 in the middle",
		"日本語のテキスト行",
		"",
		strings.Repeat("x", fullW+10),   // longer than the viewport
		strings.Repeat("🎉", fullW/2+3), // all wide, overflows on a rune count
		"combining é accent",
	}

	m := NewReader(theme.New())
	m.bodyLines = lines
	m.lineLinks = make([][]linkRange, len(lines))

	for _, cursorLine := range []int{0, 1, 5} {
		m.cursorLine, m.cursorCol = cursorLine, 2
		for i, got := range strings.Split(m.composeView(fullW), "\n") {
			if w := lipgloss.Width(got); w != fullW {
				t.Errorf("cursor on line %d: rendered line %d is %d cells, want %d (source %q)",
					cursorLine, i, w, fullW, lines[i])
			}
		}
	}
}
