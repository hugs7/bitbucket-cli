// Package tui — diff parsing, layout, and rendering.
//
// Pulled out of prs.go to keep that file focused on the model and
// update flow. Everything in here is tightly coupled to the diff
// viewport: the parsed-line representation, word-diff highlights,
// inline-comment annotation rows, file-tree side panel, and the
// unified / split renderers.
package pr

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

// diffLine is the parsed form of one line of a unified diff. We hold
// the raw text and style separately so we can re-render it at any
// width when laying out a split view.
type diffLine struct {
	raw       string
	style     lipgloss.Style
	path      string
	side      string // "new", "old", "" (header / hunk / unknown), or "both" (context)
	lineNo    int    // file line number on the new side (or sole side)
	oldNo     int    // for "both" rows, the line on the old side
	preferOld bool   // for "both" rows, anchor on the old side
	// highlights are byte ranges within raw that should be painted in
	// a brighter "word-diff" colour to draw attention to the actual
	// edited tokens within an otherwise-shaded line.
	highlights []hlRange
}

// hlRange is a [start, end) byte span within a diffLine.raw or
// diffCell.raw used to overlay word-diff highlights.
type hlRange struct{ start, end int }

// Word-diff overlay colours: brighter shades of the line backgrounds
// so the changed tokens within an added/removed line read as the
// "edit" while the surrounding text reads as "context for the edit".
var (
	diffAddHL = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("28"))
	diffDelHL = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("124"))
)

// tokenizeForDiff splits s into a stream of tokens — runs of word
// characters and individual non-word characters. Stable byte offsets
// let us map LCS results back to ranges in the original string.
func tokenizeForDiff(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		c := s[i]
		isWord := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
		if isWord {
			j := i + 1
			for j < len(s) {
				d := s[j]
				if !((d >= 'a' && d <= 'z') || (d >= 'A' && d <= 'Z') || (d >= '0' && d <= '9') || d == '_') {
					break
				}
				j++
			}
			out = append(out, s[i:j])
			i = j
		} else {
			out = append(out, s[i:i+1])
			i++
		}
	}
	return out
}

