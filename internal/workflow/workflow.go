// SPDX-License-Identifier: AGPL-3.0
// Package workflow parses a .gitli workflow definition.
package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Step is a single command step.
type Step struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

// Job is a named group of steps.
type Job struct {
	Name  string   `yaml:"name"`
	Image string   `yaml:"image"`
	Shell string   `yaml:"shell"`
	Env   []string `yaml:"env"`
	Steps []Step   `yaml:"steps"`
}

// Workflow is a parsed workflow file.
type Workflow struct {
	Name  string         `yaml:"name"`
	On    []string       `yaml:"on"`
	Env   []string       `yaml:"env"`
	Shell string         `yaml:"shell"`
	Jobs  map[string]Job `yaml:"jobs"`
}

// EffectiveShell returns the shell to use: the first job-level shell, else the
// workflow shell, else the configured default. An empty result means "auto".
func (w *Workflow) EffectiveShell(defaultShell string) string {
	names := make([]string, 0, len(w.Jobs))
	for name := range w.Jobs {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		if s := strings.TrimSpace(w.Jobs[name].Shell); s != "" {
			return s
		}
	}
	if strings.TrimSpace(w.Shell) != "" {
		return strings.TrimSpace(w.Shell)
	}
	return strings.TrimSpace(defaultShell)
}

// Parse decodes a workflow YAML document.
func Parse(data []byte) (*Workflow, error) {
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("workflow: parse yaml: %w", err)
	}
	if len(wf.Jobs) == 0 {
		return nil, fmt.Errorf("workflow: no jobs defined")
	}
	return &wf, nil
}

// Script renders the workflow into a single shell script that runs each job's
// steps in sequence. Steps are only joined with newlines (no shell interpolation
// of untrusted data): the resulting script is executed by the sandbox shell.
func (w *Workflow) Script() string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("echo '[gitli-runner] starting workflow'\n")
	// Deterministic order: sort job names.
	names := make([]string, 0, len(w.Jobs))
	for name := range w.Jobs {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		job := w.Jobs[name]
		fmt.Fprintf(&b, "echo '[gitli-runner] job: %s'\n", name)
		for _, kv := range job.Env {
			// export only well-formed KEY=VALUE entries.
			if i := strings.IndexByte(kv, '='); i > 0 {
				fmt.Fprintf(&b, "export %s\n", kv)
			}
		}
		for _, step := range job.Steps {
			if step.Name != "" {
				fmt.Fprintf(&b, "echo '[gitli-runner] step: %s'\n", step.Name)
			}
			b.WriteString(step.Run)
			b.WriteString("\n")
		}
	}
	b.WriteString("echo '[gitli-runner] workflow finished'\n")
	return b.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
