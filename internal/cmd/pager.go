package cmd

import (
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// withPager returns a writer that streams to the user's pager when stdout
// is a TTY. It returns the writer plus a cleanup function that the caller
// MUST defer-call (waits for the pager to exit). When not a TTY, it just
// returns os.Stdout and a no-op cleanup.
func withPager() (io.Writer, func()) {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return os.Stdout, func() {}
	}
	pager := os.Getenv("BB_PAGER")
	if pager == "" {
		pager = os.Getenv("PAGER")
	}
	if pager == "" {
		pager = "less"
	}
	if pager == "cat" || pager == "" {
		return os.Stdout, func() {}
	}

	args := strings.Fields(pager)
	cmd := exec.Command(args[0], args[1:]...)
	// -R: pass ANSI colour through. -F: quit if one screen. -X: don't clear screen on exit.
	if args[0] == "less" && len(args) == 1 {
		cmd = exec.Command("less", "-R", "-F", "-X")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	w, err := cmd.StdinPipe()
	if err != nil {
		return os.Stdout, func() {}
	}
	if err := cmd.Start(); err != nil {
		return os.Stdout, func() {}
	}
	return w, func() {
		_ = w.Close()
		_ = cmd.Wait()
	}
}

// useColor reports whether ANSI colour should be emitted to stdout.
// Honours NO_COLOR (https://no-color.org).
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd()) || os.Getenv("BB_FORCE_COLOR") != ""
}

// colorizeDiff applies ANSI colours to a unified diff. When colour is
// disabled it returns the input unchanged.
func colorizeDiff(diff string) string {
	if !useColor() {
		return diff
	}
	add := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	del := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hunk := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	meta := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))

	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			b.WriteString(meta.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(hunk.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(add.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(del.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
