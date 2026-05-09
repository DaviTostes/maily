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

// URL regex: matches http/https/ftp/mailto. Trailing punctuation trimmed below.
var urlRe = regexp.MustCompile(`(?:https?|ftp|mailto):[^\s<>"\x60]+`)

// Markdown inline link: [text](url). Bracketed link refs and angle-URL forms ignored.
var mdLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

type linkRange struct {
	start int
	end   int
	url   string
}

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
	lineLinks  [][]linkRange
	bodyWidth  int
	cursorLine int
	cursorCol  int
	vmode      VisualMode
	anchorLine int
	anchorCol  int

	pending string
}

func (m *ReaderModel) Pending() string    { return m.pending }
func (m *ReaderModel) SetPending(p string) { m.pending = p }
func (m *ReaderModel) ClearPending()       { m.pending = "" }

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

	// Extract markdown inline links and replace with their text so glamour
	// doesn't render the URL inline (where it gets ugly-wrapped).
	mdLinks, stripped := stripMarkdownLinks(body)

	if !(m.cachedID == m.message.ID && m.cachedWidth == fullW && m.cachedPlain != "") {
		m.cachedPlain = m.renderMarkdown(stripped, innerW)
		m.cachedID = m.message.ID
		m.cachedWidth = fullW
	}

	m.bodyLines = strings.Split(m.cachedPlain, "\n")
	m.lineLinks = make([][]linkRange, len(m.bodyLines))
	for i, line := range m.bodyLines {
		m.lineLinks[i] = extractLinks(line)
	}
	mapMarkdownLinks(mdLinks, m.bodyLines, m.lineLinks)
	m.bodyWidth = fullW
	m.clampCursor()
	return m.composeView(fullW)
}

type mdLinkInfo struct {
	text string
	url  string
}

// stripMarkdownLinks extracts inline [text](url) links from raw markdown in
// order and returns the body with each replaced by just its text.
func stripMarkdownLinks(raw string) ([]mdLinkInfo, string) {
	var links []mdLinkInfo
	stripped := mdLinkRe.ReplaceAllStringFunc(raw, func(m string) string {
		sub := mdLinkRe.FindStringSubmatch(m)
		text, url := sub[1], sub[2]
		links = append(links, mdLinkInfo{text: text, url: url})
		return text
	})
	return links, stripped
}

// mapMarkdownLinks locates each link's text within the rendered plain lines
// and records matching link ranges. Whitespace between words is flexible to
// handle wrap reflow.
func mapMarkdownLinks(links []mdLinkInfo, lines []string, lineLinks [][]linkRange) {
	if len(links) == 0 {
		return
	}
	// global rune buffer of the plain rendering, plus a map from rune index → (line, col).
	type pos struct{ line, col int }
	var posMap []pos
	var sb strings.Builder
	for li, line := range lines {
		for ci, r := range []rune(line) {
			posMap = append(posMap, pos{li, ci})
			sb.WriteRune(r)
		}
		// represent newline as a single space-equivalent so \s+ matchers cross lines.
		if li < len(lines)-1 {
			posMap = append(posMap, pos{li, -1})
			sb.WriteRune('\n')
		}
	}
	hayRunes := []rune(sb.String())
	hay := string(hayRunes)
	cursor := 0 // rune offset

	for _, link := range links {
		text, url := link.text, link.url
		words := strings.Fields(text)
		if len(words) == 0 {
			continue
		}
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = regexp.QuoteMeta(w)
		}
		pat := strings.Join(parts, `\s+`)
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		// Search byte-wise on hay starting at the byte offset matching cursor runes.
		byteCursor := len(string(hayRunes[:cursor]))
		loc := re.FindStringIndex(hay[byteCursor:])
		if loc == nil {
			continue
		}
		startByte := byteCursor + loc[0]
		endByte := byteCursor + loc[1]
		startRune := utf8RuneCount(hay[:startByte])
		endRune := utf8RuneCount(hay[:endByte])
		cursor = endRune

		// Emit per-line ranges; newline placeholders (col == -1) split segments.
		curLine, curStart, curEnd := -1, -1, -1
		flush := func() {
			if curLine >= 0 {
				lineLinks[curLine] = append(lineLinks[curLine], linkRange{
					start: curStart, end: curEnd, url: url,
				})
			}
			curLine, curStart, curEnd = -1, -1, -1
		}
		for i := startRune; i < endRune; i++ {
			p := posMap[i]
			if p.col < 0 {
				flush()
				continue
			}
			if curLine != p.line {
				flush()
				curLine = p.line
				curStart = p.col
			}
			curEnd = p.col + 1
		}
		flush()
	}
}

