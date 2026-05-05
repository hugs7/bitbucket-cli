// Package tui — PR TUI keymap.
//
// Pulled out of prs.go to keep the main model file focused on update
// flow and message handling. Every binding is declared once here;
// per-mode help structures pick the subset relevant to whichever
// view the user is currently looking at so the footer stays honest.
package pr

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up, Down         key.Binding
	Enter            key.Binding
	Diff             key.Binding
	Open             key.Binding
	CopyLink         key.Binding
	Refresh          key.Binding
	State, StatePrev key.Binding
	Help             key.Binding
	Back, Quit       key.Binding
	ClearStatus      key.Binding
	Settings         key.Binding

	Approve, Unapprove, NeedsWork, Merge key.Binding
	EditDesc, Comments, AddComment       key.Binding
	EditTarget                           key.Binding
	CreatePR, DeclinePR, DeletePR        key.Binding
	ManageReviewers                      key.Binding

	// settings-mode actions
	SettingsToggle key.Binding

	// comments-mode actions
	EditComment, DeleteComment, ReplyComment, ConfirmYes, ConfirmNo key.Binding

	// diff-mode actions
	InlineComment, ToggleSide, ToggleSplit, ToggleInline key.Binding
	TreeFocus, TreeSelect, NextFile, PrevFile            key.Binding

	// file-level comment in diff
	DiffFileComment key.Binding

	// diff-mode comment actions on the comment under the cursor
	DiffAddComment, DiffEditComment, DiffDeleteComment, DiffReactComment key.Binding

	// palette
	PaletteOpen key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view detail")),
		Diff:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
		Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browser")),
		CopyLink:  key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy link")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		State:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s/S", "state ←/→")),
		StatePrev: key.NewBinding(key.WithKeys("S")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:      key.NewBinding(key.WithKeys("esc", "h"), key.WithHelp("esc/h", "back")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		ClearStatus: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear status")),
		Settings:    key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings")),

		SettingsToggle: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter/space", "toggle / cycle")),

		Approve:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Unapprove:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "unapprove")),
		NeedsWork:  key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "needs work")),
		Merge:      key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "merge")),
		EditDesc:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit description")),
		EditTarget: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "edit target branch")),
		Comments:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comments")),
		AddComment: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new comment")),
		CreatePR:   key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "create PR")),
		DeclinePR:  key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "decline PR")),
		DeletePR:   key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete PR")),

		ManageReviewers: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "manage reviewers")),

		EditComment:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		DeleteComment: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		ReplyComment:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
		ConfirmYes:    key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes")),
		ConfirmNo:     key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n", "no")),

		InlineComment: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment line")),
		ToggleSide:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle side")),
		ToggleSplit:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "split/unified")),
		ToggleInline:  key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "show/hide comments")),

		TreeFocus:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus tree/diff")),
		TreeSelect: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open file")),
		NextFile:   key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next file")),
		PrevFile:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev file")),

		DiffFileComment: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "file comment")),

		DiffAddComment:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "PR comment")),
		DiffEditComment:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit comment")),
		DiffDeleteComment: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete comment")),
		DiffReactComment:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "react 👍")),

		PaletteOpen: key.NewBinding(key.WithKeys("ctrl+k", ":"), key.WithHelp("ctrl+k", "command palette")),
	}
}

// modeKeyMap is a help.KeyMap that exposes only the keys relevant to a
// given view mode, so the footer never lies about what's available.
type modeKeyMap struct {
	short [][]key.Binding
	full  [][]key.Binding
}

func (m modeKeyMap) ShortHelp() []key.Binding {
	if len(m.short) == 0 {
		return nil
	}
	return m.short[0]
}
func (m modeKeyMap) FullHelp() [][]key.Binding { return m.full }

func (k keyMap) listHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.Diff, k.Comments, k.Approve, k.Unapprove, k.NeedsWork, k.Merge, k.ManageReviewers, k.CreatePR, k.DeclinePR, k.DeletePR, k.PaletteOpen, k.Settings, k.Help, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Enter, k.Diff, k.Open, k.CopyLink},
			{k.Approve, k.Unapprove, k.NeedsWork, k.Merge},
			{k.EditDesc, k.EditTarget, k.Comments, k.Refresh, k.State},
			{k.ManageReviewers, k.CreatePR, k.DeclinePR, k.DeletePR},
			{k.PaletteOpen, k.Settings, k.ClearStatus, k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) viewerHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.InlineComment, k.ReplyComment, k.DiffEditComment, k.DiffDeleteComment, k.DiffReactComment, k.DiffFileComment, k.TreeFocus, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.InlineComment, k.DiffAddComment, k.DiffFileComment},
			{k.ReplyComment, k.DiffEditComment, k.DiffDeleteComment, k.DiffReactComment},
			{k.ToggleSide, k.TreeFocus, k.TreeSelect, k.PrevFile, k.NextFile},
			{k.ToggleSplit, k.ToggleInline, k.PaletteOpen, k.Help, k.Back, k.Quit},
		},
	}
}

// detailHelp surfaces the same action keys as the list, plus scroll/back.
// We want users to act on a PR straight from the detail viewport without
// hopping back to the list first.
func (k keyMap) detailHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Diff, k.Comments, k.Approve, k.Unapprove, k.NeedsWork, k.Merge, k.ManageReviewers, k.DeclinePR, k.DeletePR, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Diff, k.Comments, k.Open, k.CopyLink},
			{k.Approve, k.Unapprove, k.NeedsWork, k.Merge},
			{k.EditDesc, k.EditTarget, k.ManageReviewers, k.DeclinePR, k.DeletePR, k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) commentsHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.AddComment, k.ReplyComment, k.EditComment, k.DeleteComment, k.Back, k.Quit}},
		full: [][]key.Binding{
			{k.Up, k.Down, k.Enter, k.AddComment, k.ReplyComment},
			{k.EditComment, k.DeleteComment, k.Refresh},
			{k.Help, k.Back, k.Quit},
		},
	}
}
func (k keyMap) confirmHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.ConfirmYes, k.ConfirmNo, k.Back}},
		full:  [][]key.Binding{{k.ConfirmYes, k.ConfirmNo, k.Back}},
	}
}
func (k keyMap) paletteHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.Enter, k.Back}},
		full:  [][]key.Binding{{k.Up, k.Down, k.Enter, k.Back}},
	}
}

// settingsHelp surfaces only the keys that make sense in the settings
// overlay: navigate, toggle, clear status, back/quit.
func (k keyMap) settingsHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.SettingsToggle, k.ClearStatus, k.Back, k.Quit}},
		full:  [][]key.Binding{{k.Up, k.Down, k.SettingsToggle, k.ClearStatus, k.Back, k.Quit}},
	}
}

// messagesHelp documents the :messages history view: scroll, clear
// the log, navigate back to wherever we came from.
func (k keyMap) messagesHelp() modeKeyMap {
	return modeKeyMap{
		short: [][]key.Binding{{k.Up, k.Down, k.ClearStatus, k.Back, k.Quit}},
		full:  [][]key.Binding{{k.Up, k.Down, k.ClearStatus, k.Back, k.Quit}},
	}
}
