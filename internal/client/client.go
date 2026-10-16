// SPDX-License-Identifier: AGPL-3.0
// Package client implements the HTTP protocol between the runner and the
// gitli main service.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/LiStudioorg/gitli-runner/internal/config"
)

// Client is the HTTP client for the gitli main service.
type Client struct {
	base    string
	id      string
	tok     string
	labels  []string
	version string
	http    *http.Client
}

// Job is a task dispatched by the main service.
type Job struct {
	ID         string            `json:"id"`
	RunID      string            `json:"run_id"`
	RepoURL    string            `json:"repo_url"`
	Ref        string            `json:"ref"`
	SHA        string            `json:"sha"`
	Token      string            `json:"token"`
	Workflow   string            `json:"workflow"`
	Env        map[string]string `json:"env"`
	TimeoutSec int               `json:"timeout_sec"`
}

// Result is the job execution result.
type Result struct {
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// New builds a Client from configuration.
func New(cfg *config.Config, version string) *Client {
	tr := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	if cfg.Runner.TLSSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in via config
	}
	return &Client{
		base:    strings.TrimRight(cfg.Runner.Server, "/"),
		id:      cfg.Runner.ID,
		tok:     cfg.Runner.Token,
		labels:  cfg.Runner.Labels,
		version: version,
		http: &http.Client{
			Timeout:   cfg.Runner.RequestTimeout.Std(),
			Transport: tr,
		},
	}
}

// Poll fetches the next job; returns (nil, nil) when none is available.
func (c *Client) Poll(ctx context.Context) (*Job, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/runner/jobs/next", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	var job Job
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&job); err != nil {
		return nil, fmt.Errorf("decode job: %w", err)
	}
	return &job, nil
}

// Heartbeat reports liveness and capabilities.
func (c *Client) Heartbeat(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"runner_id": c.id,
		"status":    "active",
		"version":   c.version,
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"labels":    c.labels,
	})
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/runner/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	drain(resp.Body)
	return checkStatus(resp)
}

// Register reports capability info and validates connectivity.
func (c *Client) Register(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"id":      c.id,
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"labels":  c.labels,
		"version": c.version,
	})
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/runner/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	drain(resp.Body)
	return checkStatus(resp)
}

// LogWriter returns a streaming writer that POSTs log chunks to the service.
func (c *Client) LogWriter(ctx context.Context, jobID string) (io.WriteCloser, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/runner/jobs/"+jobID+"/logs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	pr, pw := io.Pipe()
	req.Body = pr

	done := make(chan error, 1)
	go func() {
		resp, err := c.http.Do(req)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		drain(resp.Body)
		done <- checkStatus(resp)
	}()
	return &logWriter{pw: pw, done: done}, nil
}

type logWriter struct {
	pw   *io.PipeWriter
	done chan error
}

// Write implements io.Writer.
func (w *logWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }

// Close flushes and waits for the server response.
func (w *logWriter) Close() error {
	err := w.pw.Close()
	werr := <-w.done
	if err == nil {
		err = werr
	}
	return err
}

// Report submits the final job result.
func (c *Client) Report(ctx context.Context, jobID string, result Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/runner/jobs/"+jobID+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("report result: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	drain(resp.Body)
	return checkStatus(resp)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tok)
	req.Header.Set("X-Runner-ID", c.id)
	req.Header.Set("User-Agent", "gitli-runner/"+c.version)
	return req, nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if len(msg) == 0 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// drain reads and discards a small trailing body to enable connection reuse.
func drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 4<<10))
}
