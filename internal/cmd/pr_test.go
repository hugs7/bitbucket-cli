package cmd

import "testing"

func TestPRCreateHasNonInteractiveFlag(t *testing.T) {
	flag := newPRCreateCmd().Flags().ShorthandLookup("y")
	if flag == nil || flag.Name != "yes" {
		t.Fatal("pr create is missing the -y/--yes flag")
	}
}

func TestPRMergeRetargetsDependentsByDefault(t *testing.T) {
	flag := newPRMergeCmd().Flags().Lookup("retarget-dependents")
	if flag == nil {
		t.Fatal("retarget-dependents flag is missing")
	}
	if flag.DefValue != "true" {
		t.Fatalf("retarget-dependents default = %q, want true", flag.DefValue)
	}
}
