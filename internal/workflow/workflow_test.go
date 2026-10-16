// SPDX-License-Identifier: AGPL-3.0
package workflow

import (
	"strings"
	"testing"
)

func TestParseAndScript(t *testing.T) {
	yaml := `
name: ci
on: [push]
jobs:
  build:
    env:
      - GOFLAGS=-mod=mod
    steps:
      - name: test
        run: go test ./...
      - run: echo done
`
	wf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if wf.Name != "ci" {
		t.Fatalf("name = %q", wf.Name)
	}
	script := wf.Script()
	for _, want := range []string{"set -e", "go test ./...", "echo done", "export GOFLAGS=-mod=mod"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte("name: x\n")); err == nil {
		t.Fatal("expected error for workflow without jobs")
	}
}
