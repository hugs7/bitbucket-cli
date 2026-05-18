package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	want := "## Summary\n- one\n- two\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("body only", func(t *testing.T) {
		got, err := resolveBody("hello", "")
		if err != nil || got != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("file only", func(t *testing.T) {
		got, err := resolveBody("", path)
		if err != nil || got != want {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("both set", func(t *testing.T) {
		if _, err := resolveBody("hello", path); err == nil {
			t.Fatal("expected error when both --body and --body-file are set")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := resolveBody("", filepath.Join(dir, "nope")); err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("neither", func(t *testing.T) {
		got, err := resolveBody("", "")
		if err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}
