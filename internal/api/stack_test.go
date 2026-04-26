package api

import "testing"

func mkPR(id int, src, tgt string) PullRequest {
	return PullRequest{ID: id, SourceRef: src, TargetRef: tgt}
}

func TestComputeStacks_LinearChain(t *testing.T) {
	prs := []PullRequest{
		mkPR(1, "feat-1", "main"),
		mkPR(2, "feat-2", "feat-1"),
		mkPR(3, "feat-3", "feat-2"),
	}
	stacks := ComputeStacks(prs)
	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	s := stacks[0]
	if !s.IsStacked() {
		t.Errorf("expected IsStacked() true")
	}
	if s.Base().ID != 1 {
		t.Errorf("expected base ID 1, got %d", s.Base().ID)
	}
	if s.Tip().ID != 3 {
		t.Errorf("expected tip ID 3, got %d", s.Tip().ID)
	}
	pos, total := s.PositionOf(2)
	if pos != 2 || total != 3 {
		t.Errorf("PositionOf(2) = (%d, %d), want (2, 3)", pos, total)
	}
}

func TestComputeStacks_MultipleStacksAndStandalone(t *testing.T) {
	prs := []PullRequest{
		mkPR(10, "alpha-1", "main"),
		mkPR(11, "alpha-2", "alpha-1"),
		mkPR(20, "beta-1", "main"),
		mkPR(30, "standalone", "main"),
	}
	stacks := ComputeStacks(prs)
	if len(stacks) != 3 {
		t.Fatalf("expected 3 stacks, got %d", len(stacks))
	}
	stackedCount := 0
	for _, s := range stacks {
		if s.IsStacked() {
			stackedCount++
		}
	}
	if stackedCount != 1 {
		t.Errorf("expected 1 multi-PR stack, got %d", stackedCount)
	}
}

func TestComputeStacks_EmptyInput(t *testing.T) {
	if got := ComputeStacks(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
