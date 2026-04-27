// Package gitctx detects the current Bitbucket repository from a git
// working tree by parsing `origin` (or another configured remote).
package gitctx

import (
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
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
	return AtPath("", remote)
}

// AtPath is like Current but reads the remote config from the
// repository at dir instead of the process cwd. dir == "" falls
// back to cwd. Used by `bb <path>` so the user can ask for the
// repo overview of any working tree without having to cd into it.
func AtPath(dir, remote string) (*Repo, error) {
	if remote == "" {
		remote = "origin"
	}
	args := []string{}
	if dir != "" {
		args = append(args, "-C", dir)
	}
	args = append(args, "config", "--get", "remote."+remote+".url")
	out, err := exec.Command("git", args...).Output()
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

// PRRef is a parsed "look at PR #N in this repo" coordinate, sourced
// from a Bitbucket Cloud or Server URL.
type PRRef struct {
	Repo
	ID int
}

// ParsePRURL extracts host / project / slug / id from a Bitbucket
// Cloud or Server pull-request URL. Both URL shapes are supported
// (and the trailing /overview, /diff, /commits suffixes Bitbucket
// itself sticks on the end of the path are tolerated):
//
//	https://bitbucket.org/<workspace>/<repo>/pull-requests/<id>[/...]
//	https://<host>/projects/<PROJ>/repos/<repo>/pull-requests/<id>[/...]
//
// Returns an error for anything that doesn't look like one of those
// — we'd rather be loud than silently route the user to the wrong
// repo.
func ParsePRURL(raw string) (*PRRef, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("not a URL: %q", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	idx := -1
	for i, p := range parts {
		if p == "pull-requests" || p == "pullrequests" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(parts) {
		return nil, fmt.Errorf("no /pull-requests/<id> segment in %q", raw)
	}
	id, err := strconv.Atoi(parts[idx+1])
	if err != nil {
		return nil, fmt.Errorf("PR id is not an integer in %q", raw)
	}

	var project, slug string
	switch {
	// Server form: /projects/<PROJ>/repos/<repo>/pull-requests/<id>.
	case idx >= 4 && parts[0] == "projects" && parts[2] == "repos":
		project, slug = parts[1], parts[3]
	// Cloud form: /<workspace>/<repo>/pull-requests/<id>.
	case idx == 2:
		project, slug = parts[0], parts[1]
	default:
		return nil, fmt.Errorf("unrecognised pull-request URL shape: %q", raw)
	}

	return &PRRef{
		Repo: Repo{Host: u.Hostname(), Project: project, Slug: slug, Remote: raw},
		ID:   id,
	}, nil
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
