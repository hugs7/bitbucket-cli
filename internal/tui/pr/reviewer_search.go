// Package pr — Add/Remove reviewer flow with debounced directory search.
//
// 'v' (Add Reviewer) and 'V' (Remove Reviewer) open a centred overlay
// with a textinput at the top and a results list below. As the user
// types, a 200ms debounced SearchUsers call refreshes the results —
// the same pattern Bitbucket's web UI uses ("type three letters,
// wait, results appear").
//
// Key bindings:
//   - enter         → add the highlighted user to the picked set,
//                     clear the input, and stay in the modal so
//                     the user can pick another. tab does the same.
//   - ctrl+enter / ctrl+s → submit the picked set (call the API)
//   - esc / ctrl+c  → cancel
//   - ctrl+u        → clear input
//
// The pane shows three sections:
//   - "On this PR"     — current reviewers (info / dedup hint in
//                        add mode; the source list in remove mode)
//   - "Common reviewers" — recently-used reviewers from per-host
//                          config history (add mode, empty query only)
//   - "Search results" — live SearchUsers results / filtered remove list
//   - "Picked"         — what will be sent on submit
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
	"github.com/hugs7/bitbucket-cli/internal/config"
	"github.com/hugs7/bitbucket-cli/internal/tui/theme"
)

// reviewerSearchDebounce is how long we wait after the last keystroke
// before firing the SearchUsers call. Matches the home dashboard's
// hot-search debounce so the rhythm feels consistent across screens.
const reviewerSearchDebounce = 200 * time.Millisecond

