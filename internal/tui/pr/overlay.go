// Package pr — overlay rendering helper.
//
// Lipgloss doesn't ship with a true z-axis composer; this small
// helper line-overwrites a foreground rendering on top of a
// background rendering centred at (x, y). Used by the palette and
// inline-diff editor to feel like floating cards instead of
// full-screen replacements.
package pr

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// placeOverlay overlays `fg` on top of `bg`, centred horizontally on
// the full background width and anchored vertically by yAnchor.
// Returns the merged rendering.
//
// yAnchor of -1 means "centre vertically too". Positive values are
// the row offset from the top of bg where the top of fg should land;
// the function clamps so fg never falls off the bottom.
func placeOverlay(bg, fg string, yAnchor int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	bgH := len(bgLines)
	fgH := len(fgLines)

	bgW := 0
	for _, l := range bgLines {
		if w := lipgloss.Width(l); w > bgW {
			bgW = w
		}
	}
	fgW := 0
	for _, l := range fgLines {
		if w := lipgloss.Width(l); w > fgW {
			fgW = w
		}
	}

	// Vertical placement.
	startY := yAnchor
	if startY < 0 {
		startY = (bgH - fgH) / 2
	}
	if startY < 0 {
		startY = 0
	}
	if startY+fgH > bgH {
		// Pad the background with empty lines so the overlay fits.
		extra := startY + fgH - bgH
		for i := 0; i < extra; i++ {
			bgLines = append(bgLines, strings.Repeat(" ", bgW))
		}
	}

	// Horizontal placement (always centred).
	startX := (bgW - fgW) / 2
	if startX < 0 {
		startX = 0
	}

	out := make([]string, len(bgLines))
	copy(out, bgLines)
	for i, fgLine := range fgLines {
		row := startY + i
		if row >= len(out) {
			break
		}
		out[row] = overlayLine(out[row], fgLine, startX)
	}
	return strings.Join(out, "\n")
}

// overlayLine substitutes runes from `fg` into `bg` starting at
// column `at`. It respects ANSI escape sequences in `bg` by
// preserving them outside the substitution window — but for the
// substitution window itself we drop the original styling and let
// `fg` own the colours, which is exactly what we want for a card
// overlay.
//
// Implementation note: we split bg into "left segment" (visible width
// up to `at`), then drop bg's runes for the width of fg, then keep
// the rest of bg starting at `at + width(fg)`. ANSI sequences that
// span the boundary become a minor cosmetic glitch but the visible
// colours stay correct because lipgloss usually emits a reset between
// segments.
func overlayLine(bg, fg string, at int) string {
	fgW := lipgloss.Width(fg)
	left := truncateVisible(bg, at)
	leftW := lipgloss.Width(left)
	// Pad with spaces if the bg row was shorter than `at`.
	if leftW < at {
		left += strings.Repeat(" ", at-leftW)
	}
	right := dropVisible(bg, at+fgW)
	return left + fg + right
}

// truncateVisible returns the prefix of s with at most `width`
// columns of visible content. ANSI escape sequences pass through
// without consuming columns.
func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		rw := runeWidth(r)
		if w+rw > width {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// dropVisible returns the suffix of s starting after `width` visible
// columns. Used to keep the right side of a row visible after
// overlaying content over its middle.
func dropVisible(s string, width int) string {
	if width <= 0 {
		return s
	}
	var b strings.Builder
	w := 0
	inEsc := false
	skipping := true
	for _, r := range s {
		if skipping {
			if inEsc {
				if r == 'm' {
					inEsc = false
				}
				continue
			}
			if r == '\x1b' {
				inEsc = true
				continue
			}
			rw := runeWidth(r)
			w += rw
			if w >= width {
				skipping = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// runeWidth returns the visible cell width of a rune. We treat all
// runes as width 1 here — bb's UI is ASCII-heavy and lipgloss already
// handles the wide-rune cases inside its own measurement helpers
// elsewhere. Good enough for overlay alignment in practice.
func runeWidth(r rune) int {
	if r < 0x20 {
		return 0
	}
	return 1
}
