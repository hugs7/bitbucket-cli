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
	Hosts       map[string]Host `yaml:"hosts"`
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
