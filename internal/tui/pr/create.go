// Package pr — Create-PR template flow.
//
// Building and parsing the editor template that backs the C key
// ("create new PR"). The template uses Title/Source/Target headers
// followed by a `# ---` marker and a free-form body so users can
// fill it in inside vim or the inline editor without learning a
// new format.
package pr

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// currentGitBranch returns the current local git branch name (or "" on
// failure). Used to pre-fill the source branch when creating a PR.
func currentGitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// createPRTemplate returns the editor template shown for "create PR".
// The user fills in the bare values on the Title/Source/Target lines and
// writes the description below the marker; everything else is parsed
// from the file by parseCreatePRTemplate.
func createPRTemplate(source, target string) string {
	return fmt.Sprintf(`Title: 
Source: %s
Target: %s

# Edit the values above (Title is required) and write the PR
# description below this marker. Save & exit to submit; leaving
# Title empty cancels.
# ----------------------------------------------------------------

`, source, target)
}

// parseCreatePRTemplate extracts Title / Source / Target / Description
// from the editor buffer produced by createPRTemplate. The description
// is everything after the "# ---" marker line (or after the last header
// line if the marker was removed); leading/trailing whitespace is
// trimmed and standalone "#" comment lines are dropped.
func parseCreatePRTemplate(s string) (title, source, target, description string) {
	lines := strings.Split(s, "\n")
	bodyStart := -1
	for i, line := range lines {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "# ---") {
			bodyStart = i + 1
			break
		}
		if strings.HasPrefix(l, "#") || l == "" {
			continue
		}
		if k, v, ok := splitHeader(l); ok {
			switch strings.ToLower(k) {
			case "title":
				title = v
			case "source":
				source = v
			case "target":
				target = v
			}
		}
	}
	if bodyStart < 0 {
		return title, source, target, ""
	}
	var body []string
	for _, line := range lines[bodyStart:] {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		body = append(body, line)
	}
	description = strings.TrimSpace(strings.Join(body, "\n"))
	return title, source, target, description
}

func splitHeader(s string) (key, value string, ok bool) {
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

// startCreatePR pre-fills the create-PR template with the user's
// current git branch (as source) and the repo's default branch (as
// target), then opens the editor. Pre-fill failures are non-fatal —
// the user can always type the branches in.
func (m *model) startCreatePR() tea.Cmd {
	source := currentGitBranch()
	target := ""
	if r, err := m.svc.GetRepo(m.project, m.slug); err == nil {
		target = r.DefaultRef
	}
	tmpl := createPRTemplate(source, target)
	return editInTUI("create-pr",
		fmt.Sprintf("create-pr-%s-%s", sanitizeForFilename(m.project), sanitizeForFilename(m.slug)),
		0, 0, tmpl)
}

