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
//
// When the 3270 mainframe theme is active the section name is forced
// to uppercase to match the IBM CICS look — `pull requests` becomes
// `PULL REQUESTS`. Chips are left alone so PR titles and branch
// names keep their original case.
func TitleBar(section string, chips ...string) string {
	if Current.Name == "3270" {
		section = strings.ToUpper(section)
	}
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
//
// Under the 3270 theme the unicode dots are replaced with bracketed
// 4-cell ASCII codes ([OK ], [!! ], [..], [ X ], [ ? ]) — the kind of
// fixed-width status indicators an old CICS panel would print.
func BuildDot(state string) string {
	if state == "" {
		return ""
	}
	mainframe := Current.Name == "3270"
	switch strings.ToUpper(state) {
	case "SUCCESSFUL", "SUCCESS", "PASSED":
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("[OK]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●")
	case "FAILED", "ERROR", "BROKEN":
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("[X ]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●")
	case "INPROGRESS", "RUNNING", "IN_PROGRESS":
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("[..]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("◐")
	case "CANCELLED", "STOPPED":
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[/ ]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("⊘")
	case "PENDING", "QUEUED":
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("[? ]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("○")
	default:
		if mainframe {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[  ]")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("·")
	}
}

// Mainframe reports whether the active theme is the 3270 mainframe
// tribute. Callers use this to swap glyphs / borders / prompts for
// ASCII-only equivalents.
func Mainframe() bool { return Current.Name == "3270" }

// Border returns the border style for cards / panes — single-line
// ASCII (┌─┐│└─┘) under the 3270 theme, soft rounded corners
// otherwise. Centralised so home, repo, and pr stay aligned.
func Border() lipgloss.Border {
	if Mainframe() {
		return lipgloss.NormalBorder()
	}
	return lipgloss.RoundedBorder()
}

// SearchPrompt is the leading glyph of the home/palette search box.
// "===> " is the iconic CICS / TSO command-line prompt; everywhere
// else we keep the friendlier magnifier emoji.
func SearchPrompt() string {
	if Mainframe() {
		return "===> "
	}
	return "🔎 "
}

// OKPrefix / ErrPrefix are the leading marker for transient status
// toasts. Under 3270 we use the classic operator-area prefixes
// ("OK " / "X "); RenderStatusLine uses these to detect colour.
func OKPrefix() string {
	if Mainframe() {
		return "OK "
	}
	return "✓ "
}

func ErrPrefix() string {
	if Mainframe() {
		return "X "
	}
	return "✗ "
}

// VerticalRule returns the single-cell glyph used as a vertical
// separator between panes / blocks. ASCII pipe under 3270, the
// proper unicode rule otherwise.
func VerticalRule() string {
	if Mainframe() {
		return "|"
	}
	return "│"
}

// AccentRule is a 1-cell solid block used to mark the left edge of
// dashboard sections. Plain ASCII pipe under 3270.
func AccentRule() string {
	if Mainframe() {
		return "|"
	}
	return "▎"
}

// ActiveTabUnderline is the glyph repeated under the active tab to
// give the eye a pinned anchor. ASCII '=' for 3270 (matches the
// title-bar underline style of old CICS panels), upper-eighth block
// otherwise.
func ActiveTabUnderline() string {
	if Mainframe() {
		return "="
	}
	return "▔"
}

// BranchGlyph is the small ⎇ icon used when chipping a branch name.
// Mainframe terminals can't render the icon, so a literal "BR:" is
// used instead.
func BranchGlyph() string {
	if Mainframe() {
		return "BR:"
	}
	return "⎇ "
}
