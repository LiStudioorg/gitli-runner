// SPDX-License-Identifier: AGPL-3.0
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runner.toml")
	content := `
[runner]
server = "https://git.example.com"
id = "runner-abc"
token = "glr_token"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Executor.Type != "docker" {
		t.Fatalf("default executor type = %q", cfg.Executor.Type)
	}
	if cfg.Runner.PollInterval.Std().Seconds() != 10 {
		t.Fatalf("default poll interval = %v", cfg.Runner.PollInterval.Std())
	}
	if cfg.Git.Binary != "git" {
		t.Fatalf("default git binary = %q", cfg.Git.Binary)
	}
}

func TestValidateRequiresCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runner.toml")
	_ = os.WriteFile(path, []byte("[runner]\nserver = \"https://x\"\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for missing id/token")
	}
}

func TestShellExecutorRequiresEnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runner.toml")
	content := "[runner]\nserver=\"https://x\"\nid=\"r\"\ntoken=\"t\"\n[executor]\ntype=\"shell\"\n"
	_ = os.WriteFile(path, []byte(content), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error: shell executor must be explicitly enabled")
	}
}
