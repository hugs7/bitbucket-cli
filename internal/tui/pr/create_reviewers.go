package pr

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/config"
)

// ChooseReviewers runs the final creation step after source and target are
// settled. Repository defaults start checked; searching preserves selections
// and issues one directory request per submitted query, never per keystroke.
// A non-nil initial slice replaces defaults, including an explicitly empty one.
func ChooseReviewers(svc api.Service, project, slug, source, target string, initial []string, in io.Reader, out io.Writer) ([]string, error) {
	users := []api.User{}
	selected := append([]string{}, initial...)
	if initial == nil {
		var err error
		users, err = api.DefaultReviewers(svc, project, slug, source, target)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			selected = append(selected, user.Username)
		}
	} else {
		for _, name := range initial {
			users = append(users, api.User{Username: name})
		}
	}
	for _, user := range config.Get().RecentReviewers[svc.Host()] {
		users = append(users, api.User{Username: user.Username, DisplayName: user.DisplayName})
	}
	notice := "Repository defaults are checked. Space toggles; submit with an empty search to create."
	for {
		var query, extra string
		options := reviewerOptions(users, svc.Me())
		fields := []huh.Field{}
		if len(options) > 0 {
			fields = append(fields, huh.NewMultiSelect[string]().Title("Reviewers").
				Description(notice).Options(options...).Value(&selected).Height(8).Filterable(true))
		} else {
			fields = append(fields, huh.NewNote().Title("Reviewers").Description("No suggested reviewers. Search the directory or enter usernames below."))
		}
		fields = append(fields,
			huh.NewInput().Title("Additional usernames / UUIDs (comma-separated, optional)").Value(&extra),
			huh.NewInput().Title("Find more reviewers (optional)").
				Description("Enter a name and submit to load matches; leave blank to finish.").Value(&query),
		)
		if err := huh.NewForm(huh.NewGroup(fields...)).WithInput(in).WithOutput(out).Run(); err != nil {
			return nil, err
		}
		for _, name := range strings.Split(extra, ",") {
			if name = strings.TrimSpace(name); name != "" {
				selected = append(selected, name)
				users = append(users, api.User{Username: name})
			}
		}
		if strings.TrimSpace(query) == "" {
			return append([]string{}, selected...), nil
		}
		matches, err := svc.SearchUsers(strings.TrimSpace(query), 50)
		if err != nil {
			notice = "Search failed: " + err.Error() + ". Your selections are preserved."
			continue
		}
		users = append(users, matches...)
		notice = fmt.Sprintf("%d matches added for %q. Space toggles; blank search finishes.", len(matches), query)
	}
}

// reviewerOptions retains server ordering while collapsing repeated identities.
func reviewerOptions(users []api.User, author string) []huh.Option[string] {
	seen := map[string]bool{strings.ToLower(author): true, "": true}
	options := []huh.Option[string]{}
	for _, user := range users {
		name := strings.TrimSpace(user.Username)
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		label := name
		if user.DisplayName != "" {
			label = user.DisplayName + " (" + name + ")"
		}
		options = append(options, huh.NewOption(label, name))
	}
	return options
}
