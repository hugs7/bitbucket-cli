// Package pr — Add/Remove reviewer flow with debounced directory search.
//
// 'v' (Add Reviewer) and 'V' (Remove Reviewer) open a centred overlay
// with a textinput at the top and a results list below. As the user
// types, a 200ms debounced SearchUsers call refreshes the results —
// the same pattern Bitbucket's web UI uses ("type three letters,
// wait, results appear"). Space toggles selection; enter submits;
// esc cancels.
//
// Add mode pulls candidates from svc.SearchUsers (Bitbucket Server
// reviewer directory). Remove mode operates entirely on the PR's
// existing reviewer list — no API roundtrip needed.
//
// On Bitbucket Cloud SearchUsers is a stub returning (nil, nil); the
// flow falls back to the legacy free-text editor in startAddReviewer.
package pr

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hugs7/bitbucket-cli/internal/api"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// reviewerSearchDebounce is how long we wait after the last keystroke
// before firing the SearchUsers call. Matches the home dashboard's
// hot-search debounce so the rhythm feels consistent across screens.
const reviewerSearchDebounce = 200 * time.Millisecond

// reviewerSearchState holds everything the reviewer-search overlay
// needs: the input, the current result set, the user's selections,
// and a search-version counter so stale debounced ticks can be
// discarded when newer keystrokes have superseded them.
type reviewerSearchState struct {
	prID   int
	remove bool

	input   textinput.Model
	loading bool
	err     error

	// results is the current candidate list shown below the input.
	// In add mode it's whatever SearchUsers returned for the latest
	// query; in remove mode it's the PR's reviewers filtered by
	// the input substring.
	results []api.User
	cursor  int

	// selected is the set of usernames the user has toggled on.
	// Map (rather than slice) so toggling stays O(1); the value is
	// the human label for the toast.
	selected map[string]string

	// searchVersion increments on every keystroke so stale debounced
	// ticks return early and don't overwrite fresher results.
	searchVersion int

	// existing is the lower-cased usernames already on the PR. In
	// add mode they're filtered out of the candidate list; in
	// remove mode it doubles as the source list.
	existing map[string]struct{}

	// allReviewers (remove mode only) is the static list we filter
	// client-side as the user types.
	allReviewers []api.Reviewer
}

// reviewerSearchTickMsg is posted by tea.Tick after the debounce
// window. The handler ignores it if a newer keystroke has bumped
// searchVersion, otherwise it fires the API call.
type reviewerSearchTickMsg struct {
	version int
	query   string
}

// reviewerSearchResultsMsg carries SearchUsers results back to the
// model. version lets us discard responses for queries that have
// since been superseded by newer keystrokes.
type reviewerSearchResultsMsg struct {
	version int
	users   []api.User
	err     error
}

// startAddReviewer enters the reviewer-search overlay for adding new
// reviewers. Pre-populates with whatever SearchUsers returns for an
// empty query (Server: alphabetical first 50). Falls back to the
// legacy free-text editor on Cloud, where SearchUsers is unimplemented.
func (m *model) startAddReviewer(prID int) tea.Cmd {
	users, err := m.svc.SearchUsers("", 50)
	if err != nil {
		m.status = "user search failed (" + err.Error() + ") — falling back to free text"
		return editInTUI("add-reviewer",
			fmt.Sprintf("pr-%d-add-reviewer", prID), prID, 0,
			"# Enter one or more usernames (Server) or UUIDs/emails\n"+
				"# (Cloud), separated by space or comma.\n")
	}
	if len(users) == 0 {
		// Cloud's SearchUsers stub returns (nil, nil); use the
		// legacy editor flow so the action still works.
		return editInTUI("add-reviewer",
			fmt.Sprintf("pr-%d-add-reviewer", prID), prID, 0,
			"# Enter one or more usernames (Server) or UUIDs/emails\n"+
				"# (Cloud), separated by space or comma.\n")
	}

	existing := map[string]struct{}{}
	if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.ID == prID {
		for _, r := range it.pr.Reviewers {
			existing[strings.ToLower(r.Username)] = struct{}{}
		}
	}
	users = filterOutExisting(users, existing)

	ti := textinput.New()
	ti.Placeholder = "type a name, username or email…"
	ti.Prompt = theme.SearchPrompt()
	ti.CharLimit = 80
	ti.Focus()

	m.reviewerSearch = &reviewerSearchState{
		prID:     prID,
		remove:   false,
		input:    ti,
		results:  users,
		cursor:   0,
		selected: map[string]string{},
		existing: existing,
	}
	m.mode = viewReviewerSearch
	return textinput.Blink
}

