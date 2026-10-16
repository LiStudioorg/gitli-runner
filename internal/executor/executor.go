// SPDX-License-Identifier: AGPL-3.0
// Package executor runs a workflow inside an isolated environment.
package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/LiStudioorg/gitli-runner/internal/config"
)

// Executor runs a shell script and streams its output.
type Executor interface {
	// Run executes the script with the given environment, streaming output to out.
	Run(ctx context.Context, workspace, script string, env []string, out io.Writer) error
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

// shellExecutor runs the script directly on the host (debug only).
type shellExecutor struct {
	shell string
	cfg   *config.ExecutorConfig
}

func (e *shellExecutor) Run(ctx context.Context, workspace, script string, env []string, out io.Writer) error {
	scriptPath := workspace + "/.gitli-run.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, e.shell, scriptPath)
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
	args = append(args, e.cfg.Image, e.cfg.Shell.Shell)
	// The script is passed on stdin so it is never interpolated into argv.
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run: %w", e.bin, err)
	}
	return nil
}
