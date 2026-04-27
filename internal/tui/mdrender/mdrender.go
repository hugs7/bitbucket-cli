// Package mdrender is a tiny shared wrapper around Charm's glamour
// markdown renderer used by the home and repo TUIs to draw README
// bodies. Both surfaces previously rolled their own (one was a
// no-op showing raw markdown, the other a hand-rolled three-rule
// fallback) — funnelling them through a single function keeps the
// styling consistent and lets us swap implementations later without
// chasing call-sites.
package mdrender

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
)

// Render returns body rendered as styled markdown sized for width
// columns. On any error (renderer init failure, parse error, …) we
// return the original body so callers always get something readable
// — a degraded README is better than an empty preview.
//
// The width parameter is the soft-wrap target for code blocks /
// paragraphs; pass the available preview pane width. width<=0 falls
// back to a sensible default so callers don't need to special-case
// the very first paint before WindowSizeMsg.
func Render(body string, width int) string {
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return ""
	}
	if width <= 0 {
		width = 80
	}
	// glamour.WithAutoStyle relies on termenv's terminal-color
	// detection (OSC 11 / DA1 query). Inside our bubbletea TUIs that
	// detection is unreliable — bubbletea owns /dev/tty in raw mode
	// and consumes the terminal's response before termenv can read
	// it, so glamour falls back to its no-color "ascii" style and
	// READMEs render as literal `# headings` and `**bold**` markup.
	// Force the dark style by default (bb's chrome is dark across
	// every theme) and let power users override with GLAMOUR_STYLE
	// the same way gh / glow do.
	style := os.Getenv("GLAMOUR_STYLE")
	if style == "" {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return body
	}
	out, err := r.Render(body)
	if err != nil {
		return body
	}
	// Glamour pads the output with a leading + trailing newline by
	// default; trim them so callers can compose the result with their
	// own headers / blank lines without spurious gaps.
	return strings.Trim(out, "\n")
}
