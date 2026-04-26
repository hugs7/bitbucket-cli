// Package config handles persisted bb configuration.
//
// Config lives at $XDG_CONFIG_HOME/bb/config.yml (typically
// ~/.config/bb/config.yml). Tokens are stored alongside the config for now;
// a future iteration will move them to the OS keyring.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Host struct {
	Type     string `mapstructure:"type" yaml:"type"`         // "cloud" or "server"
	Username string `mapstructure:"username" yaml:"username"` // user identifier
	Token    string `mapstructure:"token" yaml:"token"`       // app password / HTTP access token
	APIBase  string `mapstructure:"api_base" yaml:"api_base"` // for server: https://host/rest/api/1.0
}

type Config struct {
	DefaultHost string          `mapstructure:"default_host" yaml:"default_host"`
	Hosts       map[string]Host `mapstructure:"hosts" yaml:"hosts"`
}

var (
	loaded Config
	path   string
)

// Load reads the config from disk. If overridePath is empty the default
// location is used. A missing file is not an error.
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

	v := viper.New()
	v.SetConfigFile(p)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("BB")
	v.AutomaticEnv()

	if _, err := os.Stat(p); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("read config %s: %w", p, err)
		}
	}

	loaded = Config{Hosts: map[string]Host{}}
	if err := v.Unmarshal(&loaded); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if loaded.Hosts == nil {
		loaded.Hosts = map[string]Host{}
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
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.Set("default_host", loaded.DefaultHost)
	v.Set("hosts", loaded.Hosts)
	return v.WriteConfigAs(path)
}
