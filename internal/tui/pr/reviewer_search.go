// Package pr — Manage-reviewers overlay (single 'v' shortcut).
//
// 'v' opens a centred-modal overlay that lets the user both add and
// remove reviewers from one place — pressing Enter on a row that's
// already on the PR queues it for removal; pressing Enter on a row
// that isn't on the PR queues it for addition. Submit (Ctrl+S /
// Ctrl+Enter) fires the appropriate AddReviewers / RemoveReviewers
// calls in one round-trip.
//
// The textinput at the top of the overlay drives a 200ms debounced
// SearchUsers call (Server: /rest/api/1.0/users) for live directory
// matches, mirroring the Bitbucket web UI's "type-three-letters" feel.
//
// The underlying view (PR list / detail) renders as the overlay's
// backdrop via placeOverlay so the modal feels stacked rather than
// taking over the whole frame.
//
// Key bindings:
//   - enter / tab        → toggle the highlighted row's pick state
//                          (add-or-remove depending on whether the
//                          user is already on the PR), clear input,
//                          stay in the modal so the user can pick
//                          another.
//   - ctrl+s / ctrl+enter → submit (call AddReviewers and/or
//                           RemoveReviewers as appropriate).
//                           Note: ctrl+enter only reaches the
//                           terminal in iTerm2/Alacritty/Kitty/tmux;
//                           macOS Terminal.app collapses it onto
//                           plain Enter so ctrl+s is the universal
//                           fallback.
//   - esc / ctrl+c        → cancel.
//   - ctrl+u              → clear input.
//
// On Bitbucket Cloud SearchUsers is a stub returning (nil, nil); the
// flow falls back to the legacy free-text editor in the empty-results
// branch of startManageReviewers.
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

// reviewerSearchState holds the manage-reviewers overlay's state:
// the input, the current candidate list (existing reviewers + recents
// + directory matches), and the user's pending add/remove picks.
type reviewerSearchState struct {
	prID int

	input   textinput.Model
	loading bool
	err     error

	// results is the unified candidate list shown below the input.
	// Always includes existing reviewers (so they can be picked for
	// removal), the per-host recents (★ marker), and the latest
	// directory search results — deduped by username.
	results []api.User
	cursor  int

	// pickedAdd / pickedRemove are the operations the user has
	// queued. Submit fires both lists in one go. Slices (not maps)
	// so the toast can render them in pick order.
	pickedAdd    []api.User
	pickedRemove []api.User

	// searchVersion increments on every keystroke so stale debounced
	// ticks return early and don't overwrite fresher results.
	searchVersion int

	// existing is the lower-cased usernames currently on the PR.
	// Used to decide whether a pick is an "add" or a "remove" and
	// to render the [on PR] tag on the row.
	existing map[string]struct{}

	// existingDisplay is the original PR reviewer list, kept so the
	// results pane can show the existing reviewers with their
	// display name + status badge.
	existingDisplay []api.Reviewer

	// recents is the per-host common-reviewers list from config,
	// merged into the top of the results list (after existing
	// reviewers) when the input is empty so the user can pick a
	// frequent collaborator with a single Enter. recentSet is the
	// lower-cased username set used to render a ★ marker.
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

// startManageReviewers opens the manage-reviewers overlay for the
// given PR. The candidate list is seeded with existing reviewers
// (always shown) plus the SearchUsers empty-query response, with
// per-host recents pinned at the top. Falls back to the legacy
// free-text editor when SearchUsers is unimplemented (Cloud).
//
// returnTo is the view mode the user came from — the modal's
// backdrop renders that view so the popover feels stacked rather
// than replacing the screen.
func (m *model) startManageReviewers(prID int, reviewers []api.Reviewer, returnTo viewMode) tea.Cmd {
	users, err := m.svc.SearchUsers("", 50)
	if err != nil {
		m.status = "user search failed (" + err.Error() + ") — falling back to free text"
		return editInTUI("add-reviewer",
			fmt.Sprintf("pr-%d-add-reviewer", prID), prID, 0,
			"# Enter one or more usernames (Server) or UUIDs/emails\n"+
				"# (Cloud), separated by space or comma.\n")
	}
	if len(users) == 0 && len(reviewers) == 0 {
		// Cloud's SearchUsers stub returns (nil, nil) and there's
		// nothing to remove — fall back to the legacy editor.
		return editInTUI("add-reviewer",
			fmt.Sprintf("pr-%d-add-reviewer", prID), prID, 0,
			"# Enter one or more usernames (Server) or UUIDs/emails\n"+
				"# (Cloud), separated by space or comma.\n")
	}

	existing := map[string]struct{}{}
	for _, r := range reviewers {
		existing[strings.ToLower(r.Username)] = struct{}{}
	}

	recents := filterRecentsAgainstExisting(config.RecentReviewers(m.svc.Host()), existing)
	recentSet := map[string]struct{}{}
	for _, r := range recents {
		recentSet[strings.ToLower(r.Username)] = struct{}{}
	}

	ti := textinput.New()
	ti.Placeholder = "type a name, username or email…"
	ti.Prompt = theme.SearchPrompt()
	ti.CharLimit = 80
	ti.Focus()

	m.reviewerSearch = &reviewerSearchState{
		prID:            prID,
		input:           ti,
		cursor:          0,
		existing:        existing,
		existingDisplay: reviewers,
		recents:         recents,
		recentSet:       recentSet,
	}
	m.reviewerSearch.results = buildManageResults(reviewers, recents, users)
	m.reviewerSearchReturnTo = returnTo
	m.mode = viewReviewerSearch
	return textinput.Blink
}

// buildManageResults assembles the unified candidate list shown in
// the modal: existing reviewers first (so the user always sees
// who's on the PR), then recents (deduped against existing), then
// directory-search results (deduped against existing + recents).
func buildManageResults(existing []api.Reviewer, recents []config.RecentReviewer, dir []api.User) []api.User {
	out := make([]api.User, 0, len(existing)+len(recents)+len(dir))
	seen := map[string]struct{}{}
	for _, r := range existing {
		key := strings.ToLower(r.Username)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, api.User{Username: r.Username, DisplayName: r.DisplayName})
	}
	for _, r := range recents {
		key := strings.ToLower(r.Username)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, api.User{Username: r.Username, DisplayName: r.DisplayName, Email: r.Email})
	}
	for _, u := range dir {
		key := strings.ToLower(u.Username)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, u)
	}
	return out
}

