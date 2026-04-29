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

type VisualMode int

const (
	VisualNone VisualMode = iota
	VisualChar
	VisualLine
)

type ReaderModel struct {
	theme   theme.Theme
	width   int
	height  int
	message gmail.FullMessage
	vp      viewport.Model
	headerH int
	ready   bool
	hasMsg  bool

	mdRenderer    *glamour.TermRenderer
	mdRenderWidth int
	cachedPlain   string
	cachedID      string
	cachedWidth   int

	bodyLines  []string
	bodyWidth  int
	cursorLine int
	cursorCol  int
	vmode      VisualMode
	anchorLine int
	anchorCol  int
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
	m.cursorLine, m.cursorCol = 0, 0
	m.vmode = VisualNone
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
	innerW := fullW
	if innerW > 100 {
		innerW = 100
	}

	if !(m.cachedID == m.message.ID && m.cachedWidth == fullW && m.cachedPlain != "") {
		m.cachedPlain = m.renderMarkdown(body, innerW)
		m.cachedID = m.message.ID
		m.cachedWidth = fullW
	}

	m.bodyLines = strings.Split(m.cachedPlain, "\n")
	m.bodyWidth = fullW
	m.clampCursor()
	return m.composeView(fullW)
}

func (m *ReaderModel) renderMarkdown(body string, width int) string {
	if m.mdRenderer == nil || m.mdRenderWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(readerStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return wrap(body, width)
		}
		m.mdRenderer = r
		m.mdRenderWidth = width
	}
	out, err := m.mdRenderer.Render(body)
	if err != nil {
		return wrap(body, width)
	}
	plain := ansiEscRe.ReplaceAllString(out, "")
	return strings.TrimRight(plain, "\n")
}

func (m *ReaderModel) clampCursor() {
	if len(m.bodyLines) == 0 {
		m.cursorLine, m.cursorCol = 0, 0
		return
	}
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}
	if m.cursorLine >= len(m.bodyLines) {
		m.cursorLine = len(m.bodyLines) - 1
	}
	n := utf8RuneCount(m.bodyLines[m.cursorLine])
	if m.cursorCol < 0 {
		m.cursorCol = 0
	}
	if m.cursorCol > n {
		m.cursorCol = n
	}
}

func utf8RuneCount(s string) int { return len([]rune(s)) }

func (m *ReaderModel) normalizedSel() (sL, sC, eL, eC int) {
	aL, aC := m.anchorLine, m.anchorCol
	cL, cC := m.cursorLine, m.cursorCol
	if aL < cL || (aL == cL && aC <= cC) {
		return aL, aC, cL, cC
	}
	return cL, cC, aL, aC
}

func (m *ReaderModel) selRangeForLine(idx, lineLen, fullW int) (start, end int, has bool) {
	if m.vmode == VisualNone {
		return 0, 0, false
	}
	sL, sC, eL, eC := m.normalizedSel()
	if idx < sL || idx > eL {
		return 0, 0, false
	}
	if m.vmode == VisualLine {
		return 0, fullW, true
	}
	s, e := 0, lineLen
	if idx == sL {
		s = sC
	}
	if idx == eL {
		e = eC + 1
	}
	if s < 0 {
		s = 0
	}
	if e > fullW {
		e = fullW
	}
	if s > fullW {
		s = fullW
	}
	if s > e {
		s = e
	}
	return s, e, true
}

type cellStyle int

const (
	cellBase cellStyle = iota
	cellSel
	cellCursor
)

func (m *ReaderModel) composeView(fullW int) string {
	base := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorSurface)).
		Foreground(lipgloss.Color(theme.ColorFG))
	sel := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorAccentSoft)).
		Foreground(lipgloss.Color(theme.ColorBG))
	cur := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorAccent)).
		Foreground(lipgloss.Color(theme.ColorBG))

	out := make([]string, len(m.bodyLines))
	for i, line := range m.bodyLines {
		runes := []rune(line)
		n := len(runes)
		selS, selE, hasSel := m.selRangeForLine(i, n, fullW)
		isCursorLine := i == m.cursorLine

		styleAt := func(col int) cellStyle {
			if isCursorLine && col == m.cursorCol {
				return cellCursor
			}
			if hasSel && col >= selS && col < selE {
				return cellSel
			}
			return cellBase
		}

		var sb strings.Builder
		col := 0
		for col < fullW {
			s := styleAt(col)
			j := col + 1
			for j < fullW && styleAt(j) == s {
				j++
			}
			var chunk strings.Builder
			for k := col; k < j; k++ {
				if k < n {
					chunk.WriteRune(runes[k])
				} else {
					chunk.WriteByte(' ')
				}
			}
			var styled string
			switch s {
			case cellCursor:
				styled = cur.Render(chunk.String())
			case cellSel:
				styled = sel.Render(chunk.String())
			default:
				styled = base.Render(chunk.String())
			}
			sb.WriteString(styled)
			col = j
		}
		out[i] = sb.String()
	}
	return strings.Join(out, "\n")
}

