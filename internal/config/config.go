// Package config handles persisted bb configuration.
//
// Non-secret config lives at $XDG_CONFIG_HOME/bb/config.yml. Secrets (HTTP
// access tokens, app passwords) are stored via 99designs/keyring, which
// tries OS-native backends first (Keychain on macOS, Credential Manager on
// Windows, Secret Service on Linux) and falls back to an AES-encrypted file
// at $XDG_CONFIG_HOME/bb/keyring/. The fallback is what makes WSL and
// headless boxes work.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
	"github.com/charmbracelet/huh"
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

// Load reads the config from disk and pulls tokens from the keyring.
// A missing config file is not an error.
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

	// Backfill tokens from the keyring; tolerate missing entries so the
	// CLI still starts when the keyring is unavailable.
	if ring, err := openKeyring(); err == nil {
		for name, h := range loaded.Hosts {
			item, err := ring.Get(name)
			if err == nil {
				h.Token = string(item.Data)
				loaded.Hosts[name] = h
			}
		}
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

// SetHost adds or updates a host. The token is stored via the keyring;
// the rest is written to the YAML config file.
func SetHost(name string, h Host) error {
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
	}

	if h.Token != "" {
		ring, err := openKeyring()
		if err != nil {
			return fmt.Errorf("open keyring: %w", err)
		}
		if err := ring.Set(keyring.Item{
			Key:         name,
			Data:        []byte(h.Token),
			Label:       "bb-cli token for " + name,
			Description: "Bitbucket CLI access token",
		}); err != nil {
			return fmt.Errorf("save token to keyring: %w", err)
		}
	}

	stored := h
	stored.Token = "" // never on disk
	loaded.Hosts[name] = stored
	if loaded.DefaultHost == "" {
		loaded.DefaultHost = name
	}
	if err := save(); err != nil {
		return err
	}
	// keep populated in memory for the rest of this run
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
	if ring, err := openKeyring(); err == nil {
		_ = ring.Remove(name)
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

// openKeyring configures 99designs/keyring to try OS-native backends first
// and fall back to an AES-encrypted file (so WSL / headless boxes work).
func openKeyring() (keyring.Keyring, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	fileDir := filepath.Join(dir, "bb", "keyring")
	return keyring.Open(keyring.Config{
		ServiceName: keyringService,

		// macOS / Windows
		KeychainName:                   "login",
		KeychainTrustApplication:        true,
		KeychainSynchronizable:          false,
		KeychainAccessibleWhenUnlocked: true,

		// Linux Secret Service (gnome-keyring / kwallet)
		LibSecretCollectionName: keyringService,

		// File fallback (encrypted with passphrase)
		FileDir:          fileDir,
		FilePasswordFunc: filePassword,

		// Try in this order; the first that works wins.
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.WinCredBackend,
			keyring.SecretServiceBackend,
			keyring.FileBackend,
		},
	})
}

// filePassword resolves the passphrase for the file backend. Prefers
// $BB_KEYRING_PASSWORD; otherwise prompts the user once.
func filePassword(prompt string) (string, error) {
	if p := os.Getenv("BB_KEYRING_PASSWORD"); p != "" {
		return p, nil
	}
	var pw string
	err := huh.NewInput().
		Title("bb keyring passphrase").
		Description(prompt + " (set BB_KEYRING_PASSWORD to skip this prompt)").
		EchoMode(huh.EchoModePassword).
		Value(&pw).
		Run()
	if err != nil {
		return "", err
	}
	return pw, nil
}
