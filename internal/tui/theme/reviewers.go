package theme

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
)

// ApprovalSummary returns a compact "✓ approved/total" chip, plus a
// "✗ n" needs-work count, for PR rows; "" when there are no reviewers.
func ApprovalSummary(reviewers []api.Reviewer) string {
	if len(reviewers) == 0 {
		return ""
	}
	approved, needsWork := 0, 0
	for _, r := range reviewers {
		switch reviewerStatus(r) {
		case "APPROVED":
			approved++
		case "NEEDS_WORK":
			needsWork++
		}
	}
	color := "11"
	if approved == len(reviewers) {
		color = "10"
	}
	out := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).
		Render(fmt.Sprintf("✓ %d/%d", approved, len(reviewers)))
	if needsWork > 0 {
		out += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
			Render(fmt.Sprintf("✗ %d", needsWork))
	}
	return out
}

// Reviewers renders a "Reviewers" block listing each reviewer's
// status, or "" when there are none.
func Reviewers(reviewers []api.Reviewer) string {
	if len(reviewers) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintln(&sb, lipgloss.NewStyle().Bold(true).Render("Reviewers"))
	for _, r := range reviewers {
		fmt.Fprintf(&sb, "  %s  %s\n", reviewerStatusLabel(r), reviewerName(r))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func reviewerName(r api.Reviewer) string {
	if strings.TrimSpace(r.DisplayName) == "" {
		return r.Username
	}
	if strings.TrimSpace(r.Username) == "" || r.Username == r.DisplayName {
		return r.DisplayName
	}
	return fmt.Sprintf("%s (%s)", r.DisplayName, r.Username)
}

// reviewerStatus normalises a reviewer's state to APPROVED,
// NEEDS_WORK, or PENDING.
func reviewerStatus(r api.Reviewer) string {
	s := strings.ToUpper(strings.TrimSpace(r.Status))
	if s == "" && r.Approved {
		s = "APPROVED"
	}
	switch s {
	case "APPROVED":
		return "APPROVED"
	case "NEEDS_WORK", "CHANGES_REQUESTED":
		return "NEEDS_WORK"
	}
	return "PENDING"
}

func reviewerStatusLabel(r api.Reviewer) string {
	switch reviewerStatus(r) {
	case "APPROVED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓ approved")
	case "NEEDS_WORK":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("✗ needs work")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("~ pending")
}