// extractLinks returns rune-indexed link ranges within line.
func extractLinks(line string) []linkRange {
	matches := urlRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]linkRange, 0, len(matches))
	for _, m := range matches {
		raw := line[m[0]:m[1]]
		// trim trailing punctuation that's likely sentence terminator
		trimmed := strings.TrimRight(raw, ".,;:!?)]}>")
		end := m[0] + len(trimmed)
		// convert byte indices to rune indices
		startRune := utf8RuneCount(line[:m[0]])
		endRune := utf8RuneCount(line[:end])
		if endRune <= startRune {
			continue
		}
		out = append(out, linkRange{start: startRune, end: endRune, url: trimmed})
	}
	return out
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
	link := lipgloss.NewStyle().
		Background(lipgloss.Color(theme.ColorSurface)).
		Foreground(lipgloss.Color(theme.ColorAccentSoft)).
		Underline(true)

	out := make([]string, len(m.bodyLines))
	for i, line := range m.bodyLines {
		runes := []rune(line)
		n := len(runes)
		selS, selE, hasSel := m.selRangeForLine(i, n, fullW)
		isCursorLine := i == m.cursorLine

		links := m.lineLinks[i]
		linkAt := func(col int) string {
			if col >= n {
				return ""
			}
			for _, lr := range links {
				if col >= lr.start && col < lr.end {
					return lr.url
				}
			}
			return ""
		}

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
			url := linkAt(col)
			j := col + 1
			for j < fullW && styleAt(j) == s && linkAt(j) == url {
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
				if url != "" {
					styled = link.Render(chunk.String())
				} else {
					styled = base.Render(chunk.String())
				}
			}
			if url != "" {
				styled = "\x1b]8;;" + url + "\x07" + styled + "\x1b]8;;\x07"
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

// --- Vim motions / text objects ---

func isWordRune(r rune) bool {
	return r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		r >= 0x80
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' }

// charClass: 0 = space, 1 = word, 2 = punct. With big=true everything non-space is class 1.
func charClass(r rune, big bool) int {
	if isSpaceRune(r) {
		return 0
	}
	if big {
		return 1
	}
	if isWordRune(r) {
		return 1
	}
	return 2
}

func (m *ReaderModel) lineRunes(i int) []rune {
	if i < 0 || i >= len(m.bodyLines) {
		return nil
	}
	return []rune(m.bodyLines[i])
}

func (m *ReaderModel) WordForward(big bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	line, col := m.cursorLine, m.cursorCol
	runes := m.lineRunes(line)
	n := len(runes)

	// Determine current class; if at/after EOL treat as space (advance to next line).
	var startClass int
	if col >= n {
		startClass = 0
	} else {
		startClass = charClass(runes[col], big)
	}

	advance := func() bool {
		if col+1 < n {
			col++
			return true
		}
		if line+1 < len(m.bodyLines) {
			line++
			runes = m.lineRunes(line)
			n = len(runes)
			col = 0
			return true
		}
		return false
	}

	// Skip rest of current run (if non-space), then skip whitespace.
	for col < n && charClass(runes[col], big) == startClass && startClass != 0 {
		if !advance() {
			break
		}
	}
	// At end of line — fall through to advance once (which moves to next line col 0).
	if col >= n {
		if !advance() {
			m.cursorLine, m.cursorCol = line, n
			m.ensureCursorVisible()
			m.refresh()
			return
		}
	}
	for col < n && charClass(runes[col], big) == 0 {
		if !advance() {
			break
		}
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) WordBackward(big bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	line, col := m.cursorLine, m.cursorCol
	runes := m.lineRunes(line)

	step := func() bool {
		if col > 0 {
			col--
			return true
		}
		if line > 0 {
			line--
			runes = m.lineRunes(line)
			col = len(runes)
			return true
		}
		return false
	}

	// Step back once before classification (vim semantics).
	if !step() {
		return
	}
	// Skip whitespace.
	for {
		if col < len(runes) && charClass(runes[col], big) != 0 {
			break
		}
		if !step() {
			m.cursorLine, m.cursorCol = line, col
			m.clampCursor()
			m.ensureCursorVisible()
			m.refresh()
			return
		}
	}
	c := charClass(runes[col], big)
	for col > 0 && charClass(runes[col-1], big) == c {
		col--
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) WordEnd(big bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	line, col := m.cursorLine, m.cursorCol
	runes := m.lineRunes(line)
	n := len(runes)

	advance := func() bool {
		if col+1 < n {
			col++
			return true
		}
		if line+1 < len(m.bodyLines) {
			line++
			runes = m.lineRunes(line)
			n = len(runes)
			col = 0
			return true
		}
		return false
	}

	if !advance() {
		return
	}
	// Skip whitespace.
	for col < n && charClass(runes[col], big) == 0 {
		if !advance() {
			m.cursorLine, m.cursorCol = line, col
			m.clampCursor()
			m.ensureCursorVisible()
			m.refresh()
			return
		}
	}
	if col >= n {
		m.cursorLine, m.cursorCol = line, col
		m.clampCursor()
		m.ensureCursorVisible()
		m.refresh()
		return
	}
	c := charClass(runes[col], big)
	for col+1 < n && charClass(runes[col+1], big) == c {
		col++
	}
	m.cursorLine, m.cursorCol = line, col
	m.clampCursor()
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) SelectWord(big, around bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	runes := m.lineRunes(m.cursorLine)
	n := len(runes)
	if n == 0 {
		return
	}
	col := m.cursorCol
	if col >= n {
		col = n - 1
	}
	c := charClass(runes[col], big)
	s, e := col, col
	for s > 0 && charClass(runes[s-1], big) == c {
		s--
	}
	for e+1 < n && charClass(runes[e+1], big) == c {
		e++
	}
	if around {
		if c == 0 {
			// pure whitespace under cursor — extend to surrounding word
			if e+1 < n {
				ec := charClass(runes[e+1], big)
				for e+1 < n && charClass(runes[e+1], big) == ec {
					e++
				}
			} else if s > 0 {
				sc := charClass(runes[s-1], big)
				for s > 0 && charClass(runes[s-1], big) == sc {
					s--
				}
			}
		} else {
			// extend trailing whitespace; if none, extend leading.
			if e+1 < n && charClass(runes[e+1], big) == 0 {
				for e+1 < n && charClass(runes[e+1], big) == 0 {
					e++
				}
			} else {
				for s > 0 && charClass(runes[s-1], big) == 0 {
					s--
				}
			}
		}
	}
	m.vmode = VisualChar
	m.anchorLine, m.anchorCol = m.cursorLine, s
	m.cursorCol = e
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) SelectQuote(q rune, around bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	runes := m.lineRunes(m.cursorLine)
	n := len(runes)
	col := m.cursorCol
	if col > n {
		col = n
	}
	// find left quote at or before col
	l := -1
	for i := col; i >= 0 && i < n; i-- {
		if runes[i] == q {
			l = i
			break
		}
	}
	if l < 0 {
		// search forward instead
		for i := col; i < n; i++ {
			if runes[i] == q {
				l = i
				break
			}
		}
		if l < 0 {
			return
		}
	}
	r := -1
	for i := l + 1; i < n; i++ {
		if runes[i] == q {
			r = i
			break
		}
	}
	if r < 0 {
		return
	}
	var s, e int
	if around {
		s, e = l, r
	} else {
		if r-l < 2 {
			return
		}
		s, e = l+1, r-1
	}
	m.vmode = VisualChar
	m.anchorLine, m.anchorCol = m.cursorLine, s
	m.cursorCol = e
	m.ensureCursorVisible()
	m.refresh()
}

// findBracketPair searches outward across multiple lines for matched open/close.
func (m *ReaderModel) findBracketPair(open, close rune) (sL, sC, eL, eC int, ok bool) {
	cl, cc := m.cursorLine, m.cursorCol
	// search backward for unmatched open
	depth := 0
	sL, sC = -1, -1
	for line := cl; line >= 0; line-- {
		runes := m.lineRunes(line)
		start := len(runes) - 1
		if line == cl {
			start = cc
			if start >= len(runes) {
				start = len(runes) - 1
			}
		}
		for i := start; i >= 0; i-- {
			r := runes[i]
			if r == close && !(line == cl && i == cc) {
				depth++
			} else if r == open {
				if depth == 0 {
					sL, sC = line, i
					line = -1
					break
				}
				depth--
			}
		}
		if sL >= 0 {
			break
		}
	}
	if sL < 0 {
		return 0, 0, 0, 0, false
	}
	// search forward from after open
	depth = 0
	for line := sL; line < len(m.bodyLines); line++ {
		runes := m.lineRunes(line)
		i := 0
		if line == sL {
			i = sC + 1
		}
		for ; i < len(runes); i++ {
			r := runes[i]
			if r == open {
				depth++
			} else if r == close {
				if depth == 0 {
					return sL, sC, line, i, true
				}
				depth--
			}
		}
	}
	return 0, 0, 0, 0, false
}

func (m *ReaderModel) SelectBracket(open, close rune, around bool) {
	sL, sC, eL, eC, ok := m.findBracketPair(open, close)
	if !ok {
		return
	}
	if !around {
		// inner: skip the brackets themselves
		sC++
		eC--
		// empty pair
		if sL == eL && sC > eC {
			return
		}
	}
	m.vmode = VisualChar
	m.anchorLine, m.anchorCol = sL, sC
	m.cursorLine, m.cursorCol = eL, eC
	m.ensureCursorVisible()
	m.refresh()
}

func (m *ReaderModel) SelectParagraph(around bool) {
	if !m.hasMsg || len(m.bodyLines) == 0 {
		return
	}
	isBlank := func(s string) bool { return strings.TrimSpace(s) == "" }
	cl := m.cursorLine
	s, e := cl, cl
	if isBlank(m.bodyLines[cl]) {
		// expand blank run
		for s > 0 && isBlank(m.bodyLines[s-1]) {
			s--
		}
		for e+1 < len(m.bodyLines) && isBlank(m.bodyLines[e+1]) {
			e++
		}
	} else {
		for s > 0 && !isBlank(m.bodyLines[s-1]) {
			s--
		}
		for e+1 < len(m.bodyLines) && !isBlank(m.bodyLines[e+1]) {
			e++
		}
	}
	if around {
		// include trailing blank lines, else leading
		if e+1 < len(m.bodyLines) && isBlank(m.bodyLines[e+1]) {
			for e+1 < len(m.bodyLines) && isBlank(m.bodyLines[e+1]) {
				e++
			}
		} else {
			for s > 0 && isBlank(m.bodyLines[s-1]) {
				s--
			}
		}
	}
	m.vmode = VisualLine
	m.anchorLine, m.anchorCol = s, 0
	m.cursorLine, m.cursorCol = e, 0
	m.ensureCursorVisible()
	m.refresh()
}

// LinkUnderCursor returns the URL of the link at the cursor, if any.
func (m *ReaderModel) LinkUnderCursor() string {
	if m.cursorLine < 0 || m.cursorLine >= len(m.lineLinks) {
		return ""
	}
	for _, lr := range m.lineLinks[m.cursorLine] {
		if m.cursorCol >= lr.start && m.cursorCol < lr.end {
			return lr.url
		}
	}
	// also accept clicking exactly at end+0 boundary
	for _, lr := range m.lineLinks[m.cursorLine] {
		if m.cursorCol == lr.end {
			return lr.url
		}
	}
	return ""
}

// YankLine returns the cursor's current line (used by `yy`).
func (m *ReaderModel) YankLine() (string, bool) {
	if len(m.bodyLines) == 0 {
		return "", false
	}
	return m.bodyLines[m.cursorLine], true
}
