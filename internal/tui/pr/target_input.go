package pr

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// targetInput extends huh's input with completion on an empty value. Bubbles
// deliberately suppresses empty-prefix suggestions, so handle that state here.
type targetInput struct {
	*huh.Input
	source, value *string
	choices       func(string) []string
	branches      []string
	index         int
}

// NewTargetInput creates a branch input whose first suggestion can be accepted
// with Tab immediately. Ranking is refreshed on focus, not on each keystroke.
func NewTargetInput(source, value *string, choices func(string) []string) huh.Field {
	return &targetInput{
		Input: huh.NewInput().Title("Target branch").Value(value).
			Description("↑/↓ choose · tab complete · enter next").Validate(formNonEmpty),
		source: source, value: value, choices: choices,
	}
}

// Focus refreshes candidates after the source field has been edited.
func (f *targetInput) Focus() tea.Cmd {
	f.branches = f.choices(*f.source)
	f.index = 0
	f.Input.Suggestions(f.branches)
	if len(f.branches) > 0 {
		f.Input.Placeholder(f.branches[0])
	}
	return f.Input.Focus()
}

// Update cycles blank-prefix suggestions without filling the field until Tab.
func (f *targetInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && *f.value == "" && len(f.branches) > 0 {
		switch key.String() {
		case "down", "ctrl+n":
			f.index = (f.index + 1) % len(f.branches)
			f.Input.Placeholder(f.branches[f.index])
			return f, nil
		case "up", "ctrl+p":
			f.index = (f.index + len(f.branches) - 1) % len(f.branches)
			f.Input.Placeholder(f.branches[f.index])
			return f, nil
		case "tab":
			*f.value = f.branches[f.index]
			f.Input.Value(f.value)
			return f, nil
		}
	}
	_, cmd := f.Input.Update(msg)
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "tab" {
		for _, name := range f.branches {
			if strings.EqualFold(name, *f.value) {
				*f.value = name
				f.Input.Value(f.value)
				break
			}
		}
	}
	return f, cmd
}
