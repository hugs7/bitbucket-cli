package pr

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func TestTargetInputCompletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tea.KeyMsg
		want string
	}{
		{"empty tab", []tea.KeyMsg{{Type: tea.KeyTab}}, "release/new"},
		{"arrow order", []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyTab}}, "release/old"},
		{"wrap backwards", []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyTab}}, "main"},
		{"prefix", []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("RELEASE/")}, {Type: tea.KeyDown}, {Type: tea.KeyTab}}, "release/old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, value := "feature/work", ""
			f := NewTargetInput(&source, &value, func(string) []string { return []string{"release/new", "release/old", "main"} })
			km := huh.NewDefaultKeyMap()
			km.Input.AcceptSuggestion = key.NewBinding(key.WithKeys("tab"))
			km.Input.Next = key.NewBinding(key.WithKeys("enter"))
			f.WithKeyMap(km)
			f.Focus()
			if !strings.Contains(f.View(), "release/new") {
				t.Fatal("missing suggested target")
			}
			for _, msg := range tc.keys {
				f.Update(msg)
			}
			if value != tc.want {
				t.Fatalf("got %q, want %q", value, tc.want)
			}
		})
	}
}
