// Package strutil houses small string / time helpers shared between
// the CLI commands and the TUI. They were duplicated in three or four
// places before — keeping a single canonical implementation here
// avoids the inevitable drift (e.g. one truncate using bytes vs runes,
// the other counting cells).
package strutil

import (
	"fmt"
	"strings"
	"time"
)

// Truncate shortens s to at most n display runes, appending an ellipsis
// when truncation actually happens. n < 1 returns s unchanged. Counting
// runes (not bytes) keeps multi-byte characters from corrupting the
// terminal output.
func Truncate(s string, n int) string {
	if n < 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// HumanTime renders a time.Time as a "x ago" relative string. Used in
// PR / comment / build listings where exact timestamps are noise.
func HumanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// SanitizeForFilename replaces filesystem-unfriendly runes in s with
// '-' so the result is safe to embed in a temp-file name.
func SanitizeForFilename(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ', '\t', '\n':
			return '-'
		}
		return r
	}
	return strings.Map(repl, s)
}
