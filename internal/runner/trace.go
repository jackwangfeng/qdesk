package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jeffwang/qdesk/internal/llm"
)

// Trace is the persistent record of a test run. It can be replayed later
// (Phase 0 only records; replay is Phase 1).
type Trace struct {
	TestName   string        `json:"test_name"`
	TestPath   string        `json:"test_path"`
	Target     string        `json:"target,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Status     RunStatus     `json:"status"`
	Steps      []TraceStep   `json:"steps"`
	Verifies   []TraceVerify `json:"verifies"`
	Diagnosis  string        `json:"diagnosis,omitempty"`
	LLM        string        `json:"llm"`
}

// TraceStep is the per-step record.
type TraceStep struct {
	Index       int       `json:"index"`
	Goal        string    `json:"goal"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Turns       []llm.Turn `json:"turns"`
	Done        bool      `json:"done"`
	Failure     string    `json:"failure,omitempty"`
	// Screenshots[i] corresponds to Turns[i] BEFORE the turn's action was
	// taken. They live next to the trace JSON as files; this list holds
	// just the relative filenames.
	Screenshots []string `json:"screenshots"`
}

// TraceVerify records one expectation evaluation.
type TraceVerify struct {
	Expectation string    `json:"expectation"`
	Passed      bool      `json:"passed"`
	Evidence    string    `json:"evidence"`
	Screenshot  string    `json:"screenshot"` // filename relative to trace dir
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// RunStatus is the test result.
type RunStatus string

const (
	StatusPass    RunStatus = "pass"
	StatusFail    RunStatus = "fail"
	StatusError   RunStatus = "error"
	StatusRunning RunStatus = "running"
)

// SaveTrace writes trace.json + screenshot files into dir.
//
// Each step's Screenshots[i] is expected to already be written by the runner;
// this function only persists the JSON metadata.
func SaveTrace(dir string, t *Trace) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "trace.json"), b, 0o644)
}

// SaveScreenshot writes a PNG to dir/name and returns the basename.
func SaveScreenshot(dir, name string, png []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, png, 0o644); err != nil {
		return "", fmt.Errorf("save screenshot %s: %w", full, err)
	}
	return name, nil
}
