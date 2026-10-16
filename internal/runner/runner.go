// SPDX-License-Identifier: AGPL-3.0
// Package runner implements the task loop: poll, prepare, execute, report.
package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/LiStudioorg/gitli-runner/internal/client"
	"github.com/LiStudioorg/gitli-runner/internal/config"
	"github.com/LiStudioorg/gitli-runner/internal/executor"
	"github.com/LiStudioorg/gitli-runner/internal/workflow"
)

// Runner is the task execution loop.
type Runner struct {
	cfg    *config.Config
	log    *slog.Logger
	client *client.Client
	exec   executor.Executor
}

// New builds a Runner.
func New(cfg *config.Config, log *slog.Logger, version string) (*Runner, error) {
	if cfg.Cache.Enable {
		if err := os.MkdirAll(cfg.Cache.Dir, 0o750); err != nil {
			return nil, fmt.Errorf("create cache dir %s: %w", cfg.Cache.Dir, err)
		}
	}
	ex, err := executor.New(&cfg.Executor)
	if err != nil {
		return nil, err
	}
	return &Runner{
		cfg:    cfg,
		log:    log,
		client: client.New(cfg, version),
		exec:   ex,
	}, nil
}

// Register validates connectivity with the main service and reports info.
func (r *Runner) Register(ctx context.Context) error {
	return r.client.Register(ctx)
}

// Run enters the loop until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	r.log.Info("runner loop started",
		"runner_id", r.cfg.Runner.ID,
		"server", r.cfg.Runner.Server,
		"executor", r.cfg.Executor.Type,
	)
	// Best-effort registration/connectivity check at startup.
	if err := r.client.Register(ctx); err != nil {
		r.log.Warn("register failed (continuing)", "err", err)
	}

	beat := time.NewTicker(r.cfg.Runner.HeartbeatInterval.Std())
	defer beat.Stop()
	poll := time.NewTicker(r.cfg.Runner.PollInterval.Std())
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("shutting down")
			return nil
		case <-beat.C:
			hctx, cancel := context.WithTimeout(ctx, r.cfg.Runner.RequestTimeout.Std())
			if err := r.client.Heartbeat(hctx); err != nil {
				r.log.Warn("heartbeat failed", "err", err)
			}
			cancel()
		case <-poll.C:
			if err := r.pollOnce(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("poll failed", "err", err)
			}
		}
	}
}

func (r *Runner) pollOnce(ctx context.Context) error {
	job, err := r.client.Poll(ctx)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}
	r.execute(ctx, job)
	return nil
}

func (r *Runner) execute(ctx context.Context, job *client.Job) {
	log := r.log.With("job_id", job.ID, "ref", job.Ref, "sha", job.SHA)
	log.Info("job received", "repo", job.RepoURL)

	result := client.Result{Status: "success"}

	logw, err := r.client.LogWriter(ctx, job.ID)
	if err != nil {
		log.Error("open log channel", "err", err)
		_ = r.client.Report(ctx, job.ID, client.Result{Status: "failed", ExitCode: 1, Error: err.Error()})
		return
	}

	jobCtx := ctx
	if job.TimeoutSec > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(ctx, time.Duration(job.TimeoutSec)*time.Second)
		defer cancel()
	}

	code, err := r.runJob(jobCtx, job, logw)
	_ = logw.Close()
	if err != nil {
		result.Status = "failed"
		result.ExitCode = code
		result.Error = err.Error()
		log.Error("job failed", "err", err, "exit", code)
	} else {
		result.ExitCode = 0
	}
	if rerr := r.client.Report(ctx, job.ID, result); rerr != nil {
		log.Warn("report result failed", "err", rerr)
	}
	log.Info("job finished", "status", result.Status)
}

// runJob prepares the workspace and executes the workflow, returning the exit code.
func (r *Runner) runJob(ctx context.Context, job *client.Job, out io.Writer) (int, error) {
	ws, err := os.MkdirTemp(r.workRoot(), "job-")
	if err != nil {
		return 1, err
	}
	defer func() { _ = os.RemoveAll(ws) }()

	if err := r.clone(ctx, job, ws, out); err != nil {
		return 1, err
	}

	wf, err := workflow.Parse([]byte(job.Workflow))
	if err != nil {
		return 1, err
	}
	script := wf.Script()

	env := []string{}
	for k, v := range job.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	if err := r.exec.Run(ctx, ws, script, env, out); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(out, "[gitli-runner] job timed out")
			return 124, err
		}
		return 1, err
	}
	return 0, nil
}

// clone fetches the repository at the requested commit into ws.
func (r *Runner) clone(ctx context.Context, job *client.Job, ws string, out io.Writer) error {
	args := []string{"clone"}
	if r.cfg.Git.Shallow {
		args = append(args, "--depth", fmt.Sprintf("%d", r.cfg.Git.Depth))
	}
	url := job.RepoURL
	if job.Token != "" {
		url = injectToken(url, job.Token)
	}
	args = append(args, url, ws+"/repo")
	cmd := exec.CommandContext(ctx, r.cfg.Git.Binary, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GIT_TERMINAL_PROMPT=0", "LANG=C.UTF-8"}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func (r *Runner) workRoot() string {
	if r.cfg.Cache.Enable {
		return r.cfg.Cache.Dir
	}
	return os.TempDir()
}

// injectToken embeds a token into an https clone URL.
func injectToken(rawURL, token string) string {
	const p = "https://"
	if len(rawURL) > len(p) && rawURL[:len(p)] == p {
		return p + "x-access-token:" + token + "@" + rawURL[len(p):]
	}
	return rawURL
}
