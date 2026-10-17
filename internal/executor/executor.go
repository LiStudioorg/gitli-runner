// SPDX-License-Identifier: AGPL-3.0
// Package executor runs a workflow inside an isolated environment.
package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LiStudioorg/gitli-runner/internal/config"
)

// Executor runs a shell script and streams its output.
type Executor interface {
	// Run executes the script with the given environment, streaming output to out.
	Run(ctx context.Context, workspace, script string, env []string, out io.Writer) error
	// RunWithShell executes the script using an explicit shell (e.g. "bash",
	// "pwsh", "cmd"). An empty shell means the executor's default.
	RunWithShell(ctx context.Context, workspace, script string, env []string, shell string, out io.Writer) error
}

// New builds the executor selected by configuration.
func New(cfg *config.ExecutorConfig) (Executor, error) {
	switch cfg.Type {
	case "docker":
		return newContainerExecutor("docker", cfg), nil
	case "podman":
		return newContainerExecutor("podman", cfg), nil
	case "shell":
		if !cfg.Shell.Enable {
			return nil, fmt.Errorf("shell executor is disabled; set executor.shell.enable=true to allow it")
		}
		return &shellExecutor{shell: cfg.Shell.Shell, cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown executor type %q", cfg.Type)
	}
}

// resolveShell finds the shell binary and returns the argv prefix used to run a
// script file. A Windows cmd shell uses "/c".
func resolveShell(shell string) (string, []string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}
	// Allow absolute paths and bare names.
	bin := shell
	if !filepath.IsAbs(shell) {
		found, err := exec.LookPath(shell)
		if err != nil {
			return "", nil, fmt.Errorf("shell %q not found in PATH: %w", shell, err)
		}
		bin = found
	}
	return bin, nil, nil
}

// shellExecutor runs the script directly on the host (debug only).
type shellExecutor struct {
	shell string
	cfg   *config.ExecutorConfig
}

func (e *shellExecutor) Run(ctx context.Context, workspace, script string, env []string, out io.Writer) error {
	return e.RunWithShell(ctx, workspace, script, env, e.shell, out)
}

func (e *shellExecutor) RunWithShell(ctx context.Context, workspace, script string, env []string, shell string, out io.Writer) error {
	bin, _, err := resolveShell(firstNonEmpty(shell, e.shell))
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(workspace, ".gitli-run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}
	args := []string{scriptPath}
	if strings.EqualFold(filepath.Base(bin), "cmd") || strings.EqualFold(filepath.Base(bin), "cmd.exe") {
		args = []string{"/c", scriptPath}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workspace
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=C.UTF-8"}, env...)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// containerExecutor runs the script inside a docker/podman container.
type containerExecutor struct {
	bin string
	cfg *config.ExecutorConfig
}

func newContainerExecutor(bin string, cfg *config.ExecutorConfig) *containerExecutor {
	return &containerExecutor{bin: bin, cfg: cfg}
}

func (e *containerExecutor) Run(ctx context.Context, workspace, script string, env []string, out io.Writer) error {
	return e.RunWithShell(ctx, workspace, script, env, e.cfg.Shell.Shell, out)
}

func (e *containerExecutor) RunWithShell(ctx context.Context, workspace, script string, env []string, shell string, out io.Writer) error {
	// The container's shell: map common names to an in-image command.
	sh := firstNonEmpty(shell, e.cfg.Shell.Shell)
	sh = strings.TrimSpace(sh)
	if sh == "" {
		sh = "/bin/sh"
	}
	args := []string{"run", "--rm", "-i"}
	if e.cfg.AutoRemove {
		args = append(args, "--rm")
	}
	if e.cfg.Limits.CPUs != "" {
		args = append(args, "--cpus", e.cfg.Limits.CPUs)
	}
	if e.cfg.Limits.Memory != "" {
		args = append(args, "--memory", e.cfg.Limits.Memory)
	}
	if e.cfg.Limits.Pids > 0 {
		args = append(args, "--pids-limit", fmt.Sprintf("%d", e.cfg.Limits.Pids))
	}
	args = append(args, "-v", workspace+":"+e.cfg.Workdir, "-w", e.cfg.Workdir)
	if e.cfg.MountDockerSock {
		args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")
	}
	if e.cfg.NonRoot {
		args = append(args, "--user", "1000:1000")
	}
	for _, kv := range e.cfg.Env {
		args = append(args, "-e", kv)
	}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	args = append(args, e.cfg.Image, sh)
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run: %w", e.bin, err)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
