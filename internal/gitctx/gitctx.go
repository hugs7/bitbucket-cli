// Package gitctx detects the current Bitbucket repository from a git
// working tree by parsing `origin` (or another configured remote).
package gitctx

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Repo struct {
	Host    string // e.g. bitbucket.org or bitbucket.mycorp.example
	Project string // server: project key; cloud: workspace
	Slug    string // repo slug
	Remote  string // raw remote URL
}

// Current returns the Repo represented by the given git remote (defaults
// to "origin") in the current working directory.
func Current(remote string) (*Repo, error) {
	if remote == "" {
		remote = "origin"
	}
	out, err := exec.Command("git", "config", "--get", "remote."+remote+".url").Output()
	if err != nil {
		return nil, fmt.Errorf("not in a git repo or no remote %q: %w", remote, err)
	}
	raw := strings.TrimSpace(string(out))
	return Parse(raw)
}

// Parse turns a Bitbucket remote URL (https or ssh) into a Repo.
//
// Supported forms:
//
//	https://user@bitbucket.org/workspace/repo.git
//	https://bitbucket.mycorp.example/scm/PROJ/repo.git
//	ssh://git@bitbucket.mycorp.example:7999/PROJ/repo.git
//	git@bitbucket.org:workspace/repo.git
func Parse(raw string) (*Repo, error) {
	r := &Repo{Remote: raw}

	// scp-like form: git@host:path
	if !strings.Contains(raw, "://") && strings.Contains(raw, ":") {
		at := strings.Index(raw, "@")
		colon := strings.Index(raw, ":")
		if at >= 0 && colon > at {
			r.Host = raw[at+1 : colon]
			path := raw[colon+1:]
			return fillPath(r, path)
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse remote %q: %w", raw, err)
	}
	r.Host = u.Hostname()
	return fillPath(r, u.Path)
}

func fillPath(r *Repo, path string) (*Repo, error) {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	// Bitbucket Server clone URLs include a /scm/ prefix.
	path = strings.TrimPrefix(path, "scm/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected remote path %q", path)
	}
	r.Project = parts[0]
	r.Slug = parts[1]
	return r, nil
}
