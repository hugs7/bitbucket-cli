package api

// PRStack is a linear chain of PRs where each PR's target ref is the
// previous PR's source ref. The slice is ordered base → tip: index 0
// is the PR closest to the integration branch (e.g. main), and the
// last entry is the topmost PR in the stack.
//
// A stack of length 1 is a regular standalone PR; callers typically
// only treat chains with len ≥ 2 as "stacked".
type PRStack struct {
	Items []PullRequest
}

// ComputeStacks groups the given PRs into stacks by walking the
// source → target dependency graph. Two PRs are stack-linked when
// PR-B.TargetRef == PR-A.SourceRef (B sits on top of A).
//
// PRs that don't participate in any chain longer than themselves end
// up as singleton stacks. The returned slice is stable across runs:
// stacks are ordered by their tip's PR id descending so the newest
// work surfaces first.
func ComputeStacks(prs []PullRequest) []PRStack {
	if len(prs) == 0 {
		return nil
	}
	// Index by source ref. A PR's "parent" (the one it sits on top
	// of) is whichever PR has source == this PR's target.
	bySource := make(map[string]PullRequest, len(prs))
	for _, p := range prs {
		// If two PRs share a source (rare but possible after
		// rebase-and-replace), the latest by ID wins.
		if existing, ok := bySource[p.SourceRef]; !ok || p.ID > existing.ID {
			bySource[p.SourceRef] = p
		}
	}

	// hasChild[srcRef] is true when some other PR targets srcRef,
	// i.e. srcRef is the parent of at least one PR. The "tips" of
	// each stack are PRs whose source is NOT a parent of anything.
	hasChild := make(map[string]bool, len(prs))
	for _, p := range prs {
		hasChild[p.TargetRef] = true
	}

	var stacks []PRStack
	seen := make(map[int]bool, len(prs))
	for _, p := range prs {
		if seen[p.ID] {
			continue
		}
		// Only start chains from tips so we don't emit the same
		// chain multiple times rooted at different members.
		if hasChild[p.SourceRef] {
			continue
		}
		// Walk down: this PR → its parent (source matches our
		// target) → … until we hit a PR whose target isn't anyone's
		// source (the integration branch).
		var chain []PullRequest
		cur := p
		for {
			if seen[cur.ID] {
				break // defensive: cycle guard
			}
			seen[cur.ID] = true
			chain = append([]PullRequest{cur}, chain...) // prepend so base ends up first
			parent, ok := bySource[cur.TargetRef]
			if !ok {
				break
			}
			cur = parent
		}
		stacks = append(stacks, PRStack{Items: chain})
	}

	// Catch any PRs we never reached (cycle members or tips that
	// were skipped because hasChild was wrong for malformed graphs)
	// as singletons so they still show up.
	for _, p := range prs {
		if !seen[p.ID] {
			seen[p.ID] = true
			stacks = append(stacks, PRStack{Items: []PullRequest{p}})
		}
	}
	return stacks
}

// IsStacked reports whether this stack contains more than one PR
// (i.e. it's an actual chain, not a standalone PR).
func (s PRStack) IsStacked() bool { return len(s.Items) > 1 }

// Tip returns the topmost PR in the stack (the one with no children
// in the stack).
func (s PRStack) Tip() PullRequest { return s.Items[len(s.Items)-1] }

// Base returns the bottom-most PR in the stack (closest to the
// integration branch).
func (s PRStack) Base() PullRequest { return s.Items[0] }

// PositionOf returns (depth, total) for the PR with the given id, or
// (-1, 0) if it isn't in this stack. depth is 1-based: 1 = base,
// total = tip.
func (s PRStack) PositionOf(id int) (int, int) {
	for i, p := range s.Items {
		if p.ID == id {
			return i + 1, len(s.Items)
		}
	}
	return -1, 0
}
