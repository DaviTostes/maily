package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	"github.com/davitostes/maily/gmail"
	"github.com/davitostes/maily/ui/theme"
)

var ansiEscRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type ReaderModel struct {
	theme    theme.Theme
	width    int
	height   int
	message  gmail.FullMessage
	vp       viewport.Model
	headerH  int
	ready    bool
	hasMsg   bool

	mdRenderer    *glamour.TermRenderer
	mdRenderWidth int
	cachedBody    string
	cachedID      string
	cachedWidth   int
}

func NewReader(t theme.Theme) ReaderModel {
	return ReaderModel{theme: t}
}

func (m *ReaderModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.layout()
	if m.hasMsg {
		m.vp.SetContent(m.renderBody())
	}
}

func (m *ReaderModel) SetMessage(msg gmail.FullMessage) {
	m.message = msg
	m.hasMsg = true
	m.layout()
	m.vp.SetContent(m.renderBody())
	m.vp.GotoTop()
}

func (m *ReaderModel) HasMessage() bool { return m.hasMsg }
func (m *ReaderModel) Message() gmail.FullMessage { return m.message }

func (m *ReaderModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	header := m.renderHeader()
	m.headerH = lipgloss.Height(header)
	bodyH := m.height - m.headerH - 1
	if bodyH < 3 {
		bodyH = 3
	}
	if !m.ready {
		m.vp = viewport.New(m.width, bodyH)
		m.ready = true
	} else {
		m.vp.Width = m.width
		m.vp.Height = bodyH
	}
	m.vp.Style = lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorFG)).
		Background(lipgloss.Color(theme.ColorSurface)).
		Padding(1, 0)
}

func (m *ReaderModel) Update(msg tea.Msg) tea.Cmd {
	if !m.ready {
		return nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return cmd
}

func (m *ReaderModel) GotoTop()    { m.vp.GotoTop() }
func (m *ReaderModel) GotoBottom() { m.vp.GotoBottom() }

func (m *ReaderModel) ScrollLines(n int) {
	if n > 0 {
		m.vp.ScrollDown(n)
	} else if n < 0 {
		m.vp.ScrollUp(-n)
	}
}

func (m ReaderModel) renderHeader() string {
	if !m.hasMsg {
		return m.theme.HeaderCell.Width(m.width).Render("No message")
	}
	subject := m.message.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	title := m.theme.HeaderCell.Width(m.width).Bold(true).Render("✉ " + subject)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorAccentSoft)).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorFG))

	fromLine := labelStyle.Render("From: ") + valueStyle.Render(m.message.From)
	toLine := labelStyle.Render("To:   ") + valueStyle.Render(m.message.To)
	dateLine := labelStyle.Render("Date: ") + valueStyle.Render(humanDate(m.message.Date))

	meta := lipgloss.JoinVertical(lipgloss.Left, fromLine, toLine, dateLine)
	metaBlock := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorSurfaceAlt)).
		Foreground(lipgloss.Color(theme.ColorFG)).
		Padding(1, 2).
		Width(m.width).
		Render(meta)

	var badgeStrs []string
	for _, a := range m.message.Attachments {
		badgeStrs = append(badgeStrs, m.theme.Badge("📎 "+a, theme.ColorBG, theme.ColorAccent))
	}
	if n := len(m.message.Images); n > 0 {
		badgeStrs = append(badgeStrs, m.theme.Badge(fmt.Sprintf("🖼 %d image(s) — press i", n), theme.ColorBG, theme.ColorAccentSoft))
	}
	var extras string
	if len(badgeStrs) > 0 {
		extras = lipgloss.NewStyle().
			Background(lipgloss.Color(theme.ColorSurface)).
			Padding(0, 2).
			Width(m.width).
			Render(strings.Join(badgeStrs, " "))
	}

	if extras != "" {
		return lipgloss.JoinVertical(lipgloss.Left, title, metaBlock, extras)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, metaBlock)
}

func (m *ReaderModel) renderBody() string {
	if !m.hasMsg {
		return ""
	}
	body := m.message.Body
	if strings.TrimSpace(body) == "" {
		body = "(empty body)"
	}
	fullW := m.vp.Width
	if fullW < 20 {
		fullW = 20
	}
	// Adaptive wrap: use full width on small terminals, cap at ~100 cols
	// on wide ones so lines stay readable instead of stretching edge-to-edge.
	innerW := fullW
	if innerW > 100 {
		innerW = 100
	}
	if m.cachedID == m.message.ID && m.cachedWidth == fullW && m.cachedBody != "" {
		return m.cachedBody
	}

	inner := m.renderMarkdown(body, innerW)
	rendered := lipgloss.PlaceHorizontal(fullW, lipgloss.Left, inner,
		lipgloss.WithWhitespaceBackground(lipgloss.Color(theme.ColorSurface)))
	m.cachedBody = rendered
	m.cachedID = m.message.ID
	m.cachedWidth = fullW
	return rendered
}

func (m *ReaderModel) renderMarkdown(body string, width int) string {
	if m.mdRenderer == nil || m.mdRenderWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(readerStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return padLinesBG(wrap(body, width), width)
		}
		m.mdRenderer = r
		m.mdRenderWidth = width
	}
	out, err := m.mdRenderer.Render(body)
	if err != nil {
		return bgBox(wrap(body, width), width)
	}
	plain := ansiEscRe.ReplaceAllString(out, "")
	return bgBox(strings.TrimRight(plain, "\n"), width)
}

// bgBox wraps content in a single lipgloss box with uniform bg across all
// lines — no text-shaped stripes. Content must be ANSI-free so inner resets
// don't clobber the outer bg mid-line.
func bgBox(s string, width int) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorSurface)).
		Foreground(lipgloss.Color(theme.ColorFG)).
		Width(width).
		Render(s)
}

// readerStyle clones glamour's dark style but zeros the Document margin and
// forces Document bg to our ColorSurface so the viewport surface stays uniform.
func readerStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	bg := theme.ColorSurface
	s.Document.Margin = &zero
	s.Document.BackgroundColor = &bg
	return s
}

// padLinesBG right-pads each line to `width` with bg-styled spaces so the
// viewport surface color paints the full content row, not just the glyphs.
func padLinesBG(s string, width int) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(theme.ColorSurface))
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		gap := width - lipgloss.Width(line)
		if gap > 0 {
			lines[i] = line + bg.Render(strings.Repeat(" ", gap))
		}
	}
	return strings.Join(lines, "\n")
}

func (m ReaderModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if !m.hasMsg {
		return m.theme.Muted.Render("No message loaded.")
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.vp.View())
}

// wrap wraps text to width w, preserving paragraph breaks.
func wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	var out strings.Builder
	for i, para := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(wrapLine(para, w))
	}
	return out.String()
}

func wrapLine(line string, w int) string {
	if lipgloss.Width(line) <= w {
		return line
	}
	var out strings.Builder
	words := strings.Fields(line)
	if len(words) == 0 {
		return line
	}
	cur := ""
	for _, word := range words {
		if cur == "" {
			cur = word
			continue
		}
		if lipgloss.Width(cur)+1+lipgloss.Width(word) > w {
			out.WriteString(cur)
			out.WriteByte('\n')
			cur = word
		} else {
			cur += " " + word
		}
	}
	if cur != "" {
		out.WriteString(cur)
	}
	return out.String()
}

var _ = fmt.Sprintf