func (m *ReaderModel) refresh() {
	if !m.hasMsg || !m.ready {
		return
	}
	m.vp.SetContent(m.renderBody())
}

func (m *ReaderModel) ensureCursorVisible() {
	top := m.vp.YOffset
	bot := top + m.vp.Height - 1
	if m.cursorLine < top {
		m.vp.SetYOffset(m.cursorLine)
	} else if m.cursorLine > bot {
		off := m.cursorLine - m.vp.Height + 1
		if off < 0 {
			off = 0
		}
		m.vp.SetYOffset(off)
	}
}

func (m *ReaderModel) MoveCursor(dl, dc int) {
	if !m.hasMsg {
		return
	}
	m.cursorLine += dl
	m.cursorCol += dc
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}
	if len(m.bodyLines) > 0 && m.cursorLine >= len(m.bodyLines) {
		m.cursorLine = len(m.bodyLines) - 1
	}
	if m.cursorCol < 0 {
		m.cursorCol = 0
	}
	if len(m.bodyLines) > 0 {
		n := utf8RuneCount(m.bodyLines[m.cursorLine])
		if m.cursorCol > n {
			m.cursorCol = n
		}
	}
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) GotoLineStart() {
	m.cursorCol = 0
	m.refresh()
}

func (m *ReaderModel) GotoLineEnd() {
	if len(m.bodyLines) == 0 {
		return
	}
	m.cursorCol = utf8RuneCount(m.bodyLines[m.cursorLine])
	m.refresh()
}

func (m *ReaderModel) CursorTop() {
	m.cursorLine, m.cursorCol = 0, 0
	m.vp.GotoTop()
	m.refresh()
}

func (m *ReaderModel) CursorBottom() {
	if len(m.bodyLines) > 0 {
		m.cursorLine = len(m.bodyLines) - 1
		m.cursorCol = 0
	}
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) StartVisual(line bool) {
	if !m.hasMsg {
		return
	}
	if line {
		m.vmode = VisualLine
	} else {
		m.vmode = VisualChar
	}
	m.anchorLine, m.anchorCol = m.cursorLine, m.cursorCol
	m.refresh()
}

func (m *ReaderModel) ExitVisual() {
	if m.vmode == VisualNone {
		return
	}
	m.vmode = VisualNone
	m.refresh()
}

func (m *ReaderModel) VisualMode() VisualMode { return m.vmode }

func (m *ReaderModel) Yank() (string, bool) {
	if len(m.bodyLines) == 0 {
		return "", false
	}
	if m.vmode == VisualNone {
		return m.bodyLines[m.cursorLine], true
	}
	sL, sC, eL, eC := m.normalizedSel()
	if m.vmode == VisualLine {
		end := eL + 1
		if end > len(m.bodyLines) {
			end = len(m.bodyLines)
		}
		return strings.Join(m.bodyLines[sL:end], "\n"), true
	}
	if sL == eL {
		runes := []rune(m.bodyLines[sL])
		n := len(runes)
		s, e := sC, eC+1
		if s > n {
			s = n
		}
		if e > n {
			e = n
		}
		if s > e {
			s = e
		}
		return string(runes[s:e]), true
	}
	var sb strings.Builder
	first := []rune(m.bodyLines[sL])
	if sC > len(first) {
		sC = len(first)
	}
	sb.WriteString(string(first[sC:]))
	for i := sL + 1; i < eL; i++ {
		sb.WriteByte('\n')
		sb.WriteString(m.bodyLines[i])
	}
	last := []rune(m.bodyLines[eL])
	e := eC + 1
	if e > len(last) {
		e = len(last)
	}
	sb.WriteByte('\n')
	sb.WriteString(string(last[:e]))
	return sb.String(), true
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
