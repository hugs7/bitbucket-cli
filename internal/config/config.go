// Package config handles persisted bb configuration.
//
// Config (including tokens) lives at $XDG_CONFIG_HOME/bb/config.yml — by
// default ~/.config/bb/config.yml. The file is written with mode 0600 so
// only the current user can read it. This matches the behaviour of `gh`,
// `glab`, and similar CLIs on Linux when no system keyring is available.
//
// Override the location with $BB_CONFIG. Override a single token at runtime
// with $BB_TOKEN (applies to the default host).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Host struct {
	Type     string `yaml:"type"`               // "cloud" or "server"
	Username string `yaml:"username"`           // user identifier
	APIBase  string `yaml:"api_base,omitempty"` // for server: https://host/rest/api/1.0
	Token    string `yaml:"token,omitempty"`    // app password / HTTP access token
}

type Config struct {
	DefaultHost string          `yaml:"default_host"`
	Editor      string          `yaml:"editor,omitempty"` // command to launch text editor (default: $VISUAL/$EDITOR/nano)
	Hosts       map[string]Host `yaml:"hosts"`

	// Diff TUI preferences (persist across sessions). The "hide
	// inline" form is inverted so the zero value (false) means "show
	// comments" — the friendlier default.
	DiffSplit      bool `yaml:"diff_split,omitempty"`
	DiffHideInline bool `yaml:"diff_hide_inline,omitempty"`

	// Favourites are repos the user has pinned from the home TUI.
	Favourites []FavRepo `yaml:"favourites,omitempty"`

	// Theme is the named TUI colour theme. See internal/tui/theme.go
	// for the list of built-ins (default, dracula, solarized-dark, nord).
	// Empty falls back to "default".
	Theme string `yaml:"theme,omitempty"`

	// AICmd is a shell command piped a unified diff on stdin which
	// should print a PR description on stdout. Used by `bb pr describe`.
	// $BB_AI_CMD overrides this at runtime.
	AICmd string `yaml:"ai_cmd,omitempty"`

	// InlineEditor controls the default editor experience inside the
	// PR TUI. When true, comment / description edits open in an
	// in-process textarea overlay (the "picture-in-picture" editor)
	// that keeps the surrounding context visible. When false, the
	// editor launches the user's $EDITOR full-screen via tea.ExecProcess.
	// Either way, F11 toggles between modes for the current edit.
	InlineEditor bool `yaml:"inline_editor,omitempty"`
}

// FavRepo is a repo entry persisted in the user's favourites list.
type FavRepo struct {
	Host    string `yaml:"host"`
	Project string `yaml:"project"`
	Slug    string `yaml:"slug"`
	Name    string `yaml:"name,omitempty"`
}

// Editor returns the user's preferred text editor command.
// Resolution order: config.editor → $VISUAL → $EDITOR → "nano".
func (c Config) EditorCmd() string {
	if c.Editor != "" {
		return c.Editor
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return "nano"
}

var (
	loaded Config
	path   string
)

// Load reads the config from disk. A missing file is not an error.
func Load(overridePath string) error {
	p := overridePath
	if p == "" {
		if env := os.Getenv("BB_CONFIG"); env != "" {
			p = env
		} else {
			dir, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			p = filepath.Join(dir, "bb", "config.yml")
		}
	}
	path = p

	loaded = Config{Hosts: map[string]Host{}}
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config %s: %w", p, err)
	}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &loaded); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
	}

	if envTok := os.Getenv("BB_TOKEN"); envTok != "" && loaded.DefaultHost != "" {
		h := loaded.Hosts[loaded.DefaultHost]
		h.Token = envTok
		loaded.Hosts[loaded.DefaultHost] = h
	}
	return nil
}

// Get returns the loaded config.
func Get() Config { return loaded }

// SetHost adds or updates a host and persists the config.
func SetHost(name string, h Host) error {
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
	}
	loaded.Hosts[name] = h
	if loaded.DefaultHost == "" {
		loaded.DefaultHost = name
	}
	return save()
}

// RemoveHost removes a host and persists the config.
func RemoveHost(name string) error {
	delete(loaded.Hosts, name)
	if loaded.DefaultHost == name {
		loaded.DefaultHost = ""
		for n := range loaded.Hosts {
			loaded.DefaultHost = n
			break
		}
	}
	return save()
}

// SetDiffPrefs persists the diff TUI toggles so they survive between
// `bb prs` sessions. Best-effort — callers can ignore the error.
func SetDiffPrefs(split, showInline bool) error {
	loaded.DiffSplit = split
	loaded.DiffHideInline = !showInline
	return save()
}

// SetTheme persists the chosen TUI theme name. Best-effort.
func SetTheme(name string) error {
	loaded.Theme = name
	return save()
}

// SetInlineEditor persists the inline (PIP) editor preference. When
// true, comment / description edits open in an in-process textarea
// overlay; when false, they shell out to the user's $EDITOR.
func SetInlineEditor(on bool) error {
	loaded.InlineEditor = on
	return save()
}

// IsFavourite reports whether a repo is pinned.
func IsFavourite(host, project, slug string) bool {
	for _, f := range loaded.Favourites {
		if f.Host == host && f.Project == project && f.Slug == slug {
			return true
		}
	}
	return false
}

// AddFavourite adds (or refreshes) a favourite entry.
func AddFavourite(f FavRepo) error {
	for i, ex := range loaded.Favourites {
		if ex.Host == f.Host && ex.Project == f.Project && ex.Slug == f.Slug {
			loaded.Favourites[i] = f
			return save()
		}
	}
	loaded.Favourites = append(loaded.Favourites, f)
	return save()
}

// RemoveFavourite removes a favourite entry if present.
func RemoveFavourite(host, project, slug string) error {
	out := loaded.Favourites[:0]
	for _, f := range loaded.Favourites {
		if f.Host == host && f.Project == project && f.Slug == slug {
			continue
		}
		out = append(out, f)
	}
	loaded.Favourites = out
	return save()
}

func save() error {
	if path == "" {
		return fmt.Errorf("config path not set")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(&loaded)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
