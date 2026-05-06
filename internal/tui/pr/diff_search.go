// Package pr — diff search ("/pattern", n/N).
//
// vim-flavoured incremental search for the diff viewport. Pressing
// "/" pops a small textinput at the bottom of the diff pane; the user
// types a pattern and hits enter to commit. The match cursor jumps
// to the first hit and "n"/"N" cycle through subsequent hits. The
// active pattern is remembered across diff loads so re-entering the
// view keeps highlighting consistent.
package pr

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// diffSearchPrompt is the chip rendered at the start of the search
// overlay (mirrors `/` from less / vim so the affordance is obvious).
const diffSearchPrompt = "/"

// newDiffSearchInput builds the textinput used for the "/pattern"
// overlay. Kept off-character-limit and styled to blend with the
// surrounding diff chrome.
func newDiffSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "search diff…"
	ti.Prompt = diffSearchPrompt
	ti.CharLimit = 0
	return ti
}

// startDiffSearch focuses the search input so the user can type a
// pattern. Pre-fills with the previous pattern (if any) so the user
// can press enter to re-run the same search without retyping.
func (m *model) startDiffSearch() tea.Cmd {
	m.diffSearchActive = true
	m.diffSearchInput.SetValue(m.diffSearchPattern)
	m.diffSearchInput.CursorEnd()
	return m.diffSearchInput.Focus()
}

// commitDiffSearch records the current input as the active pattern,
// recomputes the match list, and jumps the diff cursor to the first
// hit at or after the current position. Status surfaces the count.
func (m *model) commitDiffSearch() {
	pattern := strings.TrimSpace(m.diffSearchInput.Value())
	m.diffSearchActive = false
	m.diffSearchInput.Blur()
	m.diffSearchPattern = pattern
	m.recomputeDiffSearchMatches()
	if pattern == "" {
		m.status = ""
		return
	}
	if len(m.diffSearchMatches) == 0 {
		m.status = theme.ErrPrefix() + "no matches for /" + pattern
		return
	}
	// Pick the first match at or after the current cursor so the
	// user lands on the closest hit instead of always snapping to
	// the top of the file.
	m.diffSearchIdx = 0
	for i, idx := range m.diffSearchMatches {
		if idx >= m.diffCursor {
			m.diffSearchIdx = i
			break
		}
	}
	m.jumpToCurrentDiffMatch()
	m.status = theme.OKPrefix() + diffSearchSummary(len(m.diffSearchMatches), m.diffSearchIdx, pattern)
}

// cancelDiffSearch dismisses the input without committing a new
// pattern; the previous pattern (and matches) stay in place so n/N
// keep working.
func (m *model) cancelDiffSearch() {
	m.diffSearchActive = false
	m.diffSearchInput.Blur()
}

// nextDiffMatch / prevDiffMatch advance the match cursor. Wrapping
// is the vim default so users don't fall off the ends; the status
// line reports a wrap when it happens so the jump isn't surprising.
func (m *model) nextDiffMatch() {
	if len(m.diffSearchMatches) == 0 {
		return
	}
	m.diffSearchIdx = (m.diffSearchIdx + 1) % len(m.diffSearchMatches)
	m.jumpToCurrentDiffMatch()
	m.status = theme.OKPrefix() + diffSearchSummary(len(m.diffSearchMatches), m.diffSearchIdx, m.diffSearchPattern)
}

func (m *model) prevDiffMatch() {
	if len(m.diffSearchMatches) == 0 {
		return
	}
	m.diffSearchIdx = (m.diffSearchIdx - 1 + len(m.diffSearchMatches)) % len(m.diffSearchMatches)
	m.jumpToCurrentDiffMatch()
	m.status = theme.OKPrefix() + diffSearchSummary(len(m.diffSearchMatches), m.diffSearchIdx, m.diffSearchPattern)
}

// jumpToCurrentDiffMatch moves the diff cursor onto the row for the
// current diffSearchIdx and re-renders so the marker / scroll catch
// up. Bails silently when the index is out of range so a stale
// search after a diff reload doesn't crash.
func (m *model) jumpToCurrentDiffMatch() {
	if m.diffSearchIdx < 0 || m.diffSearchIdx >= len(m.diffSearchMatches) {
		return
	}
	row := m.diffSearchMatches[m.diffSearchIdx]
	if row < 0 || row >= len(m.diffRows) {
		return
	}
	m.diffCursor = row
	m.syncTreeCursor()
	m.diff.SetContent(m.renderDiffRows())
	m.ensureDiffCursorVisible()
}

// recomputeDiffSearchMatches rebuilds diffSearchMatches by scanning
// every visible row's raw text for diffSearchPattern (case-insensitive).
// Annotation rows and blank rows are skipped — searching them would
// surface comment hits when the user is asking about diff content.
func (m *model) recomputeDiffSearchMatches() {
	m.diffSearchMatches = m.diffSearchMatches[:0]
	if m.diffSearchPattern == "" {
		return
	}
	needle := strings.ToLower(m.diffSearchPattern)
	for i, row := range m.diffRows {
		if row.annotation {
			continue
		}
		if rowContainsDiffNeedle(row, needle) {
			m.diffSearchMatches = append(m.diffSearchMatches, i)
		}
	}
}

// rowContainsDiffNeedle scans every cell of the row (split mode uses
// both columns; unified uses only cells[0]) for a case-insensitive
// substring hit.
func rowContainsDiffNeedle(row diffRow, needle string) bool {
	for _, c := range row.cells {
		if c.raw == "" {
			continue
		}
		if strings.Contains(strings.ToLower(c.raw), needle) {
			return true
		}
	}
	return false
}

// renderDiffSearchOverlay returns the styled "/pattern" footer drawn
// just above the help bar while the search input is active. Returns
// "" when search isn't active so View can collapse the line.
func (m model) renderDiffSearchOverlay() string {
	if !m.diffSearchActive {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true).
		Render(m.diffSearchInput.View())
}

// diffSearchSummary formats the "match m of n for /pattern" status.
func diffSearchSummary(total, idx int, pattern string) string {
	return fmt.Sprintf("match %d of %d for /%s", idx+1, total, pattern)
}