// filterRecentsAgainstExisting hides recents that are already on
// the PR — the existing reviewers are already pinned at the top of
// the results list, so listing them twice would just be noise.
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

// clampCursor keeps the cursor within the visible result range.
func (s *reviewerSearchState) clampCursor() {
	if s.cursor >= len(s.results) {
		s.cursor = len(s.results) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// togglePickFromCursor flips the pick state of the row under the
// cursor. If the user is already on the PR, the pick goes into
// pickedRemove; otherwise into pickedAdd. Toggling re-clicks the
// same row (de-selects). Returns true if the toggle landed on a
// real row.
func (s *reviewerSearchState) togglePickFromCursor() bool {
	if s.cursor < 0 || s.cursor >= len(s.results) {
		return false
	}
	u := s.results[s.cursor]
	key := strings.ToLower(u.Username)
	if _, onPR := s.existing[key]; onPR {
		// Remove operation — toggle in pickedRemove.
		if removeUserFromSlice(&s.pickedRemove, u.Username) {
			return true
		}
		s.pickedRemove = append(s.pickedRemove, u)
		return true
	}
	// Add operation — toggle in pickedAdd.
	if removeUserFromSlice(&s.pickedAdd, u.Username) {
		return true
	}
	s.pickedAdd = append(s.pickedAdd, u)
	return true
}

// removeUserFromSlice removes the entry whose Username matches the
// given username (case-sensitive — usernames are returned exactly
// from the API). Returns true if a removal happened.
func removeUserFromSlice(s *[]api.User, username string) bool {
	for i, u := range *s {
		if u.Username == username {
			*s = append((*s)[:i], (*s)[i+1:]...)
			return true
		}
	}
	return false
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
		m.mode = m.reviewerSearchReturnTo
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
		// Toggle the highlighted user's pick state. Whether that's
		// an add or a remove is decided by whether the user is
		// already on the PR (see togglePickFromCursor). After a
		// pick we clear the input so the user can search for the
		// next person without reaching for backspace.
		if !s.togglePickFromCursor() {
			m.status = "no candidates to pick"
			return m, nil
		}
		s.input.SetValue("")
		s.cursor = 0
		// Refresh the empty-query result set so the visible list
		// looks lively after a pick.
		s.loading = true
		return m, tea.Batch(m.spinner.Tick, m.scheduleReviewerSearch())

	case "ctrl+s", "ctrl+enter", "alt+enter":
		// Submit: fire AddReviewers / RemoveReviewers as needed.
		// We accept ctrl+s (universal), ctrl+enter (iTerm2 /
		// Alacritty / Kitty / tmux) and alt+enter (so users on
		// macOS who've mapped ⌘ to Meta in their terminal get
		// ⌘+Enter as a submit shortcut). macOS Terminal.app
		// collapses both ctrl+enter and ⌘+enter onto plain Enter,
		// so ctrl+s is the safe fallback there. If nothing has
		// been queued, treat the cursor row as a shortcut
		// single-pick so the user can get out in one keystroke.
		if len(s.pickedAdd) == 0 && len(s.pickedRemove) == 0 {
			if !s.togglePickFromCursor() {
				m.status = "no reviewers picked"
				return m, nil
			}
		}
		return m.submitReviewerPicks()

	case "ctrl+u":
		s.input.SetValue("")
		s.loading = true
		return m, tea.Batch(m.spinner.Tick, m.scheduleReviewerSearch())
	}

	// Forward everything else (printable chars, backspace, etc) to
	// the textinput. Anything that mutates the buffer schedules a
	// debounced search.
	prev := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if s.input.Value() != prev {
		s.loading = true
		return m, tea.Batch(cmd, m.spinner.Tick, m.scheduleReviewerSearch())
	}
	return m, cmd
}

// submitReviewerPicks fires AddReviewers and/or RemoveReviewers with
// the queued picks, persists each added reviewer to the per-host
// recents list, and closes the overlay.
func (m model) submitReviewerPicks() (tea.Model, tea.Cmd) {
	s := m.reviewerSearch
	if s == nil || (len(s.pickedAdd) == 0 && len(s.pickedRemove) == 0) {
		return m, nil
	}
	addUsers := pluckUsernames(s.pickedAdd)
	addLabels := pluckLabels(s.pickedAdd)
	removeUsers := pluckUsernames(s.pickedRemove)
	removeLabels := pluckLabels(s.pickedRemove)
	prID := s.prID
	host := m.svc.Host()
	picked := append([]api.User(nil), s.pickedAdd...)

	m.reviewerSearch = nil
	m.mode = m.reviewerSearchReturnTo
	m.loading = true

	label := buildSubmitLabel(addLabels, removeLabels)
	return m, tea.Batch(m.spinner.Tick, m.doAction(label, true, func() error {
		if len(removeUsers) > 0 {
			if err := m.svc.RemoveReviewers(m.project, m.slug, prID, removeUsers); err != nil {
				return fmt.Errorf("remove: %w", err)
			}
		}
		if len(addUsers) > 0 {
			if err := m.svc.AddReviewers(m.project, m.slug, prID, addUsers); err != nil {
				return fmt.Errorf("add: %w", err)
			}
			// Persist each successfully-added reviewer to the
			// per-host recents list so the next overlay can offer
			// them as quick picks. Best-effort.
			for _, u := range picked {
				_ = config.AddRecentReviewer(host, config.RecentReviewer{
					Username:    u.Username,
					DisplayName: u.DisplayName,
					Email:       u.Email,
				})
			}
		}
		return nil
	}))
}

// pluckUsernames extracts the Username field from each user.
func pluckUsernames(us []api.User) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.Username)
	}
	return out
}

