// Package runner orchestrates qdesk-driven AI tests: it reads a .qdesk file,
// drives the sandbox via the control plane, and produces an HTML report.
package runner

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TestSpec is the parsed representation of a .qdesk YAML file.
type TestSpec struct {
	// Name is a human-readable label for the test (used in the report).
	Name string `yaml:"name"`

	// Template selects the sandbox image. Empty means "ubuntu-chrome".
	Template string `yaml:"template,omitempty"`

	// URL is opened in Chromium when the sandbox starts.
	URL string `yaml:"url,omitempty"`

	// Goal is a high-level natural-language description of what the test
	// should accomplish overall (used as the single Step goal when steps[]
	// is empty — typical for v0.1).
	Goal string `yaml:"goal,omitempty"`

	// Steps is an optional ordered list of sub-goals. If non-empty, the
	// runner runs each one to completion before moving to the next.
	Steps []string `yaml:"steps,omitempty"`

	// Expect lists assertions evaluated after all steps complete.
	Expect []string `yaml:"expect,omitempty"`

	// MaxSteps caps total Act() turns across the whole test.
	MaxSteps int `yaml:"max_steps,omitempty"`

	// LLM picks which model adapter to use (overrides CLI flag).
	LLM string `yaml:"llm,omitempty"`

	// TTLSeconds bounds the underlying sandbox session lifetime.
	TTLSeconds int `yaml:"ttl_seconds,omitempty"`

	// Source path of the YAML file (filled by ParseFile).
	SourcePath string `yaml:"-"`
}

// ParseFile reads and parses a .qdesk YAML test spec.
func ParseFile(path string) (*TestSpec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var t TestSpec
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	t.SourcePath = path
	t.applyDefaults()
	return &t, nil
}

func (t *TestSpec) validate() error {
	if t.Goal == "" && len(t.Steps) == 0 {
		return fmt.Errorf("either 'goal' or non-empty 'steps' is required")
	}
	if len(t.Expect) == 0 {
		return fmt.Errorf("'expect' list must contain at least one item")
	}
	return nil
}

func (t *TestSpec) applyDefaults() {
	if t.Template == "" {
		t.Template = "ubuntu-chrome"
	}
	if t.MaxSteps == 0 {
		t.MaxSteps = 20
	}
	if t.TTLSeconds == 0 {
		t.TTLSeconds = 300 // 5 min
	}
}

// EffectiveSteps returns the step list, falling back to [Goal] if Steps is empty.
func (t *TestSpec) EffectiveSteps() []string {
	if len(t.Steps) > 0 {
		return t.Steps
	}
	return []string{t.Goal}
}
