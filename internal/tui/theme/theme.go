// Package theme is the shared visual layer used by every bb TUI:
// the named colour themes plus the small cohort of styles (title bars,
// status chips, build dots) that every page renders. Pulled out into
// its own importable package so home / repo / pr can stay independent
// while still drawing the same chrome.
package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/config"
)

// Theme is a small palette plus a name. Colours are lipgloss.Color
// values (any of "9", "#ff0000", lipgloss.AdaptiveColor as a string).
type Theme struct {
	Name string

	StatusOK   lipgloss.Color
	StatusErr  lipgloss.Color
	StatusInfo lipgloss.Color

	TitleChip     lipgloss.Color
	TitleChipDim  lipgloss.Color
	TitleChipWarn lipgloss.Color
	TitleBadgeBg  lipgloss.Color
	TitleBadgeFg  lipgloss.Color

	Branch lipgloss.Color
	Author lipgloss.Color
}

// builtinThemes is the registry of named themes shipped with bb.
// Add new ones here and they're automatically available via the
// palette cycler and the `theme:` config field.
var builtinThemes = []Theme{
	{
		Name:          "default",
		StatusOK:      lipgloss.Color("10"),
		StatusErr:     lipgloss.Color("9"),
		StatusInfo:    lipgloss.Color("11"),
		TitleChip:     lipgloss.Color("159"),
		TitleChipDim:  lipgloss.Color("245"),
		TitleChipWarn: lipgloss.Color("221"),
		TitleBadgeBg:  lipgloss.Color("57"),
		TitleBadgeFg:  lipgloss.Color("231"),
		Branch:        lipgloss.Color("14"),
		Author:        lipgloss.Color("13"),
	},
	{
		Name:          "dracula",
		StatusOK:      lipgloss.Color("#50fa7b"),
		StatusErr:     lipgloss.Color("#ff5555"),
		StatusInfo:    lipgloss.Color("#f1fa8c"),
		TitleChip:     lipgloss.Color("#8be9fd"),
		TitleChipDim:  lipgloss.Color("#6272a4"),
		TitleChipWarn: lipgloss.Color("#ffb86c"),
		TitleBadgeBg:  lipgloss.Color("#bd93f9"),
		TitleBadgeFg:  lipgloss.Color("#282a36"),
		Branch:        lipgloss.Color("#8be9fd"),
		Author:        lipgloss.Color("#ff79c6"),
	},
	{
		Name:          "solarized-dark",
		StatusOK:      lipgloss.Color("#859900"),
		StatusErr:     lipgloss.Color("#dc322f"),
		StatusInfo:    lipgloss.Color("#b58900"),
		TitleChip:     lipgloss.Color("#2aa198"),
		TitleChipDim:  lipgloss.Color("#586e75"),
		TitleChipWarn: lipgloss.Color("#cb4b16"),
		TitleBadgeBg:  lipgloss.Color("#268bd2"),
		TitleBadgeFg:  lipgloss.Color("#fdf6e3"),
		Branch:        lipgloss.Color("#2aa198"),
		Author:        lipgloss.Color("#d33682"),
	},
	{
		Name:          "nord",
		StatusOK:      lipgloss.Color("#a3be8c"),
		StatusErr:     lipgloss.Color("#bf616a"),
		StatusInfo:    lipgloss.Color("#ebcb8b"),
		TitleChip:     lipgloss.Color("#88c0d0"),
		TitleChipDim:  lipgloss.Color("#4c566a"),
		TitleChipWarn: lipgloss.Color("#d08770"),
		TitleBadgeBg:  lipgloss.Color("#5e81ac"),
		TitleBadgeFg:  lipgloss.Color("#eceff4"),
		Branch:        lipgloss.Color("#8fbcbb"),
		Author:        lipgloss.Color("#b48ead"),
	},
	{
		// IBM 3270 / Reflection green-screen tribute. Bright cyan
		// for protected fields, bright green for the operator
		// status line, bright red for errors, bright yellow for
		// attention/warnings — same palette every Westpac mainframe
		// terminal has shipped since the 80s. Pair with a black
		// terminal background and a monospaced font for full effect.
		Name:          "3270",
		StatusOK:      lipgloss.Color("10"), // bright green (operator status)
		StatusErr:     lipgloss.Color("9"),  // bright red (X SYSTEM error)
		StatusInfo:    lipgloss.Color("14"), // bright cyan
		TitleChip:     lipgloss.Color("14"), // bright cyan (protected field)
		TitleChipDim:  lipgloss.Color("6"),  // dim cyan
		TitleChipWarn: lipgloss.Color("11"), // bright yellow (attention)
		TitleBadgeBg:  lipgloss.Color("14"),
		TitleBadgeFg:  lipgloss.Color("0"), // black on cyan
		Branch:        lipgloss.Color("14"),
		Author:        lipgloss.Color("10"),
	},
}

// Current is the in-process active theme. Read by Apply on init and
// by the palette cycler. Style variables (TitleChip, StatusOK, …) are
// rebound by Apply so call sites just reference the package-level
// vars without ever needing to know about the current theme.
var Current = builtinThemes[0]

// ChangedMsg is emitted by the palette so models can flash a toast
// (and so theme changes integrate cleanly into the bubbletea event
// loop instead of mutating styles mid-View).
type ChangedMsg struct{ Name string }

// Lookup returns the named theme, or the default theme when the name
// is unknown / empty.
func Lookup(name string) Theme {
	if name == "" {
		return builtinThemes[0]
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, t := range builtinThemes {
		if strings.ToLower(t.Name) == want {
			return t
		}
	}
	return builtinThemes[0]
}

// Apply swaps the active theme and rebinds the package-level style
// variables so subsequent View() calls pick up the new colours
// without callers needing to refactor every render call.
func Apply(t Theme) {
	Current = t

	StatusOK = lipgloss.NewStyle().Foreground(t.StatusOK)
	StatusErr = lipgloss.NewStyle().Foreground(t.StatusErr)
	StatusInfo = lipgloss.NewStyle().Foreground(t.StatusInfo)

	TitleChip = lipgloss.NewStyle().Foreground(t.TitleChip)
	TitleChipDim = lipgloss.NewStyle().Foreground(t.TitleChipDim)
	TitleChipWarn = lipgloss.NewStyle().Foreground(t.TitleChipWarn)
	TitleBadge = lipgloss.NewStyle().Bold(true).
		Foreground(t.TitleBadgeFg).Background(t.TitleBadgeBg).Padding(0, 1)
	sep := " • "
	if t.Name == "3270" {
		// CICS panels separate fields with double-bars, not bullets.
		sep = " || "
	}
	TitleSep = lipgloss.NewStyle().Foreground(t.TitleChipDim).Render(sep)
}

// Next returns the theme name following `current` in the cycle,
// wrapping around at the end. Drives the palette's "Switch theme"
// item.
func Next(current string) string {
	for i, t := range builtinThemes {
		if strings.EqualFold(t.Name, current) {
			return builtinThemes[(i+1)%len(builtinThemes)].Name
		}
	}
	return builtinThemes[0].Name
}

// Init applies whichever theme the user has configured. Called once
// per TUI launch from each model constructor so the first paint
// already shows the chosen palette.
func Init() {
	Apply(Lookup(config.Get().Theme))
}
