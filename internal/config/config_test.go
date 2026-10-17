// SPDX-License-Identifier: AGPL-3.0
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "runner.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseRichConfig(t *testing.T) {
	p := writeCfg(t, `
[core]
server = "https://git.example.com"
id = "runner-x"
token = "t"
labels = ["linux","amd64"]

[execution]
max_concurrent_jobs = 3
job_timeout = "45m"
work_dir = "/tmp/work"
cleanup_policy = "success"

[client]
poll_interval = "7s"
heartbeat_interval = "20s"
request_timeout = "12s"

[environment]
variables = { A = "1", B = "2" }
default_shell = "bash"

[log]
level = "debug"
format = "json"
max_log_size = "5MB"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Execution.MaxConcurrentJobs != 3 {
		t.Fatalf("max jobs = %d", cfg.Execution.MaxConcurrentJobs)
	}
	if cfg.Execution.JobTimeout.Std() != 45*time.Minute {
		t.Fatalf("job timeout = %v", cfg.Execution.JobTimeout.Std())
	}
	if cfg.Execution.CleanupPolicy != "success" {
		t.Fatalf("cleanup = %s", cfg.Execution.CleanupPolicy)
	}
	if cfg.Client.PollInterval.Std() != 7*time.Second {
		t.Fatalf("poll = %v", cfg.Client.PollInterval.Std())
	}
	if cfg.Environment.Variables["A"] != "1" || cfg.Environment.Variables["B"] != "2" {
		t.Fatalf("vars = %v", cfg.Environment.Variables)
	}
	if cfg.Log.MaxLogSize.Bytes() != 5*1000*1000 {
		t.Fatalf("max log size = %d", cfg.Log.MaxLogSize.Bytes())
	}
}

func TestDefaults(t *testing.T) {
	p := writeCfg(t, `
[core]
server = "https://git.example.com"
id = "r"
token = "t"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConcurrent() != 1 {
		t.Fatalf("default concurrency = %d", cfg.MaxConcurrent())
	}
	if cfg.Execution.JobTimeout.Std() != 30*time.Minute {
		t.Fatalf("default timeout = %v", cfg.Execution.JobTimeout.Std())
	}
	if cfg.Execution.CleanupPolicy != "always" {
		t.Fatalf("default cleanup = %s", cfg.Execution.CleanupPolicy)
	}
	if cfg.Client.PollInterval.Std() != 5*time.Second || cfg.Client.RequestTimeout.Std() != 10*time.Second {
		t.Fatalf("client defaults wrong")
	}
	if !cfg.CleanupAfter(false) || !cfg.CleanupAfter(true) {
		t.Fatal("always cleanup should remove on both outcomes")
	}
}

func TestCleanupPolicy(t *testing.T) {
	cfg := &Config{Execution: ExecutionConfig{CleanupPolicy: "success"}}
	if cfg.CleanupAfter(false) {
		t.Fatal("success policy should keep on failure")
	}
	if !cfg.CleanupAfter(true) {
		t.Fatal("success policy should remove on success")
	}
	cfg.Execution.CleanupPolicy = "never"
	if cfg.CleanupAfter(true) {
		t.Fatal("never policy should keep always")
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"1KB": 1000, "1KiB": 1024, "10MB": 10000000, "2g": 2000000000, "512": 512,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Fatalf("ParseSize(%q) = %d %v, want %d", in, got, err, want)
		}
	}
	if _, err := ParseSize("nonsense"); err == nil {
		t.Fatal("expected error for invalid size")
	}
}

func TestLegacyMigrates(t *testing.T) {
	p := writeCfg(t, `
[runner]
server = "https://legacy.example.com"
id = "legacy"
token = "lt"
poll_interval = "3s"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Core.Server != "https://legacy.example.com" || cfg.Core.ID != "legacy" {
		t.Fatalf("legacy migration failed: %+v", cfg.Core)
	}
	if cfg.Client.PollInterval.Std() != 3*time.Second {
		t.Fatalf("legacy poll = %v", cfg.Client.PollInterval.Std())
	}
}

func TestValidateRequiresCore(t *testing.T) {
	p := writeCfg(t, "[core]\nserver=\"x\"\n")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing id/token")
	}
}