// startRemoveReviewer enters the reviewer-search overlay for removing
// existing reviewers. Operates on the PR's own reviewer list — no API
// roundtrip needed — and filters that list client-side as the user
// types.
func (m *model) startRemoveReviewer(prID int, reviewers []api.Reviewer) tea.Cmd {
	if len(reviewers) == 0 {
		m.status = "no reviewers to remove"
		return nil
	}

	ti := textinput.New()
	ti.Placeholder = "filter current reviewers…"
	ti.Prompt = theme.SearchPrompt()
	ti.CharLimit = 80
	ti.Focus()

	m.reviewerSearch = &reviewerSearchState{
		prID:         prID,
		remove:       true,
		input:        ti,
		cursor:       0,
		selected:     map[string]string{},
		allReviewers: reviewers,
	}
	m.reviewerSearch.results = reviewersToUsers(reviewers)
	m.mode = viewReviewerSearch
	return textinput.Blink
}

// reviewersToUsers converts the unified Reviewer slice into User shape
// so the rendering code can treat add/remove modes uniformly.
func reviewersToUsers(rs []api.Reviewer) []api.User {
	out := make([]api.User, 0, len(rs))
	for _, r := range rs {
		out = append(out, api.User{
			Username:    r.Username,
			DisplayName: r.DisplayName,
		})
	}
	return out
}

// filterOutExisting drops users whose username matches a key in the
// existing-reviewer set (case-insensitive). Used in add mode so the
// user can't accidentally re-add someone already on the PR.
func filterOutExisting(users []api.User, existing map[string]struct{}) []api.User {
	out := make([]api.User, 0, len(users))
	for _, u := range users {
		if _, dup := existing[strings.ToLower(u.Username)]; dup {
			continue
		}
		out = append(out, u)
	}
	return out
}

// scheduleReviewerSearch increments searchVersion and emits a
// debounced tick. Only the latest tick (matching the current
// version) actually fires the API call; older in-flight ticks are
// dropped, giving us cancellation for free.
func (m *model) scheduleReviewerSearch() tea.Cmd {
	if m.reviewerSearch == nil {
		return nil
	}
	m.reviewerSearch.searchVersion++
	v := m.reviewerSearch.searchVersion
	q := strings.TrimSpace(m.reviewerSearch.input.Value())
	return tea.Tick(reviewerSearchDebounce, func(time.Time) tea.Msg {
		return reviewerSearchTickMsg{version: v, query: q}
	})
}

// runReviewerSearchNow calls SearchUsers immediately. Used by the
// tick handler once the debounce window has elapsed.
func (m *model) runReviewerSearchNow(version int, query string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		users, err := svc.SearchUsers(query, 50)
		return reviewerSearchResultsMsg{version: version, users: users, err: err}
	}
}

// applyReviewerFilter is the remove-mode equivalent of a SearchUsers
// call: it filters the static reviewer list by the current input
// substring (case-insensitive against display name and username).
func (s *reviewerSearchState) applyReviewerFilter() {
	q := strings.ToLower(strings.TrimSpace(s.input.Value()))
	if q == "" {
		s.results = reviewersToUsers(s.allReviewers)
	} else {
		out := make([]api.User, 0, len(s.allReviewers))
		for _, r := range s.allReviewers {
			if strings.Contains(strings.ToLower(r.Username), q) ||
				strings.Contains(strings.ToLower(r.DisplayName), q) {
				out = append(out, api.User{Username: r.Username, DisplayName: r.DisplayName})
			}
		}
		s.results = out
	}
	if s.cursor >= len(s.results) {
		s.cursor = len(s.results) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// handleReviewerSearchKey owns every keystroke while the overlay is
// open. Returns the model + any cmd to dispatch.
func (m model) handleReviewerSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.reviewerSearch
	if s == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.reviewerSearch = nil
		m.mode = viewDetail
		m.status = "reviewer change cancelled"
		return m, nil

	case "up", "ctrl+p":
		if s.cursor > 0 {
			s.cursor--
		}
		return m, nil

	case "down", "ctrl+n":
		if s.cursor < len(s.results)-1 {
			s.cursor++
		}
		return m, nil

	case "tab":
		// Toggle the user under the cursor without leaving the form
		// so multi-select is one tab-tap per pick. Space is reserved
		// for the search input so users can type display names with
		// spaces (e.g. "Alice Smith").
		if s.cursor >= 0 && s.cursor < len(s.results) {
			u := s.results[s.cursor]
			if _, ok := s.selected[u.Username]; ok {
				delete(s.selected, u.Username)
			} else {
				s.selected[u.Username] = formatUserLabel(u)
			}
		}
		return m, nil

	case "enter":
		// If nothing was toggled with space, treat enter on the
		// cursor as "pick this one and submit" — the common case
		// for "I just want to add Alice".
		if len(s.selected) == 0 && s.cursor >= 0 && s.cursor < len(s.results) {
			u := s.results[s.cursor]
			s.selected[u.Username] = formatUserLabel(u)
		}
		if len(s.selected) == 0 {
			m.status = "no reviewers selected"
			return m, nil
		}
		usernames := make([]string, 0, len(s.selected))
		labels := make([]string, 0, len(s.selected))
		for u, l := range s.selected {
			usernames = append(usernames, u)
			labels = append(labels, l)
		}
		prID := s.prID
		remove := s.remove
		m.reviewerSearch = nil
		m.mode = viewDetail
		m.loading = true
		if remove {
			return m, tea.Batch(m.spinner.Tick, m.doAction(
				fmt.Sprintf("removed reviewer(s) %s", strings.Join(labels, ", ")), true, func() error {
					return m.svc.RemoveReviewers(m.project, m.slug, prID, usernames)
				}))
		}
		return m, tea.Batch(m.spinner.Tick, m.doAction(
			fmt.Sprintf("added reviewer(s) %s", strings.Join(labels, ", ")), true, func() error {
				return m.svc.AddReviewers(m.project, m.slug, prID, usernames)
			}))

	case "ctrl+u":
		s.input.SetValue("")
	}

	// Forward everything else (printable chars, backspace, etc) to
	// the textinput. Anything that mutates the buffer schedules a
	// debounced search.
	prev := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if s.input.Value() != prev {
		if s.remove {
			s.applyReviewerFilter()
			return m, cmd
		}
		// Add mode: kick off a debounced API call.
		s.loading = true
		return m, tea.Batch(cmd, m.spinner.Tick, m.scheduleReviewerSearch())
	}
	return m, cmd
}

