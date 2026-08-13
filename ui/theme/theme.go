package theme

import "github.com/charmbracelet/lipgloss"

// Raw palette — black, white, gray. Nothing else.
//
// An empty color is not black: lipgloss emits no escape for it, so the
// terminal's own background shows through untouched. Every full-width
// surface is empty on purpose — painting them would kill terminal
// transparency. Only small chrome (cursor row, status pills) is filled.
const (
	ColorSurface    = "" // transparent
	ColorSurfaceAlt = "" // transparent — no zebra stripe, it would need a fill
	ColorHeaderBG   = "" // transparent; headers carry weight via bold + white
	ColorHeaderFG   = "#FFFFFF"
	ColorInk        = "#000000" // text on filled (light) chrome
	ColorAccent     = "#FFFFFF"
	ColorAccentSoft = "#BFBFBF"
	// Severity is a brightness ladder, not a hue: danger is loudest.
	ColorSuccess = "#8A8A8A"
	ColorWarning = "#BFBFBF"
	ColorDanger  = "#FFFFFF"
	ColorBorder  = "#3A3A3A"
	ColorMuted   = "#8A8A8A"
	ColorFG      = "#E6E6E6"

	ColorSurfaceBright = "#303030" // cursor row / active row — the one fill
)

type Theme struct {
	Cell          lipgloss.Style
	AltCell       lipgloss.Style
	ActiveRowCell lipgloss.Style
	CursorCell    lipgloss.Style
	HeaderCell    lipgloss.Style
	PromptShell   lipgloss.Style
	HelpCard      lipgloss.Style
	StatusBar     lipgloss.Style
	StatusSegment lipgloss.Style
	Muted         lipgloss.Style
	Accent        lipgloss.Style
	Danger        lipgloss.Style
	Warning       lipgloss.Style
	Success       lipgloss.Style
}

func New() Theme {
	fg := lipgloss.Color(ColorFG)
	muted := lipgloss.Color(ColorMuted)
	surface := lipgloss.Color(ColorSurface)
	surfaceAlt := lipgloss.Color(ColorSurfaceAlt)
	surfaceBright := lipgloss.Color(ColorSurfaceBright)
	border := lipgloss.Color(ColorBorder)
	accent := lipgloss.Color(ColorAccent)
	accentSoft := lipgloss.Color(ColorAccentSoft)
	headerBG := lipgloss.Color(ColorHeaderBG)
	headerFG := lipgloss.Color(ColorHeaderFG)
	success := lipgloss.Color(ColorSuccess)
	warning := lipgloss.Color(ColorWarning)
	danger := lipgloss.Color(ColorDanger)

	cell := lipgloss.NewStyle().
		Foreground(fg).
		Background(surface).
		Padding(0, 1)

	altCell := lipgloss.NewStyle().
		Foreground(fg).
		Background(surfaceAlt).
		Padding(0, 1)

	activeRowCell := lipgloss.NewStyle().
		Foreground(fg).
		Background(surfaceBright).
		Padding(0, 1).
		Bold(false)

	cursorCell := lipgloss.NewStyle().
		Foreground(fg).
		Background(surfaceBright).
		Bold(true).
		Padding(0, 1)

	headerCell := lipgloss.NewStyle().
		Foreground(headerFG).
		Background(headerBG).
		Bold(true).
		Padding(0, 1)

	promptShell := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(accentSoft).
		Foreground(fg).
		Padding(0, 1)

	helpCard := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Foreground(fg).
		Background(surface).
		Padding(1, 2)

	statusBar := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderLeft(false).
		BorderRight(false).
		BorderBottom(false).
		BorderForeground(border).
		Foreground(fg)

	statusSegment := lipgloss.NewStyle().
		Foreground(fg).
		Padding(0, 1)

	mutedS := lipgloss.NewStyle().Foreground(muted)
	accentS := lipgloss.NewStyle().Foreground(accent)
	dangerS := lipgloss.NewStyle().Foreground(danger).Bold(true)
	warningS := lipgloss.NewStyle().Foreground(warning).Bold(true)
	successS := lipgloss.NewStyle().Foreground(success).Bold(true)

	return Theme{
		Cell:          cell,
		AltCell:       altCell,
		ActiveRowCell: activeRowCell,
		CursorCell:    cursorCell,
		HeaderCell:    headerCell,
		PromptShell:   promptShell,
		HelpCard:      helpCard,
		StatusBar:     statusBar,
		StatusSegment: statusSegment,
		Muted:         mutedS,
		Accent:        accentS,
		Danger:        dangerS,
		Warning:       warningS,
		Success:       successS,
	}
}

// Badge returns colored status-bar pill.
func (t Theme) Badge(label, fg, bg string) string {
	return t.StatusSegment.
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Bold(true).
		Padding(0, 1).
		Render(label)
}
