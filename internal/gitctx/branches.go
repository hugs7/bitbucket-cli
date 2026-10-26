package gitctx

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TargetBranches ranks local and selected-remote branches by their closest
// shared ancestor with source, then branch-tip recency, then name. It never
// fetches. One bounded graph walk serves every candidate; missing/shallow
// history degrades to alphabetical suggestions. inferred is empty when no
// shared ancestor is known, so automatic creation can fail rather than guess.
func TargetBranches(dir, remote, source string) (branches []string, inferred string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	git := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "git", append([]string{"-C", gitDir(dir)}, args...)...).Output()
	}
	if remote == "" {
		if r, err := AtPath(dir, ""); err == nil {
			remote = r.RemoteName
		}
	}
	patterns := []string{"refs/heads/"}
	if remote != "" {
		patterns = append(patterns, "refs/remotes/"+remote+"/")
	}
	out, err := git(append([]string{"for-each-ref", "--format=%(refname) %(objectname) %(committerdate:unix) %(symref)"}, patterns...)...)
	if err != nil {
		return nil, ""
	}
	type tip struct {
		hash string
		date int64
	}
	tips := map[string]tip{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 { // excludes symbolic remote HEAD
			continue
		}
		name := strings.TrimPrefix(fields[0], "refs/heads/")
		name = strings.TrimPrefix(name, "refs/remotes/"+remote+"/")
		date, _ := strconv.ParseInt(fields[2], 10, 64)
		tips[name] = tip{fields[1], date} // remote tip wins over stale local head
	}
	for name := range tips {
		if name != source {
			branches = append(branches, name)
		}
	}
	sort.Strings(branches)
	if source == "" || len(branches) == 0 {
		return branches, ""
	}
	// Resolve the source explicitly, never interpreting a user value as an option.
	var sourceHash string
	if out, err := git("rev-parse", "--verify", "refs/heads/"+source+"^{commit}"); err == nil {
		sourceHash = strings.TrimSpace(string(out))
	} else if tip, ok := tips[source]; ok {
		sourceHash = tip.hash
	} else {
		return branches, ""
	}
	args := []string{"rev-list", "--topo-order", "--parents", "--max-count=50000", sourceHash}
	seen := map[string]bool{sourceHash: true}
	for _, name := range branches {
		if hash := tips[name].hash; !seen[hash] {
			args = append(args, hash)
			seen[hash] = true
		}
	}
	args = append(args, "--")
	out, err = git(args...)
	if err != nil {
		return branches, ""
	}
	parents := map[string][]string{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			order = append(order, fields[0])
			parents[fields[0]] = fields[1:]
		}
	}
	// Distances are topology-based rather than dates, which can be skewed.
	distance := map[string]int{sourceHash: 0}
	queue := []string{sourceHash}
	for i := 0; i < len(queue); i++ {
		for _, parent := range parents[queue[i]] {
			if _, ok := distance[parent]; !ok {
				distance[parent] = distance[queue[i]] + 1
				queue = append(queue, parent)
			}
		}
	}
	// Parent-before-child dynamic programming shares work across all refs.
	best := map[string]int{}
	for i := len(order) - 1; i >= 0; i-- {
		hash := order[i]
		if d, ok := distance[hash]; ok {
			best[hash] = d
		}
		for _, parent := range parents[hash] {
			if d, ok := best[parent]; ok {
				if old, exists := best[hash]; !exists || d < old {
					best[hash] = d
				}
			}
		}
	}
	sort.SliceStable(branches, func(i, j int) bool {
		a, b := tips[branches[i]], tips[branches[j]]
		da, oka := best[a.hash]
		db, okb := best[b.hash]
		if oka != okb {
			return oka
		}
		if !oka {
			return branches[i] < branches[j]
		}
		if da != db {
			return da < db
		}
		if a.date != b.date {
			return a.date > b.date
		}
		return branches[i] < branches[j]
	})
	if _, ok := best[tips[branches[0]].hash]; ok {
		inferred = branches[0]
	}
	return branches, inferred
}

// Matches reports whether this working tree represents the API repository.
func Matches(host, project, slug string) bool {
	r, err := Current("")
	return err == nil && strings.EqualFold(r.Host, host) && strings.EqualFold(r.Project, project) && r.Slug == slug
}
