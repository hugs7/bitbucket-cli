// Package preview composes README / markdown preview bodies for the
// home and repo TUIs.
//
// Both surfaces previously inlined the same "render markdown, fall
// back to muted placeholder text, optionally prefix a description"
// pattern in slightly different shapes. Centralising it here keeps
// their visual treatment of READMEs identical (so a repo viewed via
// the dashboard preview and via `bb .` looks the same), and gives us
// one place to evolve the styling when we eventually swap glamour
// for a different renderer or introduce light-theme handling.
package preview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/tui/mdrender"
)

// Muted is the canonical "hint" foreground for placeholder /
// secondary text in preview panes. Exported so call-sites that need
// to render their own one-off muted strings (e.g. the home empty
// states) stay colour-consistent with everything else here.
var Muted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

// Description is the style used for the optional one-line summary
// that prefixes a rendered README — same hue as Muted so it sits
// quietly above the body, plus italic so the eye reads it as
// metadata rather than content.
var Description = Muted.Italic(true)

// Body composes the README pane's content for a single repo.
//
//   - When body is non-empty it's rendered as styled markdown sized
//     for width.
//   - When body is blank the fallback string is wrapped in Muted so
//     the pane never collapses to nothing.
//   - When description is non-empty it's prepended in italic + muted
//     so the user always has a one-line summary above the README.
func Body(body, description string, width int, fallback string) string {
	var rendered string
	if strings.TrimSpace(body) == "" {
		rendered = Muted.Render(fallback)
	} else {
		rendered = mdrender.Render(body, width)
	}
	if description != "" {
		rendered = Description.Render(description) + "\n\n" + rendered
	}
	return rendered
}