// pluckLabels formats each user as a human-readable label for the toast.
func pluckLabels(us []api.User) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, formatUserLabel(u))
	}
	return out
}

// buildSubmitLabel produces the action toast: "added X, Y · removed Z".
func buildSubmitLabel(adds, removes []string) string {
	switch {
	case len(adds) > 0 && len(removes) > 0:
		return fmt.Sprintf("added %s · removed %s",
			strings.Join(adds, ", "), strings.Join(removes, ", "))
	case len(adds) > 0:
		return fmt.Sprintf("added reviewer(s) %s", strings.Join(adds, ", "))
	case len(removes) > 0:
		return fmt.Sprintf("removed reviewer(s) %s", strings.Join(removes, ", "))
	}
	return "no reviewers changed"
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

// renderReviewerSearch draws the centred overlay card. The caller is
// responsible for compositing this onto the underlying view via
// placeOverlay so the modal feels stacked.
func (m model) renderReviewerSearch() string {
	s := m.reviewerSearch
	if s == nil {
		return ""
	}
	width := editorBoxInnerWidth(m.width) + 2
	innerW := width - 2

	header := theme.TitleBadge.Render(" MANAGE REVIEWERS ") + "  " +
		theme.TitleChipDim.Render("enter pick · ctrl+s/alt+enter submit · esc cancel · ctrl+u clear")

	sectionHeader := func(label string) string {
		return theme.TitleChipDim.Render("── " + label + " ──")
	}

	parts := []string{header, ""}

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

	// Results pane: existing reviewers, recents (★) and directory
	// matches all in one cursor-navigable list. The marker column
	// communicates each row's pick state at a glance.
	parts = append(parts, sectionHeader("Reviewers"))
	if len(s.results) == 0 {
		parts = append(parts, theme.TitleChipDim.Render("  (no matches)"))
	} else {
		maxRows := 12
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
			parts = append(parts, s.renderResultRow(i, innerW))
		}
		if len(s.results) > maxRows {
			parts = append(parts, theme.TitleChipDim.Render(
				fmt.Sprintf("  (%d more — keep typing to narrow)", len(s.results)-maxRows)))
		}
	}

	// Picks pane: a running summary of what submit will do.
	if len(s.pickedAdd) > 0 || len(s.pickedRemove) > 0 {
		parts = append(parts, "", sectionHeader("Will submit"))
		for _, u := range s.pickedAdd {
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("10")).
				Render("  + "+formatUserLabel(u)))
		}
		for _, u := range s.pickedRemove {
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
				Render("  − "+formatUserLabel(u)))
		}
	}

	body := strings.Join(parts, "\n")

	box := lipgloss.NewStyle().
		Border(theme.Border()).
		BorderForeground(lipgloss.Color("57")).
		Padding(1, 2).
		Width(width)

	return box.Render(body)
}

