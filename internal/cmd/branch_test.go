package cmd

import "testing"

func TestRepoBranchDeleteCmd(t *testing.T) {
	c := newRepoBranchDeleteCmd()

	if err := c.Args(c, nil); err == nil {
		t.Fatal("expected at least one branch argument")
	}
	if err := c.Args(c, []string{"feature/one", "feature/two"}); err != nil {
		t.Fatalf("expected multiple branch arguments to be accepted: %v", err)
	}
	if c.Flags().Lookup("yes") == nil {
		t.Fatal("expected --yes flag")
	}
	if c.Flags().Lookup("force") == nil {
		t.Fatal("expected --force flag")
	}
	if c.Flags().Lookup("repo") == nil {
		t.Fatal("expected --repo flag")
	}
}
