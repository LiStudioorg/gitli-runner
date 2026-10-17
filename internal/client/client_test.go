// SPDX-License-Identifier: AGPL-3.0
package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LiStudioorg/gitli-runner/internal/config"
)

func testConfig(server string) *config.Config {
	c := &config.Config{}
	c.Core.Server = server
	c.Core.ID = "runner-test"
	c.Core.Token = "glr_secret"
	c.Client.PollInterval = config.Duration(10 * time.Second)
	c.Client.RequestTimeout = config.Duration(10 * time.Second)
	return c
}

func TestPollAuthAndDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer glr_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Runner-ID") != "runner-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42","ref":"refs/heads/main","sha":"abc","workflow":"name: ci"}`))
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL), "test")
	job, err := c.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != "42" || job.SHA != "abc" {
		t.Fatalf("job = %+v", job)
	}
}

func TestPollNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(testConfig(srv.URL), "test")
	job, err := c.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("expected nil job, got %+v", job)
	}
}

func TestLogWriterStreams(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		buf := make([]byte, 1024)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		received <- b.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(testConfig(srv.URL), "test")
	lw, err := c.LogWriter(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lw.Write([]byte("hello "))
	_, _ = lw.Write([]byte("world"))
	if err := lw.Close(); err != nil {
		t.Fatal(err)
	}
	if got := <-received; got != "hello world" {
		t.Fatalf("server received %q", got)
	}
}

func TestHeartbeatUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(testConfig(srv.URL), "test")
	if err := c.Heartbeat(context.Background()); err == nil {
		t.Fatal("expected heartbeat error on 401")
	}
}