// renderResultRow formats one row in the results list. The marker
// column is "+", "−", "✓" (existing), "★" (recent), or blank, with
// the cursor row highlighted.
func (s *reviewerSearchState) renderResultRow(i, innerW int) string {
	u := s.results[i]
	key := strings.ToLower(u.Username)

	_, onPR := s.existing[key]
	_, recent := s.recentSet[key]
	pickedToAdd := userInSlice(s.pickedAdd, u.Username)
	pickedToRemove := userInSlice(s.pickedRemove, u.Username)

	marker := "  "
	switch {
	case pickedToAdd:
		marker = "+ "
	case pickedToRemove:
		marker = "− "
	case onPR:
		marker = "✓ "
	case recent:
		marker = "★ "
	}

	tag := ""
	switch {
	case pickedToAdd:
		tag = "  " + theme.TitleChipDim.Render("[queued: add]")
	case pickedToRemove:
		tag = "  " + theme.TitleChipDim.Render("[queued: remove]")
	case onPR:
		tag = "  " + theme.TitleChipDim.Render("[on PR]")
	case recent:
		tag = "  " + theme.TitleChipDim.Render("[recent]")
	}

	line := marker + formatUserLabel(u) + tag
	if i == s.cursor {
		// Keep the highlight a fixed width so the row reads as a
		// solid bar rather than a ragged stripe.
		line = lipgloss.NewStyle().
			Background(lipgloss.Color("57")).
			Foreground(lipgloss.Color("231")).
			Bold(true).
			Width(innerW - 4).
			Render(line)
	}
	return line
}

// userInSlice reports whether a username is in a slice of users.
func userInSlice(xs []api.User, username string) bool {
	for _, x := range xs {
		if x.Username == username {
			return true
		}
	}
	return false
}
