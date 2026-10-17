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
	sem    chan struct{}
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
	maxJobs := cfg.MaxConcurrent()
	if maxJobs <= 0 {
		maxJobs = 1
	}
	return &Runner{
		cfg:    cfg,
		log:    log,
		client: client.New(cfg, version),
		exec:   ex,
		sem:    make(chan struct{}, maxJobs),
	}, nil
}

// Register validates connectivity with the main service and reports info.
func (r *Runner) Register(ctx context.Context) error {
	return r.client.Register(ctx)
}

// Run enters the loop until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	r.log.Info("runner loop started",
		"runner_id", r.cfg.Core.ID,
		"server", r.cfg.Core.Server,
		"executor", r.cfg.Executor.Type,
	)
	// Best-effort registration/connectivity check at startup.
	if err := r.client.Register(ctx); err != nil {
		r.log.Warn("register failed (continuing)", "err", err)
	}

	beat := time.NewTicker(r.cfg.Client.HeartbeatInterval.Std())
	defer beat.Stop()
	poll := time.NewTicker(r.cfg.Client.PollInterval.Std())
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("shutting down")
			return nil
		case <-beat.C:
			hctx, cancel := context.WithTimeout(ctx, r.cfg.Client.RequestTimeout.Std())
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
	// Wait for a free slot before polling so we never claim more jobs than we
	// can run concurrently.
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return nil
	}
	job, err := r.client.Poll(ctx)
	if err != nil {
		<-r.sem
		return err
	}
	if job == nil {
		<-r.sem
		return nil
	}
	go func() {
		defer func() { <-r.sem }()
		r.execute(ctx, job)
	}()
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
	timeout := r.cfg.Execution.JobTimeout.Std()
	if job.TimeoutSec > 0 && time.Duration(job.TimeoutSec)*time.Second < timeout {
		timeout = time.Duration(job.TimeoutSec) * time.Second
	}
	var cancel context.CancelFunc
	jobCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

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
func (r *Runner) runJob(ctx context.Context, job *client.Job, out io.Writer) (code int, err error) {
	root := r.workRoot()
	if root != "" {
		if merr := os.MkdirAll(root, 0o750); merr != nil {
			return 1, merr
		}
	}
	ws, werr := os.MkdirTemp(root, "job-")
	if werr != nil {
		return 1, werr
	}
	success := false
	defer func() {
		// cleanup_policy: always | never | success
		if r.cfg.CleanupAfter(success) {
			_ = os.RemoveAll(ws)
		} else {
			fmt.Fprintf(out, "[gitli-runner] work dir kept: %s\n", ws)
		}
	}()

	if cerr := r.clone(ctx, job, ws, out); cerr != nil {
		return 1, cerr
	}

	wf, perr := workflow.Parse([]byte(job.Workflow))
	if perr != nil {
		return 1, perr
	}
	script := wf.Script()

	// Global env from [environment].variables, overridden by job env.
	envMap := map[string]string{}
	for k, v := range r.cfg.Environment.Variables {
		envMap[k] = v
	}
	for k, v := range job.Env {
		envMap[k] = v
	}
	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	shell := wf.EffectiveShell(r.cfg.Environment.DefaultShell)
	if rerr := r.exec.RunWithShell(ctx, ws, script, env, shell, out); rerr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(out, "[gitli-runner] job timed out")
			return 124, rerr
		}
		return 1, rerr
	}
	success = true
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
	if r.cfg.Execution.WorkDir != "" {
		return r.cfg.Execution.WorkDir
	}
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
