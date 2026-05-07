// Package runner orchestrates qdesk-driven AI tests: it reads a .qdesk file,
// drives the sandbox via the control plane, and produces an HTML report.
package runner

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Valid Target values. Empty defaults to TargetLinuxChrome for
// backward compatibility with v0 yaml files.
const (
	TargetLinuxChrome = "linux-chrome"
	TargetMacHost     = "mac-host"
)

// MacHostConfig is the yaml block under `mac:` for target=mac-host.
type MacHostConfig struct {
	// Endpoint is the qdesk-mac --listen URL (e.g. http://127.0.0.1:8765).
	Endpoint string `yaml:"endpoint"`
	// APIKeyEnv is the name of the environment variable holding the bearer
	// token (e.g. "QDESK_MAC_API_KEY"). The value is read at runner setup.
	APIKeyEnv string `yaml:"api_key_env"`
	// BundleID is the macOS app the runner should drive. The driver
	// activates it on Setup and passes it as target_bundle_id on every
	// subsequent action so a stray focus change can't redirect input.
	BundleID string `yaml:"bundle_id"`
}

// TestSpec is the parsed representation of a .qdesk YAML file.
type TestSpec struct {
	// Name is a human-readable label for the test (used in the report).
	Name string `yaml:"name"`

	// Target picks the runtime backend. "" or "linux-chrome" preserves
	// the existing per-session Docker sandbox. "mac-host" drives the
	// user's local macOS via qdesk-mac.
	Target string `yaml:"target,omitempty"`

	// Mac is required when Target == "mac-host" and ignored otherwise.
	Mac *MacHostConfig `yaml:"mac,omitempty"`

	// Template selects the sandbox image (Linux Docker target only).
	// Empty means "ubuntu-chrome".
	Template string `yaml:"template,omitempty"`

	// URL is opened in Chromium when a Linux Docker sandbox starts.
	// Ignored for non-web targets.
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
	switch t.Target {
	case "", TargetLinuxChrome:
		if t.Mac != nil {
			return fmt.Errorf("'mac:' block only valid when target == %q", TargetMacHost)
		}
	case TargetMacHost:
		if t.Mac == nil {
			return fmt.Errorf("target %q requires a 'mac:' block", TargetMacHost)
		}
		if t.Mac.Endpoint == "" {
			return fmt.Errorf("mac.endpoint is required")
		}
		if t.Mac.APIKeyEnv == "" {
			return fmt.Errorf("mac.api_key_env is required")
		}
		if t.Mac.BundleID == "" {
			return fmt.Errorf("mac.bundle_id is required")
		}
		if t.URL != "" || (t.Template != "" && t.Template != "ubuntu-chrome") {
			return fmt.Errorf("'url:' / non-default 'template:' not valid when target == %q", TargetMacHost)
		}
	default:
		return fmt.Errorf("unknown target %q (valid: %q, %q)", t.Target, TargetLinuxChrome, TargetMacHost)
	}
	return nil
}

func (t *TestSpec) applyDefaults() {
	if t.Target == "" {
		t.Target = TargetLinuxChrome
	}
	if t.Target == TargetLinuxChrome && t.Template == "" {
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
