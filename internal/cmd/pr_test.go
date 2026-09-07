package cmd

import "testing"

func TestPRMergeRetargetsDependentsByDefault(t *testing.T) {
	flag := newPRMergeCmd().Flags().Lookup("retarget-dependents")
	if flag == nil {
		t.Fatal("retarget-dependents flag is missing")
	}
	if flag.DefValue != "true" {
		t.Fatalf("retarget-dependents default = %q, want true", flag.DefValue)
	}
}
