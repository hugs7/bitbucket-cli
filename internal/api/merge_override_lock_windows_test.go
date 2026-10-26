package api

import (
	"os"
	"strings"
	"testing"
)

func TestMergeOverrideWindowsRefusesUnsafeJournalPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	svc := &serverService{} // Must refuse before even accessing the HTTP client.
	for _, operation := range []func() error{
		func() error { return svc.MergePRBypassingChecks("PROJ", "repo", 1, "") },
		func() error { return svc.RecoverMergeChecks("PROJ", "repo") },
	} {
		err := operation()
		if err == nil || !strings.Contains(err.Error(), "ACL-backed") || !strings.Contains(err.Error(), "durable directory persistence") {
			t.Fatalf("missing explicit secure-persistence refusal: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported operation created recovery files: %v %v", entries, err)
	}
}
