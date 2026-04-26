// Package theme — title-bar chrome and state-coloured glyphs.
//
// These styles are reassigned by Apply() on theme changes; they MUST
// stay declared as plain `var` (not `const`) so the theme switcher can
// rebind them at runtime without every call site needing to know.
package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// TitleBarPad wraps the whole title bar with a 1-cell horizontal
	// padding so badges don't kiss the terminal edge.
	TitleBarPad = lipgloss.NewStyle().Padding(0, 1)

	// TitleBadge is the bold pill that names the current view
	// ("PULL REQUESTS", "DIFF · PR #42"). High-contrast white on
	// indigo by default; rebound by Apply() per theme.
	TitleBadge = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("57")).
			Padding(0, 1)

	// TitleAccent is a subtle bold accent used inline in headers.
	TitleAccent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141"))

	// TitleSep is the dim dot that separates chips in the title bar.
	// Pre-rendered so callers can string-concat without re-styling.
	TitleSep = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" • ")

	// Title chip variants — bright (TitleChip), muted (TitleChipDim),
	// warn (TitleChipWarn). Rebound per theme.
	TitleChip     = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
	TitleChipDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	TitleChipWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))

	// Status colours used by RenderStatusLine and direct callers
	// (e.g. fatal-error fallbacks).
	StatusOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	StatusErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	StatusInfo = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// TitleBar composes a uniform header line: a coloured badge with the
// section name, followed by optional context "chips" separated by dim
// bullets. Empty chips are skipped so callers can pass conditional
// strings without worrying about double-separators.
func TitleBar(section string, chips ...string) string {
	parts := []string{TitleBadge.Render(section)}
	for _, c := range chips {
		if strings.TrimSpace(c) == "" {
			continue
		}
		parts = append(parts, TitleSep, c)
	}
	return TitleBarPad.Render(strings.Join(parts, ""))
}

// StyleState renders Bitbucket-style state strings (OPEN, MERGED,
// FAILED…) with the canonical colour for that state. Falls back to
// the plain input for unknown states.
func StyleState(s string) string {
	switch strings.ToUpper(s) {
	case "OPEN", "INPROGRESS", "PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(s)
	case "MERGED", "SUCCESSFUL", "SUCCESS":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(s)
	case "DECLINED", "FAILED", "CANCELLED", "STOPPED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(s)
	}
	return s
}

// BuildDot returns a single coloured glyph representing a build state,
// or "" if state is unknown / not yet loaded. Used in PR list / repo
// summary so reviewers can see CI health at a glance.
func BuildDot(state string) string {
	if state == "" {
		return ""
	}
	switch strings.ToUpper(state) {
	case "SUCCESSFUL", "SUCCESS", "PASSED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●")
	case "FAILED", "ERROR", "BROKEN":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●")
	case "INPROGRESS", "RUNNING", "IN_PROGRESS":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("◐")
	case "CANCELLED", "STOPPED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("⊘")
	case "PENDING", "QUEUED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("○")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("·")
	}
}
