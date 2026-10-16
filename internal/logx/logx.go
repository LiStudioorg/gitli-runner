// SPDX-License-Identifier: AGPL-3.0
// Package logx builds the slog logger from configuration.
package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/LiStudioorg/gitli-runner/internal/config"
)

// New builds a slog.Logger and a closer for any opened log file.
func New(cfg config.LogConfig) (*slog.Logger, func(), error) {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var w io.Writer = os.Stderr
	closer := func() {}
	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
		if err != nil {
			return nil, nil, err
		}
		w = f
		closer = func() { _ = f.Close() }
	}

	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if strings.ToLower(cfg.Format) == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h), closer, nil
}
