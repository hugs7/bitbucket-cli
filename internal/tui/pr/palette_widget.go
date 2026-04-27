// Package pr — searchable command palette widget.
//
// A custom overlay-style palette with text input + fuzzy filtering,
// designed to feel like Amp's command palette: typing immediately
// filters the list, up/down navigates, enter selects, esc closes.
// Rendered as a centred card on top of the underlying view rather
// than as a full-screen replacement.
package pr

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// paletteWidget is a self-contained fuzzy command palette: a search
// input on top, a ranked list of matches below, and a cursor index
// for selection. Used in place of bubbles/list so typing immediately
// filters (no need to press '/' first) and the rendering stays a
// compact card we can overlay on the underlying view.
type paletteWidget struct {
	input    textinput.Model
	items    []paletteItem // master list, populated per mode on open
	filtered []int         // indexes into items, ordered by match rank
	cursor   int           // index into filtered (0-based)
}

// newPaletteWidget constructs a fresh widget with sensible defaults.
// The textinput is left blurred; openPalette focuses it on entry so
// the very first keystroke contributes to the filter.
func newPaletteWidget() paletteWidget {
	ti := textinput.New()
	ti.Placeholder = "Type to filter…"
	ti.Prompt = theme.SearchPrompt()
	ti.CharLimit = 120
	return paletteWidget{input: ti}
}

// SetItems re-seeds the master item list, clears the query and
// resets the cursor. Called each time the palette opens so the items
// reflect the current view mode.
func (p *paletteWidget) SetItems(items []paletteItem) {
	p.items = items
	p.input.SetValue("")
	p.refilter()
}

// Focus engages the textinput so typing flows into the query.
func (p *paletteWidget) Focus() tea.Cmd { return p.input.Focus() }

// SelectedItem returns the currently-highlighted paletteItem, or
// (zero, false) when the filter excludes everything.
func (p *paletteWidget) SelectedItem() (paletteItem, bool) {
	if len(p.filtered) == 0 || p.cursor < 0 || p.cursor >= len(p.filtered) {
		return paletteItem{}, false
	}
	return p.items[p.filtered[p.cursor]], true
}

// MoveCursor advances the cursor by delta within the filtered list,
// clamped to [0, len-1]. No wrap-around — we want predictable bounds
// when the user holds j/k.
func (p *paletteWidget) MoveCursor(delta int) {
	if len(p.filtered) == 0 {
		p.cursor = 0
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
}

// Update routes messages into the textinput. After every change we
// re-run the fuzzy filter so the list shrinks live as the user types.
// Cursor is clamped back into range; we don't try to "stick" to the
// previously-selected item because the rank order changes.
func (p *paletteWidget) Update(msg tea.Msg) tea.Cmd {
	prev := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prev {
		p.refilter()
		p.cursor = 0
	}
	return cmd
}

// refilter re-runs the fuzzy match against the current query. An
// empty query shows everything in the original order so users see
// the full menu when they first open the palette.
func (p *paletteWidget) refilter() {
	q := strings.TrimSpace(p.input.Value())
	if q == "" {
		p.filtered = make([]int, len(p.items))
		for i := range p.items {
			p.filtered[i] = i
		}
		return
	}
	targets := make([]string, len(p.items))
	for i, it := range p.items {
		targets[i] = it.label
	}
	matches := fuzzy.Find(q, targets)
	p.filtered = p.filtered[:0]
	for _, m := range matches {
		p.filtered = append(p.filtered, m.Index)
	}
}

// View renders the palette as a bordered card. Width is bounded so
// the card stays compact regardless of terminal size; height grows
// with the filtered list up to a sensible maximum so very long menus
// don't push the input off-screen.
func (p paletteWidget) View(width, height int) string {
	cardW := width * 60 / 100
	if cardW < 50 {
		cardW = 50
	}
	if cardW > width-4 {
		cardW = width - 4
	}
	if cardW > 90 {
		cardW = 90
	}
	maxItems := height - 8 // header + input + footer + borders
	if maxItems < 5 {
		maxItems = 5
	}
	if maxItems > 18 {
		maxItems = 18
	}

	header := theme.TitleBadge.Render(" COMMAND PALETTE ") + "  " +
		theme.TitleChipDim.Render("type to filter · ↑/↓ select · enter run · esc close")

	p.input.Width = cardW - 4
	body := p.input.View()

	listLines := make([]string, 0, maxItems)
	if len(p.filtered) == 0 {
		listLines = append(listLines, theme.TitleChipDim.Render("  (no matches)"))
	} else {
		// Window the filtered slice around the cursor so long lists
		// stay within maxItems without losing the selected row.
		start := 0
		if p.cursor >= maxItems {
			start = p.cursor - maxItems + 1
		}
		end := start + maxItems
		if end > len(p.filtered) {
			end = len(p.filtered)
		}
		selStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("57")).
			Bold(true)
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		for i := start; i < end; i++ {
			it := p.items[p.filtered[i]]
			row := "  " + it.label
			if it.hint != "" {
				row += "  " + hintStyle.Render("("+it.hint+")")
			}
			// Pad to card width so the highlight bar fills the row.
			pad := cardW - 4 - lipgloss.Width(row)
			if pad > 0 {
				row += strings.Repeat(" ", pad)
			}
			if i == p.cursor {
				row = selStyle.Render(row)
			}
			listLines = append(listLines, row)
		}
	}

	footer := theme.TitleChipDim.Render(
		formatPaletteFooter(len(p.filtered), len(p.items), p.cursor),
	)

	content := strings.Join([]string{
		header,
		"",
		body,
		"",
		strings.Join(listLines, "\n"),
		"",
		footer,
	}, "\n")

	return lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color("57")).
		Padding(1, 2).
		Width(cardW).
		Render(content)
}

// formatPaletteFooter renders the "n of N · selected position" status
// line shown beneath the palette list.
func formatPaletteFooter(filtered, total, cursor int) string {
	if total == 0 {
		return "no items"
	}
	if filtered == 0 {
		return "0 of " + strconv.Itoa(total)
	}
	return strconv.Itoa(filtered) + " of " + strconv.Itoa(total) + "  ·  selected " + strconv.Itoa(cursor+1)
}