// reviewerSearchState holds everything the reviewer-search overlay
// needs: the input, the current result set, the user's picks, and a
// search-version counter so stale debounced ticks can be discarded
// when newer keystrokes have superseded them.
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

	// picked is the ordered list of users the user has chosen so
	// far. Slice (not map) so the pane can render them in the
	// order they were added.
	picked []api.User

	// searchVersion increments on every keystroke so stale debounced
	// ticks return early and don't overwrite fresher results.
	searchVersion int

	// existing is the lower-cased usernames already on the PR. In
	// add mode they're filtered out of the candidate list; in
	// remove mode it doubles as the source list.
	existing map[string]struct{}

	// existingDisplay is the original PR reviewer list, kept around
	// so the "On this PR" pane can show display names.
	existingDisplay []api.Reviewer

	// allReviewers (remove mode only) is the static list we filter
	// client-side as the user types.
	allReviewers []api.Reviewer

	// recents is the per-host common-reviewers list from config,
	// merged into the top of the results list when the input is
	// empty so the user can pick a frequent collaborator with a
	// single Enter. recentSet is the lower-cased username set
	// used to render a ★ marker against those rows.
	recents   []config.RecentReviewer
	recentSet map[string]struct{}
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
// since been superseded by newer keystrokes; query lets the handler
// decide whether to merge recents (empty query only).
type reviewerSearchResultsMsg struct {
	version int
	query   string
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
	var existingDisplay []api.Reviewer
	if it, ok := m.list.SelectedItem().(prItem); ok && it.pr.ID == prID {
		existingDisplay = it.pr.Reviewers
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

	recents := filterRecentsAgainstExisting(config.RecentReviewers(m.svc.Host()), existing)
	recentSet := map[string]struct{}{}
	for _, r := range recents {
		recentSet[strings.ToLower(r.Username)] = struct{}{}
	}

	m.reviewerSearch = &reviewerSearchState{
		prID:            prID,
		remove:          false,
		input:           ti,
		cursor:          0,
		picked:          nil,
		existing:        existing,
		existingDisplay: existingDisplay,
		recents:         recents,
		recentSet:       recentSet,
	}
	m.reviewerSearch.results = mergeRecentsWithUsers(recents, users)
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
		prID:            prID,
		remove:          true,
		input:           ti,
		cursor:          0,
		picked:          nil,
		allReviewers:    reviewers,
		existingDisplay: reviewers,
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

// filterRecentsAgainstExisting hides recents that are already on the PR.
func filterRecentsAgainstExisting(recents []config.RecentReviewer, existing map[string]struct{}) []config.RecentReviewer {
	if len(recents) == 0 {
		return nil
	}
	out := make([]config.RecentReviewer, 0, len(recents))
	for _, r := range recents {
		if _, dup := existing[strings.ToLower(r.Username)]; dup {
			continue
		}
		out = append(out, r)
	}
	return out
}

// mergeRecentsWithUsers prepends recents (as User shape) to a
// directory-search result slice, deduping by lower-cased username so
// a recent reviewer who also appears in the API response only shows
// once. Used to make the "common reviewers" list pickable via the
// same cursor that navigates search results.
func mergeRecentsWithUsers(recents []config.RecentReviewer, users []api.User) []api.User {
	if len(recents) == 0 {
		return users
	}
	out := make([]api.User, 0, len(recents)+len(users))
	seen := map[string]struct{}{}
	for _, r := range recents {
		key := strings.ToLower(r.Username)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, api.User{
			Username:    r.Username,
			DisplayName: r.DisplayName,
			Email:       r.Email,
		})
	}
	for _, u := range users {
		key := strings.ToLower(u.Username)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
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
		return reviewerSearchResultsMsg{version: version, query: query, users: users, err: err}
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
	s.clampCursor()
}

// clampCursor keeps the cursor within the visible result range.
func (s *reviewerSearchState) clampCursor() {
	if s.cursor >= len(s.results) {
		s.cursor = len(s.results) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// pickByUsername adds a user to the picked list, deduping on username.
func (s *reviewerSearchState) pickByUsername(u api.User) {
	for _, p := range s.picked {
		if p.Username == u.Username {
			return
		}
	}
	s.picked = append(s.picked, u)
}

// pickFromCursor adds the user under the cursor to the picked list,
// then clears the input so the user can search for another. Returns
// true if a pick was made (false when the result list is empty).
func (s *reviewerSearchState) pickFromCursor() bool {
	if s.cursor < 0 || s.cursor >= len(s.results) {
		return false
	}
	u := s.results[s.cursor]
	s.pickByUsername(u)
	s.input.SetValue("")
	s.cursor = 0
	// In remove mode the candidate list is the static reviewer list,
	// so we just re-filter against the (now empty) input. Add mode's
	// candidate list will be replaced by the next debounced tick or
	// by the empty-query results that were preloaded; do nothing
	// here so the user can keep scrolling the previous results.
	if s.remove {
		s.applyReviewerFilter()
	}
	return true
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

	case "enter", "tab":
		// "Pick the highlighted user and stay open" — clears the
		// input so the user can immediately search for another.
		// Submitting (the API call) is on ctrl+s / ctrl+enter.
		if !s.pickFromCursor() {
			m.status = "no candidates to pick"
		}
		// In add mode: refresh candidates for the now-empty query so
		// the list keeps looking lively (the empty-query response
		// was loaded at startup and may be stale after picks).
		if !s.remove {
			s.loading = true
			return m, tea.Batch(m.spinner.Tick, m.scheduleReviewerSearch())
		}
		return m, nil

	case "ctrl+s", "ctrl+enter":
		// Submit: call AddReviewers / RemoveReviewers with the
		// picked set. ctrl+enter is what most terminals send for
		// shift+enter / cmd+enter so we accept both — and ctrl+s
		// is a universal fallback (the inline editor uses it too).
		if len(s.picked) == 0 {
			// Fall back to "submit the cursor row" for the
			// single-pick case so users who never tapped enter
			// can still get out in one keystroke.
			if !s.pickFromCursor() {
				m.status = "no reviewers picked"
				return m, nil
			}
		}
		return m.submitReviewerPicks()

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

// submitReviewerPicks fires AddReviewers / RemoveReviewers with the
// current picked set, persists each added reviewer to the per-host
// recents list, and closes the overlay.
func (m model) submitReviewerPicks() (tea.Model, tea.Cmd) {
	s := m.reviewerSearch
	if s == nil || len(s.picked) == 0 {
		return m, nil
	}
	usernames := make([]string, 0, len(s.picked))
	labels := make([]string, 0, len(s.picked))
	for _, u := range s.picked {
		usernames = append(usernames, u.Username)
		labels = append(labels, formatUserLabel(u))
	}
	prID := s.prID
	remove := s.remove
	host := m.svc.Host()
	picked := s.picked
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
			if err := m.svc.AddReviewers(m.project, m.slug, prID, usernames); err != nil {
				return err
			}
			// Persist each successfully-added reviewer to the
			// per-host recents list so the next overlay can offer
			// them as quick picks. Best-effort — the reviewers are
			// already on the PR, so a save failure shouldn't fail
			// the action.
			for _, u := range picked {
				_ = config.AddRecentReviewer(host, config.RecentReviewer{
					Username:    u.Username,
					DisplayName: u.DisplayName,
					Email:       u.Email,
				})
			}
			return nil
		}))
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
// the input on top and the various reviewer panes below.
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
		theme.TitleChipDim.Render("enter pick · ctrl+s submit · esc cancel · ctrl+u clear")

	sectionHeader := func(label string) string {
		return theme.TitleChipDim.Render("── " + label + " ──")
	}

	parts := []string{header, ""}

	// "On this PR" pane: lists the existing reviewers so the user
	// can see who's already there before picking. In remove mode
	// this is also the source list, but the search results pane
	// repeats them filtered — keep the section to anchor the modal.
	if len(s.existingDisplay) > 0 {
		parts = append(parts, sectionHeader("On this PR"))
		for _, r := range s.existingDisplay {
			line := "  " + r.DisplayName
			if r.DisplayName == "" {
				line = "  " + r.Username
			} else if r.Username != r.DisplayName {
				line += "  (" + r.Username + ")"
			}
			if r.Status != "" {
				line += "  " + theme.TitleChipDim.Render("["+strings.ToLower(r.Status)+"]")
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(line))
		}
		parts = append(parts, "")
	}

	// Search input + status line.
	parts = append(parts, s.input.View())
	switch {
	case s.loading:
		parts = append(parts, theme.TitleChipDim.Render(m.spinner.View()+" searching…"))
	case s.err != nil:
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
			Render("error: "+s.err.Error()))
	}
	parts = append(parts, "")

	// Results pane: live SearchUsers results in add mode (with the
	// per-host "common reviewers" pinned at the top — marked with
	// ★), client-side-filtered reviewer list in remove mode.
	resultsLabel := "Search results"
	if s.remove {
		resultsLabel = "Reviewers on PR"
	} else if strings.TrimSpace(s.input.Value()) == "" && len(s.recents) > 0 {
		resultsLabel = "Common reviewers + directory"
	}
	parts = append(parts, sectionHeader(resultsLabel))
	if len(s.results) == 0 {
		parts = append(parts, theme.TitleChipDim.Render("  (no matches)"))
	} else {
		maxRows := 10
		if maxRows > len(s.results) {
			maxRows = len(s.results)
		}
		// Window the results around the cursor so it stays visible
		// when the list is longer than maxRows.
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
			if pickedHas(s.picked, u.Username) {
				marker = "✓ "
			} else if _, recent := s.recentSet[strings.ToLower(u.Username)]; recent {
				marker = "★ "
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
			parts = append(parts, line)
		}
		if len(s.results) > maxRows {
			parts = append(parts, theme.TitleChipDim.Render(
				fmt.Sprintf("  (%d more — keep typing to narrow)", len(s.results)-maxRows)))
		}
	}

	// Picked pane: what gets sent on submit.
	if len(s.picked) > 0 {
		parts = append(parts, "")
		pickLabel := "Picked to add"
		if s.remove {
			pickLabel = "Picked to remove"
		}
		parts = append(parts, sectionHeader(pickLabel))
		for _, u := range s.picked {
			parts = append(parts, "  ✓ "+formatUserLabel(u))
		}
	}

	body := strings.Join(parts, "\n")

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

// pickedHas reports whether username is in the picked slice.
func pickedHas(picked []api.User, username string) bool {
	for _, p := range picked {
		if p.Username == username {
			return true
		}
	}
	return false
}
