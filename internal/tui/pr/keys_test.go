package pr

import "testing"

// TestApplyKeybindingOverrides verifies that user config remaps
// reach the live keyMap, preserve the original help text, and that
// unknown names are surfaced (not silently dropped) so the caller
// can warn the user.
func TestApplyKeybindingOverrides(t *testing.T) {
	t.Run("remaps a single key and preserves help", func(t *testing.T) {
		km := defaultKeys()
		got := applyKeybindingOverrides(&km, map[string]string{"approve": "y"})
		if len(got) != 0 {
			t.Fatalf("unexpected unknown overrides: %v", got)
		}
		keys := km.Approve.Keys()
		if len(keys) != 1 || keys[0] != "y" {
			t.Fatalf("expected approve remapped to [y], got %v", keys)
		}
		if km.Approve.Help().Desc != "approve" {
			t.Fatalf("expected help desc preserved as 'approve', got %q", km.Approve.Help().Desc)
		}
	})

	t.Run("supports comma-separated alternates", func(t *testing.T) {
		km := defaultKeys()
		applyKeybindingOverrides(&km, map[string]string{"merge": "g, M"})
		keys := km.Merge.Keys()
		if len(keys) != 2 || keys[0] != "g" || keys[1] != "M" {
			t.Fatalf("expected [g M], got %v", keys)
		}
	})

	t.Run("reports unknown names sorted", func(t *testing.T) {
		km := defaultKeys()
		got := applyKeybindingOverrides(&km, map[string]string{
			"approve":   "y",
			"nope":      "x",
			"also_nope": "z",
		})
		if len(got) != 2 {
			t.Fatalf("expected 2 unknowns, got %v", got)
		}
		// Sorted output keeps the warning toast deterministic.
		if got[0] != "also_nope" || got[1] != "nope" {
			t.Fatalf("expected sorted unknowns [also_nope nope], got %v", got)
		}
	})

	t.Run("ignores empty values", func(t *testing.T) {
		km := defaultKeys()
		applyKeybindingOverrides(&km, map[string]string{"approve": "  ,  "})
		// Original "a" should still be in place.
		if keys := km.Approve.Keys(); len(keys) == 0 || keys[0] != "a" {
			t.Fatalf("empty override should be a no-op, got %v", keys)
		}
	})
}