// wordDiffRanges computes the byte ranges within a and b that differ
// by tokens (LCS-based). Ignored when either side is empty.
func wordDiffRanges(a, b string) (aRanges, bRanges []hlRange) {
	if a == "" || b == "" {
		return nil, nil
	}
	ta := tokenizeForDiff(a)
	tb := tokenizeForDiff(b)
	n, m := len(ta), len(tb)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if ta[i-1] == tb[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	sameA := make([]bool, n)
	sameB := make([]bool, m)
	i, j := n, m
	for i > 0 && j > 0 {
		switch {
		case ta[i-1] == tb[j-1]:
			sameA[i-1] = true
			sameB[j-1] = true
			i--
			j--
		case dp[i-1][j] >= dp[i][j-1]:
			i--
		default:
			j--
		}
	}
	pos := 0
	for k, t := range ta {
		if !sameA[k] {
			aRanges = append(aRanges, hlRange{pos, pos + len(t)})
		}
		pos += len(t)
	}
	pos = 0
	for k, t := range tb {
		if !sameB[k] {
			bRanges = append(bRanges, hlRange{pos, pos + len(t)})
		}
		pos += len(t)
	}
	return coalesceHL(aRanges), coalesceHL(bRanges)
}

func coalesceHL(rs []hlRange) []hlRange {
	if len(rs) <= 1 {
		return rs
	}
	out := []hlRange{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.start <= last.end {
			if r.end > last.end {
				last.end = r.end
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}

// computeWordHighlights walks parsed diff lines, finds runs of "-"
// followed by "+" (the typical hunk shape), and pair-wise computes
// word-diff ranges into each line's .highlights field. Ranges include
// the leading "+"/"-" prefix offset so renderers can apply them
// directly against raw.
func computeWordHighlights(lines []diffLine) {
	i := 0
	for i < len(lines) {
		if lines[i].side != "old" {
			i++
			continue
		}
		dStart := i
		for i < len(lines) && lines[i].side == "old" {
			i++
		}
		aStart := i
		for i < len(lines) && lines[i].side == "new" {
			i++
		}
		dels := lines[dStart:aStart]
		adds := lines[aStart:i]
		n := min(len(adds), len(dels))
		for k := 0; k < n; k++ {
			oldBody := strings.TrimPrefix(dels[k].raw, "-")
			newBody := strings.TrimPrefix(adds[k].raw, "+")
			oR, nR := wordDiffRanges(oldBody, newBody)
			// Shift back to include the leading "-"/"+" character.
			for r := range oR {
				oR[r].start++
				oR[r].end++
			}
			for r := range nR {
				nR[r].start++
				nR[r].end++
			}
			dels[k].highlights = oR
			adds[k].highlights = nR
		}
	}
}

// commentable reports whether this row can carry an inline comment.
func (d diffLine) commentable() bool { return d.path != "" && d.side != "" && d.lineNo > 0 }

// cleanDiffPath strips the various prefix conventions that show up in
// unified diff headers so the result is a plain repo-relative path
// matching what inline-comment anchors use. Bitbucket Server emits
// `src://` and `dst://` for some diffs in addition to git's `a/`/`b/`.
func cleanDiffPath(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"src://", "dst://", "a/", "b/"} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	return s
}

// parseDiff walks a unified diff and produces one diffLine per text
// line, decorated with file/line/side metadata where applicable.
func parseDiff(diff string) []diffLine {
	var out []diffLine
	var path string
	var oldLine, newLine int

	for _, line := range strings.Split(diff, "\n") {
		dl := diffLine{raw: line, style: diffCtxStyle}
		switch {
		case strings.HasPrefix(line, "diff "):
			// "diff --git a/foo b/foo" — take the b/ side as the post-image path.
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				path = cleanDiffPath(parts[3])
			}
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "+++ "):
			p := cleanDiffPath(strings.TrimPrefix(line, "+++ "))
			if p != "/dev/null" {
				path = p
			}
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "index "):
			dl.style = diffMetaStyle
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = parseHunkHeader(line)
			dl.style = diffHunkStyle
		case strings.HasPrefix(line, "+"):
			dl.style = diffAddStyle
			dl.path, dl.side, dl.lineNo = path, "new", newLine
			newLine++
		case strings.HasPrefix(line, "-"):
			dl.style = diffDelStyle
			dl.path, dl.side, dl.lineNo = path, "old", oldLine
			oldLine++
		case strings.HasPrefix(line, " "):
			// Context: exists on both sides. We default to "new" for
			// commenting; user can press 't' to flip.
			dl.path, dl.side, dl.lineNo, dl.oldNo = path, "both", newLine, oldLine
			oldLine++
			newLine++
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" — leave plain.
		}
		out = append(out, dl)
	}
	return out
}

