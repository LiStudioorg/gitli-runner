// SPDX-License-Identifier: AGPL-3.0
// Command gitli-runner is the standalone runner for the gitli Git service.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/LiStudioorg/gitli-runner/internal/config"
	"github.com/LiStudioorg/gitli-runner/internal/logx"
	"github.com/LiStudioorg/gitli-runner/internal/runner"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "run":
		err = runCmd(ctx, os.Args[2:])
	case "register":
		err = registerCmd(ctx, os.Args[2:])
	case "version", "-v", "--version":
		fmt.Printf("gitli-runner %s (commit %s, built %s)\n", version, commit, date)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitli-runner:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gitli-runner — standalone runner for gitli

Usage:
  gitli-runner run      -c runner.toml   start the runner loop
  gitli-runner register -c runner.toml   validate connectivity and report info
  gitli-runner version                    print version
  gitli-runner help                       show this help
`)
}

func runCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("c", "runner.toml", "path to runner.toml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	logger, closer, err := logx.New(cfg.Log)
	if err != nil {
		return err
	}
	defer closer()

	r, err := runner.New(cfg, logger, version)
	if err != nil {
		return err
	}
	return r.Run(ctx)
}

func registerCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	cfgPath := fs.String("c", "runner.toml", "path to runner.toml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	logger, closer, err := logx.New(cfg.Log)
	if err != nil {
		return err
	}
	defer closer()

	r, err := runner.New(cfg, logger, version)
	if err != nil {
		return err
	}
	if err := r.Register(ctx); err != nil {
		return err
	}
	fmt.Println("registration ok:", cfg.Runner.ID)
	return nil
}
