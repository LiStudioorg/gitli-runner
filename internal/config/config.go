// SPDX-License-Identifier: AGPL-3.0
// Package config loads the runner TOML configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration.
type Config struct {
	Core        CoreConfig        `toml:"core"`
	Execution   ExecutionConfig   `toml:"execution"`
	Client      ClientConfig      `toml:"client"`
	Environment EnvironmentConfig `toml:"environment"`
	Executor    ExecutorConfig    `toml:"executor"`
	Git         GitConfig         `toml:"git"`
	Log         LogConfig         `toml:"log"`
	Cache       CacheConfig       `toml:"cache"`

	// Legacy [runner] section, still accepted for backward compatibility.
	Runner RunnerConfig `toml:"runner"`
}

// CoreConfig holds identity and connection info (required).
type CoreConfig struct {
	Server string   `toml:"server"`
	ID     string   `toml:"id"`
	Token  string   `toml:"token"`
	Labels []string `toml:"labels"`
}

// ExecutionConfig controls concurrency, timeouts and cleanup.
type ExecutionConfig struct {
	MaxConcurrentJobs int      `toml:"max_concurrent_jobs"`
	JobTimeout        Duration `toml:"job_timeout"`
	WorkDir           string   `toml:"work_dir"`
	CleanupPolicy     string   `toml:"cleanup_policy"` // always | never | success
}

// ClientConfig controls polling and request timeouts.
type ClientConfig struct {
	PollInterval      Duration `toml:"poll_interval"`
	HeartbeatInterval Duration `toml:"heartbeat_interval"`
	RequestTimeout    Duration `toml:"request_timeout"`
	TLSSkipVerify     bool     `toml:"tls_skip_verify"`
}

// EnvironmentConfig configures inherited environment and shell.
type EnvironmentConfig struct {
	Variables    map[string]string `toml:"variables"`
	DefaultShell string            `toml:"default_shell"`
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
	Level      string `toml:"level"`
	Format     string `toml:"format"`
	File       string `toml:"file"`
	MaxLogSize Size   `toml:"max_log_size"`
}

// CacheConfig configures the local cache directory.
type CacheConfig struct {
	Dir    string `toml:"dir"`
	Enable bool   `toml:"enable"`
}

// RunnerConfig is the deprecated connection section.
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

// MarshalText renders the duration.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Size is a byte size parsed from strings like "10MB", "512MiB", "1g".
type Size int64

// UnmarshalText parses a human readable size.
func (s *Size) UnmarshalText(text []byte) error {
	v, err := ParseSize(string(text))
	if err != nil {
		return err
	}
	*s = Size(v)
	return nil
}

// MarshalText renders the size in bytes.
func (s Size) MarshalText() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(s), 10)), nil
}

// Bytes returns the size as int64.
func (s Size) Bytes() int64 { return int64(s) }

// ParseSize parses "10MB", "512KiB", "1G", "1048576" into bytes.
func ParseSize(v string) (int64, error) {
	v = strings.TrimSpace(strings.ToUpper(v))
	if v == "" {
		return 0, fmt.Errorf("empty size")
	}
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"TIB", 1 << 40}, {"TB", 1e12}, {"T", 1e12},
		{"GIB", 1 << 30}, {"GB", 1e9}, {"G", 1e9},
		{"MIB", 1 << 20}, {"MB", 1e6}, {"M", 1e6},
		{"KIB", 1 << 10}, {"KB", 1e3}, {"K", 1e3},
		{"B", 1},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(v, m.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(v, m.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q", v)
			}
			return int64(f * float64(m.mult)), nil
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", v)
	}
	return n, nil
}

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
	migrateLegacy(cfg)
	applyDefaults(cfg)
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// migrateLegacy folds the deprecated [runner] section into the new sections.
func migrateLegacy(cfg *Config) {
	r := &cfg.Runner
	if r.Server != "" && cfg.Core.Server == "" {
		cfg.Core.Server = r.Server
	}
	if r.ID != "" && cfg.Core.ID == "" {
		cfg.Core.ID = r.ID
	}
	if r.Token != "" && cfg.Core.Token == "" {
		cfg.Core.Token = r.Token
	}
	if len(r.Labels) > 0 && len(cfg.Core.Labels) == 0 {
		cfg.Core.Labels = r.Labels
	}
	if r.PollInterval != 0 && cfg.Client.PollInterval == 0 {
		cfg.Client.PollInterval = r.PollInterval
	}
	if r.HeartbeatInterval != 0 && cfg.Client.HeartbeatInterval == 0 {
		cfg.Client.HeartbeatInterval = r.HeartbeatInterval
	}
	if r.RequestTimeout != 0 && cfg.Client.RequestTimeout == 0 {
		cfg.Client.RequestTimeout = r.RequestTimeout
	}
	if r.TLSSkipVerify {
		cfg.Client.TLSSkipVerify = true
	}
}

func defaults() *Config {
	return &Config{}
}

func applyDefaults(cfg *Config) {
	if cfg.Core.Labels == nil {
		cfg.Core.Labels = []string{"self-hosted"}
	}
	c := &cfg.Client
	if c.PollInterval == 0 {
		c.PollInterval = Duration(5 * time.Second)
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = Duration(30 * time.Second)
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = Duration(10 * time.Second)
	}
	e := &cfg.Execution
	if e.MaxConcurrentJobs < 0 {
		e.MaxConcurrentJobs = 0
	}
	if e.MaxConcurrentJobs == 0 {
		e.MaxConcurrentJobs = 1
	}
	if e.JobTimeout == 0 {
		e.JobTimeout = Duration(30 * time.Minute)
	}
	if e.CleanupPolicy == "" {
		e.CleanupPolicy = "always"
	}
	breakBeforeRefresh := strings.ToLower(e.CleanupPolicy)
	if breakBeforeRefresh != "always" && breakBeforeRefresh != "never" && breakBeforeRefresh != "success" {
		e.CleanupPolicy = "always"
	}

	ex := &cfg.Executor
	if ex.Type == "" {
		ex.Type = "docker"
	}
	if ex.Image == "" {
		ex.Image = "alpine:3.20"
	}
	if ex.Timeout == 0 {
		ex.Timeout = e.JobTimeout
	}
	if ex.Workdir == "" {
		ex.Workdir = "/workspace"
	}
	if ex.Shell.Shell == "" {
		ex.Shell.Shell = "/bin/sh"
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
	if cfg.Log.MaxLogSize == 0 {
		cfg.Log.MaxLogSize = Size(10 << 20)
	}
	if cfg.Cache.Dir == "" {
		cfg.Cache.Dir = "/var/lib/gitli-runner"
	}
	if cfg.Environment.Variables == nil {
		cfg.Environment.Variables = map[string]string{}
	}
}

func validate(cfg *Config) error {
	if cfg.Core.Server == "" {
		return fmt.Errorf("core.server is required")
	}
	if cfg.Core.ID == "" {
		return fmt.Errorf("core.id is required")
	}
	if cfg.Core.Token == "" {
		return fmt.Errorf("core.token is required")
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

// MaxConcurrent returns the effective concurrency (never unlimited unless
// explicitly configured as a large number).
func (c *Config) MaxConcurrent() int {
	if c.Execution.MaxConcurrentJobs > 0 {
		return c.Execution.MaxConcurrentJobs
	}
	return 1
}

// CleanupAfter reports whether a job's work dir should be removed given success.
func (c *Config) CleanupAfter(success bool) bool {
	switch strings.ToLower(c.Execution.CleanupPolicy) {
	case "never":
		return false
	case "success":
		return success
	default:
		return true
	}
}