// parseHunkHeader extracts the starting line numbers from "@@ -A,B +C,D @@".
func parseHunkHeader(line string) (oldStart, newStart int) {
	rest := strings.TrimPrefix(line, "@@")
	rest = strings.TrimSpace(rest)
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return 0, 0
	}
	parseRange := func(s string) int {
		s = strings.TrimPrefix(s, "-")
		s = strings.TrimPrefix(s, "+")
		if i := strings.Index(s, ","); i >= 0 {
			s = s[:i]
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return parseRange(fields[0]), parseRange(fields[1])
}

// diffCell is one column of one display row. Cells carry both visual
// content and the (path, side, line) anchor needed when the user fires
// 'c' to add an inline comment from this position.
type diffCell struct {
	raw   string
	style lipgloss.Style
	path  string
	side  string
	line  int

	// Word-diff overlay: highlights are byte ranges within raw and
	// hlStyle is the style to paint over them. Empty highlights → no
	// overlay (the cell renders as a single styled run).
	highlights []hlRange
	hlStyle    lipgloss.Style
}

func (c diffCell) commentable() bool { return c.path != "" && c.side != "" && c.line > 0 }

// diffRow is one logical row in the rendered diff. In unified mode
// only cells[0] is populated; in split mode cells[0] is the old (LHS)
// side and cells[1] is the new (RHS) side. Hunk and file headers use
// fullWidth=true to span both columns.
type diffRow struct {
	cells        [2]diffCell
	fullWidth    bool
	annotation   bool           // comment annotation row (no anchor of its own)
	annoText     string         // plain text (no ANSI) for annotation
	annoStyle    lipgloss.Style // style applied at render time at the right width
	annoSide     int            // 0 = old column, 1 = new column (split only)
	annoIsHeader bool           // header (author/timestamp) vs body line of a comment
}

type renderedDiffRow struct {
	lines []string
}

// rebuildDiffRows regenerates the display row sequence from the parsed
// diff lines + comments according to the current toggles. Called after
// the diff loads, the comments load, the user toggles split / inline,
// or the viewport resizes.
func (m *model) rebuildDiffRows() {
	if m.diffSplit {
		m.diffRows = buildSplitRows(m.diffLines)
	} else {
		m.diffRows = buildUnifiedRows(m.diffLines)
	}
	if m.diffShowInline {
		m.diffRows = injectInlineComments(m.diffRows, m.diffComments, m.annotationWidth())
	}
	m.invalidateDiffRenderCache()
	m.diffFiles = computeDiffFiles(m.diffRows)
	m.diffTree = computeFileTree(m.diffFiles)
	m.diff.SetContent(m.renderDiffRows())
	m.clampDiffCursor()
	m.syncTreeCursor()
}

func (m *model) invalidateDiffRenderCache() {
	m.diffRenderRows = nil
	m.diffRenderW = 0
}

// annotationWidth returns the wrap width (in cells) for inline-comment
// annotation text in the current layout. Reserves space for the row
// marker, gutter glyphs and indent so wrapped lines line up with the
// code they're attached to.
func (m model) annotationWidth() int {
	width := m.diff.Width
	if width <= 0 {
		width = 80
	}
	if m.diffSplit {
		// Same column maths as renderDiffRows, minus the gutter
		// glyphs we prepend ("│  " = 3 cells, plus 1 cell of slack).
		colW := (width - 5) / 2
		w := colW - 4
		if w < 12 {
			w = 12
		}
		return w
	}
	// Unified: 2 cells marker + 4 cells indent + 2 cells gutter glyphs.
	w := width - 8
	if w < 20 {
		w = 20
	}
	return w
}

// computeDiffFiles scans the rendered rows and records the position of
// each file's "diff --git" header so the tree can jump-scroll there.
func computeDiffFiles(rows []diffRow) []diffFile {
	var out []diffFile
	for i, r := range rows {
		if !r.fullWidth {
			continue
		}
		raw := r.cells[0].raw
		if !strings.HasPrefix(raw, "diff ") {
			continue
		}
		parts := strings.Fields(raw)
		if len(parts) < 4 {
			continue
		}
		out = append(out, diffFile{path: cleanDiffPath(parts[3]), rowIdx: i})
	}
	return out
}

// syncTreeCursor moves the tree cursor to whichever file the diff
// cursor currently sits inside.
func (m *model) syncTreeCursor() {
	// Find the file containing the diff cursor.
	activePath := ""
	for _, f := range m.diffFiles {
		if f.rowIdx <= m.diffCursor {
			activePath = f.path
		}
	}
	if activePath == "" {
		return
	}
	for i, n := range m.diffTree {
		if !n.isDir && n.path == activePath {
			m.diffTreeCursor = i
			return
		}
	}
}

// hlStyleFor maps a diff side to its word-diff overlay style.
func hlStyleFor(side string) lipgloss.Style {
	switch side {
	case "new":
		return diffAddHL
	case "old":
		return diffDelHL
	}
	return lipgloss.NewStyle()
}

func buildUnifiedRows(lines []diffLine) []diffRow {
	rows := make([]diffRow, 0, len(lines))
	for _, dl := range lines {
		c := diffCell{raw: dl.raw, style: dl.style}
		if dl.commentable() {
			side, lineNo := inlineSide(dl)
			c.path, c.side, c.line = dl.path, side, lineNo
		}
		if len(dl.highlights) > 0 {
			c.highlights = dl.highlights
			c.hlStyle = hlStyleFor(dl.side)
		}
		// Header / hunk rows have no side; mark as fullWidth so the
		// file-tree builder can locate file boundaries (and so future
		// styling can treat them specially even in unified mode).
		full := dl.side == ""
		rows = append(rows, diffRow{cells: [2]diffCell{c}, fullWidth: full})
	}
	return rows
}

// buildSplitRows pairs runs of "-" with "+" so removed and added text
// sit on opposite columns of the same display row. Context lines
// appear identically on both sides; headers / hunk markers use
// fullWidth so they span the full width of the viewport.
func buildSplitRows(lines []diffLine) []diffRow {
	rows := []diffRow{}
	emptyOld := diffCell{}
	emptyNew := diffCell{}
	for i := 0; i < len(lines); {
		dl := lines[i]
		switch dl.side {
		case "":
			rows = append(rows, diffRow{cells: [2]diffCell{{raw: dl.raw, style: dl.style}}, fullWidth: true})
			i++
		case "both":
			body := strings.TrimPrefix(dl.raw, " ")
			oldCell := diffCell{raw: " " + body, style: diffCtxStyle, path: dl.path, side: "old", line: dl.oldNo}
			newCell := diffCell{raw: " " + body, style: diffCtxStyle, path: dl.path, side: "new", line: dl.lineNo}
			rows = append(rows, diffRow{cells: [2]diffCell{oldCell, newCell}})
			i++
		case "old":
			dels := []diffLine{dl}
			i++
			for i < len(lines) && lines[i].side == "old" {
				dels = append(dels, lines[i])
				i++
			}
			adds := []diffLine{}
			for i < len(lines) && lines[i].side == "new" {
				adds = append(adds, lines[i])
				i++
			}
			n := max(len(adds), len(dels))
			for j := 0; j < n; j++ {
				row := diffRow{cells: [2]diffCell{emptyOld, emptyNew}}
				if j < len(dels) {
					d := dels[j]
					row.cells[0] = diffCell{
						raw: d.raw, style: d.style,
						path: d.path, side: "old", line: d.lineNo,
						highlights: d.highlights, hlStyle: diffDelHL,
					}
				}
				if j < len(adds) {
					a := adds[j]
					row.cells[1] = diffCell{
						raw: a.raw, style: a.style,
						path: a.path, side: "new", line: a.lineNo,
						highlights: a.highlights, hlStyle: diffAddHL,
					}
				}
				rows = append(rows, row)
			}
		case "new":
			adds := []diffLine{dl}
			i++
			for i < len(lines) && lines[i].side == "new" {
				adds = append(adds, lines[i])
				i++
			}
			for _, a := range adds {
				rows = append(rows, diffRow{cells: [2]diffCell{
					emptyOld,
					{raw: a.raw, style: a.style, path: a.path, side: "new", line: a.lineNo},
				}})
			}
		default:
			i++
		}
	}
	return rows
}

// injectInlineComments walks the row list and inserts annotation rows
// immediately after each row that has a matching inline comment
// anchor. Comments are word-wrapped to wrapW so the full body is
// visible (rather than truncated to one line); each emitted row is
// one wrapped line so cursor / viewport maths stay row-accurate.
//
// File-level comments (Inline.Line == 0) are emitted right after the
// "diff --git" header for the matching file, so the entire file's
// general feedback shows at the top.
func injectInlineComments(rows []diffRow, comments []api.Comment, wrapW int) []diffRow {
	if len(comments) == 0 {
		return rows
	}
	// Index comments by (path, side, line) → []Comment so we can
	// attach multiple replies to the same anchor. Line==0 entries
	// land in fileComments instead.
	type key struct {
		path string
		side string
		line int
	}
	byAnchor := map[key][]api.Comment{}
	fileComments := map[string][]api.Comment{}
	for _, c := range comments {
		if c.Inline == nil {
			continue
		}
		if c.Inline.Line == 0 {
			fileComments[c.Inline.Path] = append(fileComments[c.Inline.Path], c)
			continue
		}
		k := key{c.Inline.Path, c.Inline.Side, c.Inline.Line}
		byAnchor[k] = append(byAnchor[k], c)
	}
	if len(byAnchor) == 0 && len(fileComments) == 0 {
		return rows
	}
	emitAnnotation := func(out *[]diffRow, c api.Comment, side int) {
		for _, line := range formatInlineAnnotationLines(c, wrapW) {
			*out = append(*out, diffRow{
				annotation:   true,
				annoText:     line.text,
				annoStyle:    line.style,
				annoSide:     side,
				annoIsHeader: line.header,
			})
		}
	}
	out := make([]diffRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
		if row.fullWidth {
			// On the "diff --git" header for a file, attach any
			// file-level comments so they appear at the top of the
			// file's diff in the overlay.
			raw := row.cells[0].raw
			if strings.HasPrefix(raw, "diff ") {
				parts := strings.Fields(raw)
				if len(parts) >= 4 {
					path := cleanDiffPath(parts[3])
					for _, c := range fileComments[path] {
						emitAnnotation(&out, c, 0)
					}
				}
			}
			continue
		}
		for idx, cell := range row.cells {
			if !cell.commentable() {
				continue
			}
			for _, c := range byAnchor[key{cell.path, cell.side, cell.line}] {
				emitAnnotation(&out, c, idx)
			}
		}
	}
	return out
}

type annoTextLine struct {
	text   string
	style  lipgloss.Style
	header bool
}

// formatInlineAnnotationLines splits a comment into a header line
// (author + relative timestamp) followed by word-wrapped body lines.
// Each returned line carries its own style so the renderer can draw
// the header bold and the body soft/italic.
func formatInlineAnnotationLines(c api.Comment, wrapW int) []annoTextLine {
	if wrapW < 8 {
		wrapW = 8
	}
	body := strings.TrimSpace(c.Text)
	if body == "" {
		body = "(empty)"
	}
	header := "💬 " + c.Author
	if !c.CreatedAt.IsZero() {
		header += "  " + humanTime(c.CreatedAt)
	}

	out := []annoTextLine{}
	for _, w := range wrapText(header, wrapW) {
		out = append(out, annoTextLine{text: w, style: annoHeaderStyle, header: true})
	}
	for _, raw := range strings.Split(body, "\n") {
		wrapped := wrapText(raw, wrapW)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, w := range wrapped {
			out = append(out, annoTextLine{text: w, style: annoBodyStyle})
		}
	}
	return out
}

// wrapText word-wraps s to fit width w (in terminal cells), breaking
// on whitespace where possible and falling back to hard rune-level
// breaks for tokens longer than w. Empty input yields one empty line.
func wrapText(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
		curW = 0
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	for _, word := range words {
		ww := lipgloss.Width(word)
		// Break long words across multiple lines.
		if ww > w {
			if curW > 0 {
				flush()
			}
			runes := []rune(word)
			for len(runes) > 0 {
				take := 0
				wAcc := 0
				for take < len(runes) {
					rw := lipgloss.Width(string(runes[take]))
					if wAcc+rw > w {
						break
					}
					wAcc += rw
					take++
				}
				if take == 0 {
					take = 1
				}
				out = append(out, string(runes[:take]))
				runes = runes[take:]
			}
			continue
		}
		extra := ww
		if curW > 0 {
			extra++ // leading space
		}
		if curW+extra > w {
			flush()
			cur.WriteString(word)
			curW = ww
		} else {
			if curW > 0 {
				cur.WriteByte(' ')
				curW++
			}
			cur.WriteString(word)
			curW += ww
		}
	}
	if curW > 0 {
		flush()
	}
	return out
}

// renderDiffRows produces the final string fed to the viewport.
// For split rows it formats two columns; for unified one. The cursor
// row gets a "▶" marker at the start. Long lines wrap to additional
// visual lines in split mode so code never overflows a column —
// diffRowYs records the visual offset of each logical row so cursor
// scroll math stays honest with multi-line wraps.
func (m *model) renderDiffRows() string {
	var b strings.Builder
	pointer := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render("▶ ")
	leftPad := "  "

	width := m.diff.Width
	if width <= 0 {
		width = 80
	}
	m.ensureDiffRenderCache(width)
	if len(m.diffRowYs) > 0 {
		totalLines := m.diffRowYs[len(m.diffRowYs)-1]
		if totalLines > 0 {
			b.Grow(totalLines * (width + 1))
		}
	}

	for i := range m.diffRows {
		lines := m.diffRenderRows[i].lines
		if m.diffSplit && i == m.diffCursor && !m.diffRows[i].fullWidth && !m.diffRows[i].annotation {
			lines = m.renderDiffRowLines(m.diffRows[i], width, true, m.diffCursorSide)
		}
		for j, line := range lines {
			if i == m.diffCursor && j == 0 {
				b.WriteString(pointer)
			} else {
				b.WriteString(leftPad)
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *model) ensureDiffRenderCache(width int) {
	if len(m.diffRenderRows) == len(m.diffRows) && m.diffRenderW == width && m.diffRenderSplit == m.diffSplit {
		return
	}
	m.diffRenderRows = make([]renderedDiffRow, len(m.diffRows))
	m.diffRenderW = width
	m.diffRenderSplit = m.diffSplit
	m.diffRowYs = make([]int, 0, len(m.diffRows)+1)
	visual := 0
	for i, row := range m.diffRows {
		m.diffRowYs = append(m.diffRowYs, visual)
		lines := m.renderDiffRowLines(row, width, false, 0)
		m.diffRenderRows[i] = renderedDiffRow{lines: lines}
		visual += len(lines)
	}
	m.diffRowYs = append(m.diffRowYs, visual)
}

func (m *model) renderDiffRowLines(row diffRow, width int, active bool, activeSide int) []string {
	colW := max(10, (width-5)/2) // 2 for marker, 3 for separator " │ "

	// gutterFor returns the styled left bar for an annotation row
	// — bright slab on header lines, thin bar on body lines, both
	// painted with the same background as the surrounding text so
	// the comment block reads as one continuous chip.
	gutterFor := func(headerLine bool) (string, lipgloss.Style) {
		if headerLine {
			return annoGutterStyle.Render("▎ "), annoGutterStyle
		}
		return annoBodyGutter.Render("│ "), annoBodyGutter
	}

	switch {
	case row.annotation:
		var b strings.Builder
		glyph, gStyle := gutterFor(row.annoIsHeader)
		if m.diffSplit {
			inner := colW - lipgloss.Width(glyph)
			if inner < 1 {
				inner = 1
			}
			text := truncateForCell(row.annoText, inner)
			styled := row.annoStyle.Width(inner).MaxWidth(inner).Render(text)
			cell := glyph + styled
			blank := strings.Repeat(" ", colW)
			if row.annoSide == 0 {
				b.WriteString(cell)
				b.WriteString(gStyle.Render(" │ "))
				b.WriteString(blank)
			} else {
				b.WriteString(blank)
				b.WriteString(gStyle.Render(" │ "))
				b.WriteString(cell)
			}
		} else {
			// Pad the body so the comment background spans the
			// remaining viewport width, mimicking Bitbucket's
			// commented-line block.
			body := width - 2 - 4 - lipgloss.Width(glyph)
			if body < 1 {
				body = 1
			}
			text := truncateForCell(row.annoText, body)
			b.WriteString(gStyle.Render("    "))
			b.WriteString(glyph)
			b.WriteString(row.annoStyle.Width(body).MaxWidth(body).Render(text))
		}
		return []string{b.String()}

	case row.fullWidth:
		body := width - 2
		if body < 1 {
			body = 1
		}
		return []string{row.cells[0].style.Width(body).MaxWidth(body).Render(row.cells[0].raw)}

	case m.diffSplit:
		leftActive := active && activeSide == 0
		rightActive := active && activeSide == 1
		leftChunks := wrapDiffCellChunks(row.cells[0], colW, leftActive)
		rightChunks := wrapDiffCellChunks(row.cells[1], colW, rightActive)
		n := len(leftChunks)
		if len(rightChunks) > n {
			n = len(rightChunks)
		}
		if n == 0 {
			n = 1
		}
		out := make([]string, 0, n)
		blank := strings.Repeat(" ", colW)
		for j := 0; j < n; j++ {
			var line strings.Builder
			if j < len(leftChunks) {
				line.WriteString(leftChunks[j])
			} else {
				line.WriteString(blank)
			}
			line.WriteString(" │ ")
			if j < len(rightChunks) {
				line.WriteString(rightChunks[j])
			} else {
				line.WriteString(blank)
			}
			out = append(out, line.String())
		}
		return out

	default:
		body := width - 2
		if body < 1 {
			body = 1
		}
		return []string{renderCellWithHL(row.cells[0], body)}
	}
}

// wrapDiffCellChunks returns one or more rendered strings for a split
// cell. When the cell's visible width fits in colW it returns a
// single chunk (matching the old behaviour); when the line is longer
// it splits the raw text into successive runs of at most colW cells
// and renders each with the cell's style + filtered highlights so
// the wrap reads as one continuous coloured block.
func wrapDiffCellChunks(c diffCell, w int, active bool) []string {
	if c.raw == "" {
		return []string{strings.Repeat(" ", w)}
	}
	if active {
		c.style = c.style.Underline(true)
		if len(c.highlights) > 0 {
			c.hlStyle = c.hlStyle.Underline(true)
		}
	}
	if lipgloss.Width(c.raw) <= w {
		return []string{renderCellWithHL(c, w)}
	}
	pieces := splitByCells(c.raw, w)
	out := make([]string, 0, len(pieces))
	for _, p := range pieces {
		sub := diffCell{
			raw:     c.raw[p.start:p.end],
			style:   c.style,
			hlStyle: c.hlStyle,
		}
		// Restrict highlights to the slice and shift indices so the
		// renderer can apply them inside the chunk's local range.
		for _, h := range c.highlights {
			if h.end <= p.start || h.start >= p.end {
				continue
			}
			s, e := h.start, h.end
			if s < p.start {
				s = p.start
			}
			if e > p.end {
				e = p.end
			}
			sub.highlights = append(sub.highlights, hlRange{start: s - p.start, end: e - p.start})
		}
		out = append(out, renderCellWithHL(sub, w))
	}
	return out
}

// splitSpan describes one wrap chunk: byte offsets [start, end) into
// the source string. byteEnd is exclusive.
type splitSpan struct{ start, end int }

// splitByCells walks raw rune-by-rune, accumulating visible cells
// (using lipgloss.Width to handle wide / zero-width runes) and emits
// a splitSpan whenever adding the next rune would exceed w. The
// caller can slice raw[start:end] to recover each chunk's text
// without copying.
func splitByCells(raw string, w int) []splitSpan {
	if w <= 0 {
		return []splitSpan{{0, len(raw)}}
	}
	var out []splitSpan
	cur := 0
	chunkStart := 0
	for i := 0; i < len(raw); {
		// Decode one rune. Skip past combining marks etc by stepping
		// by their UTF-8 size (lipgloss.Width handles zero-width).
		r, size := utf8.DecodeRuneInString(raw[i:])
		rw := lipgloss.Width(string(r))
		if rw < 0 {
			rw = 0
		}
		if cur+rw > w && i > chunkStart {
			out = append(out, splitSpan{chunkStart, i})
			chunkStart = i
			cur = 0
		}
		cur += rw
		i += size
	}
	if chunkStart < len(raw) {
		out = append(out, splitSpan{chunkStart, len(raw)})
	}
	if len(out) == 0 {
		out = []splitSpan{{0, len(raw)}}
	}
	return out
}

// renderCellWithHL renders a diff cell honouring its word-diff
// highlight ranges. Falls back to the cheap "single styled run with
// width padding" when there are no highlights or when the line is
// wider than the column (in which case lipgloss handles truncation
// for us).
func renderCellWithHL(c diffCell, w int) string {
	if len(c.highlights) == 0 || lipgloss.Width(c.raw) > w {
		return c.style.Width(w).MaxWidth(w).Render(c.raw)
	}
	var b strings.Builder
	cur := 0
	for _, h := range c.highlights {
		if h.start < cur {
			h.start = cur
		}
		if h.start > len(c.raw) {
			break
		}
		if h.end > len(c.raw) {
			h.end = len(c.raw)
		}
		if h.start > cur {
			b.WriteString(c.style.Render(c.raw[cur:h.start]))
		}
		if h.end > h.start {
			b.WriteString(c.hlStyle.Render(c.raw[h.start:h.end]))
		}
		cur = h.end
	}
	if cur < len(c.raw) {
		b.WriteString(c.style.Render(c.raw[cur:]))
	}
	have := lipgloss.Width(b.String())
	if w > have {
		b.WriteString(c.style.Render(strings.Repeat(" ", w-have)))
	}
	return b.String()
}

// truncateForCell trims plain text to fit a column of width w in
// terminal cells (using lipgloss.Width which is ANSI- and rune-aware).
// We trim from the end and append a single-cell ellipsis.
func truncateForCell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for n := len(runes) - 1; n > 0; n-- {
		cut := string(runes[:n]) + "…"
		if lipgloss.Width(cut) <= w {
			return cut
		}
	}
	return "…"
}

// clampDiffCursor keeps the cursor within bounds after the row list
// is rebuilt.
func (m *model) clampDiffCursor() {
	if len(m.diffRows) == 0 {
		m.diffCursor = 0
		return
	}
	if m.diffCursor < 0 {
		m.diffCursor = 0
	}
	if m.diffCursor >= len(m.diffRows) {
		m.diffCursor = len(m.diffRows) - 1
	}
}

// activeDiffCell returns the cell the cursor is currently pointing to,
// honouring the column selection in split mode.
func (m *model) activeDiffCell() (diffCell, bool) {
	if m.diffCursor >= len(m.diffRows) {
		return diffCell{}, false
	}
	row := m.diffRows[m.diffCursor]
	if row.fullWidth || row.annotation {
		return diffCell{}, false
	}
	idx := 0
	if m.diffSplit {
		idx = m.diffCursorSide
	}
	c := row.cells[idx]
	if !c.commentable() {
		// Fall back to the other side if active side is empty (e.g.
		// pure addition row in split, cursor on old side).
		other := row.cells[1-idx]
		if other.commentable() {
			return other, true
		}
	}
	return c, c.commentable()
}

// ensureDiffCursorVisible scrolls the viewport if the cursor would
// otherwise be off-screen. With split-mode line wrapping a single
// logical row can span multiple visual lines, so we translate the
// cursor's row index into a visual offset using diffRowYs before
// comparing against the viewport's YOffset window.
func (m *model) ensureDiffCursorVisible() {
	h := m.diff.Height
	if h <= 0 {
		return
	}
	cursorTop, cursorBot := m.diffCursorVisualRange()
	top := m.diff.YOffset
	bot := top + h - 1
	switch {
	case cursorTop < top:
		m.diff.SetYOffset(cursorTop)
	case cursorBot > bot:
		m.diff.SetYOffset(cursorBot - h + 1)
	}
}

// diffCursorVisualRange returns the [top, bottom] visual line range
// (inclusive) the cursor's logical row currently occupies in the
// rendered viewport. Falls back to the cursor index itself when
// diffRowYs hasn't been populated yet (e.g. very first render).
func (m *model) diffCursorVisualRange() (int, int) {
	if len(m.diffRowYs) == 0 || m.diffCursor < 0 || m.diffCursor >= len(m.diffRowYs) {
		return m.diffCursor, m.diffCursor
	}
	top := m.diffRowYs[m.diffCursor]
	bot := top
	if m.diffCursor+1 < len(m.diffRowYs) {
		bot = m.diffRowYs[m.diffCursor+1] - 1
	}
	if bot < top {
		bot = top
	}
	return top, bot
}

// inlineSide normalises a row's side into the API value. "both" rows
// (context) default to "new"; flipping dl.preferOld switches to "old".
func inlineSide(dl diffLine) (string, int) {
	if dl.side == "both" {
		if dl.preferOld {
			return "old", dl.oldNo
		}
		return "new", dl.lineNo
	}
	return dl.side, dl.lineNo
}
