// SPDX-License-Identifier: AGPL-3.0
// Package config loads the runner TOML configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration.
type Config struct {
	Runner   RunnerConfig   `toml:"runner"`
	Executor ExecutorConfig `toml:"executor"`
	Git      GitConfig      `toml:"git"`
	Log      LogConfig      `toml:"log"`
	Cache    CacheConfig    `toml:"cache"`
}

// RunnerConfig configures the connection to the gitli main service.
type RunnerConfig struct {
	Server            string   `toml:"server"`
	ID                string   `toml:"id"`
	Token             string   `toml:"token"`
	Labels            []string `toml:"labels"`
	PollInterval      Duration `toml:"poll_interval"`
	HeartbeatInterval Duration `toml:"heartbeat_interval"`
	RequestTimeout    Duration `toml:"request_timeout"`
	TLSSkipVerify     bool     `toml:"tls_skip_verify"`
}

// ExecutorConfig selects and configures the execution backend.
type ExecutorConfig struct {
	Type            string       `toml:"type"`
	Image           string       `toml:"image"`
	Timeout         Duration     `toml:"timeout"`
	Workdir         string       `toml:"workdir"`
	AutoRemove      bool         `toml:"auto_remove"`
	MountDockerSock bool         `toml:"mount_docker_sock"`
	Env             []string     `toml:"env"`
	NonRoot         bool         `toml:"non_root"`
	Shell           ShellConfig  `toml:"shell"`
	Limits          LimitsConfig `toml:"limits"`
}

// ShellConfig configures the debug shell backend.
type ShellConfig struct {
	Enable bool   `toml:"enable"`
	Shell  string `toml:"shell"`
}

// LimitsConfig bounds container resources.
type LimitsConfig struct {
	CPUs   string `toml:"cpus"`
	Memory string `toml:"memory"`
	Pids   int    `toml:"pids"`
}

// GitConfig configures repository fetching.
type GitConfig struct {
	Binary  string `toml:"binary"`
	Shallow bool   `toml:"shallow"`
	Depth   int    `toml:"depth"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
	File   string `toml:"file"`
}

// CacheConfig configures the local cache directory.
type CacheConfig struct {
	Dir    string `toml:"dir"`
	Enable bool   `toml:"enable"`
}

// Duration is a TOML-friendly duration (e.g. "30s", "5m").
type Duration time.Duration

// UnmarshalText parses a duration string.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	*d = Duration(v)
	return nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Load reads the configuration and applies defaults.
func Load(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("config file not found: %s", path)
			}
			return nil, fmt.Errorf("decode config %s: %w", path, err)
		}
	}
	applyDefaults(cfg)
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{}
}

func applyDefaults(cfg *Config) {
	r := &cfg.Runner
	if r.PollInterval == 0 {
		r.PollInterval = Duration(10 * time.Second)
	}
	if r.HeartbeatInterval == 0 {
		r.HeartbeatInterval = Duration(30 * time.Second)
	}
	if r.RequestTimeout == 0 {
		r.RequestTimeout = Duration(30 * time.Second)
	}
	e := &cfg.Executor
	if e.Type == "" {
		e.Type = "docker"
	}
	if e.Image == "" {
		e.Image = "alpine:3.20"
	}
	if e.Timeout == 0 {
		e.Timeout = Duration(30 * time.Minute)
	}
	if e.Workdir == "" {
		e.Workdir = "/workspace"
	}
	if e.Shell.Shell == "" {
		e.Shell.Shell = "/bin/sh"
	}
	if cfg.Git.Binary == "" {
		cfg.Git.Binary = "git"
	}
	if cfg.Git.Depth == 0 {
		cfg.Git.Depth = 1
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "text"
	}
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = "/var/lib/gitli-runner"
	}
}

func validate(cfg *Config) error {
	if cfg.Runner.Server == "" {
		return fmt.Errorf("runner.server is required")
	}
	if cfg.Runner.ID == "" {
		return fmt.Errorf("runner.id is required")
	}
	if cfg.Runner.Token == "" {
		return fmt.Errorf("runner.token is required")
	}
	switch cfg.Executor.Type {
	case "docker", "podman", "shell":
	default:
		return fmt.Errorf("executor.type must be docker, podman or shell (got %q)", cfg.Executor.Type)
	}
	if cfg.Executor.Type == "shell" && !cfg.Executor.Shell.Enable {
		return fmt.Errorf("executor.type=shell requires executor.shell.enable=true")
	}
	return nil
}