// formatUserLabel produces "Display Name (username) <email>" — used
// in the toast and in selection set so the user sees a friendly name
// rather than a UUID.
func formatUserLabel(u api.User) string {
	parts := []string{}
	if u.DisplayName != "" {
		parts = append(parts, u.DisplayName)
	}
	if u.Username != "" && u.Username != u.DisplayName {
		parts = append(parts, "("+u.Username+")")
	}
	if u.Email != "" {
		parts = append(parts, "<"+u.Email+">")
	}
	if len(parts) == 0 {
		return u.Username
	}
	return strings.Join(parts, " ")
}

// renderReviewerSearch draws the centred overlay: a bordered box with
// the input on top, the results list in the middle, and a help line
// at the bottom.
func (m model) renderReviewerSearch() string {
	s := m.reviewerSearch
	if s == nil {
		return ""
	}
	width := editorBoxInnerWidth(m.width) + 2
	innerW := width - 2

	title := "ADD REVIEWER"
	if s.remove {
		title = "REMOVE REVIEWER"
	}

	header := theme.TitleBadge.Render(" "+title+" ") + "  " +
		theme.TitleChipDim.Render("tab toggle · enter submit · esc cancel · ctrl+u clear")

	inputView := s.input.View()

	statusLine := ""
	switch {
	case s.loading:
		statusLine = theme.TitleChipDim.Render(m.spinner.View() + " searching…")
	case s.err != nil:
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: " + s.err.Error())
	case len(s.results) == 0:
		statusLine = theme.TitleChipDim.Render("(no matches)")
	default:
		statusLine = theme.TitleChipDim.Render(fmt.Sprintf("%d match%s · %d selected",
			len(s.results), plural(len(s.results)), len(s.selected)))
	}

	rows := make([]string, 0, len(s.results))
	maxRows := 12
	if maxRows > len(s.results) {
		maxRows = len(s.results)
	}
	// Window the results around the cursor so it stays visible when
	// the list is longer than maxRows. Simple "scroll-to-keep-cursor-
	// visible" — no fancy easing.
	start := 0
	if s.cursor >= maxRows {
		start = s.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(s.results) {
		end = len(s.results)
	}
	for i := start; i < end; i++ {
		u := s.results[i]
		marker := "  "
		if _, picked := s.selected[u.Username]; picked {
			marker = "✓ "
		}
		line := marker + formatUserLabel(u)
		if i == s.cursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("57")).
				Foreground(lipgloss.Color("231")).
				Bold(true).
				Width(innerW - 4).
				Render(line)
		}
		rows = append(rows, line)
	}
	if len(s.results) > maxRows {
		rows = append(rows, theme.TitleChipDim.Render(
			fmt.Sprintf("(%d more — keep typing to narrow)", len(s.results)-maxRows)))
	}

	body := header + "\n\n" +
		inputView + "\n" +
		statusLine + "\n\n" +
		strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color("57")).
		Padding(1, 2).
		Width(width)

	return lipgloss.Place(m.width, m.height-2,
		lipgloss.Center, lipgloss.Center,
		box.Render(body),
	)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
