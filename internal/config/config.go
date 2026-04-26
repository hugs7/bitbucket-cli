// Package config handles persisted bb configuration.
//
// Non-secret config lives at $XDG_CONFIG_HOME/bb/config.yml (typically
// ~/.config/bb/config.yml). Secrets (HTTP access tokens, app passwords) are
// stored in the OS keyring (libsecret on Linux, Keychain on macOS, Windows
// Credential Manager) and never written to disk.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const keyringService = "bb-cli"

type Host struct {
	Type     string `yaml:"type"`               // "cloud" or "server"
	Username string `yaml:"username"`           // user identifier
	APIBase  string `yaml:"api_base,omitempty"` // for server: https://host/rest/api/1.0
	Token    string `yaml:"-"`                  // populated from keyring at load
}

type Config struct {
	DefaultHost string          `yaml:"default_host"`
	Hosts       map[string]Host `yaml:"hosts"`
}

var (
	loaded Config
	path   string
)

// Load reads the config from disk and pulls tokens from the OS keyring.
// A missing file is not an error.
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
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config %s: %w", p, err)
	}
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
	}
	// Backfill tokens from the keyring; tolerate missing entries.
	for name, h := range loaded.Hosts {
		if tok, err := keyring.Get(keyringService, name); err == nil {
			h.Token = tok
			loaded.Hosts[name] = h
		}
	}
	// Allow env var override for one-off use.
	if envTok := os.Getenv("BB_TOKEN"); envTok != "" && loaded.DefaultHost != "" {
		h := loaded.Hosts[loaded.DefaultHost]
		h.Token = envTok
		loaded.Hosts[loaded.DefaultHost] = h
	}
	return nil
}

// Get returns the loaded config.
func Get() Config { return loaded }

// SetHost adds or updates a host. Token is stored in the OS keyring; the
// rest is written to the YAML config file.
func SetHost(name string, h Host) error {
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
	}

	// Persist the secret to the keyring (or fall back to the file if the
	// keyring isn't available — better to work than to fail outright).
	keyringOK := true
	if h.Token != "" {
		if err := keyring.Set(keyringService, name, h.Token); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not save token to OS keyring (%v); falling back to plaintext config\n", err)
			keyringOK = false
		}
	}

	stored := h
	if keyringOK {
		stored.Token = "" // not written to disk
	}
	loaded.Hosts[name] = stored
	if loaded.DefaultHost == "" {
		loaded.DefaultHost = name
	}
	if err := save(); err != nil {
		return err
	}
	// Keep the in-memory copy populated for the rest of this run.
	stored.Token = h.Token
	loaded.Hosts[name] = stored
	return nil
}

// RemoveHost deletes a host's config and keyring entry.
func RemoveHost(name string) error {
	delete(loaded.Hosts, name)
	if loaded.DefaultHost == name {
		loaded.DefaultHost = ""
		for n := range loaded.Hosts {
			loaded.DefaultHost = n
			break
		}
	}
	_ = keyring.Delete(keyringService, name)
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
