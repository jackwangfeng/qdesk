package runner

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/llm"
	"github.com/jeffwang/qdesk/pkg/client"
	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Options configures a Run.
type Options struct {
	// ControlURL is the qdesk-control base URL (e.g. http://localhost:8080).
	ControlURL string

	// APIKey is the bearer token for the control plane.
	APIKey string

	// Agent is the VisionAgent used for Act / Verify / Diagnose.
	Agent llm.VisionAgent

	// OutDir is where the trace + screenshots + report.html are written.
	OutDir string

	// MaxIterPerStep caps the per-step turn count (the test's own MaxSteps
	// caps the total).
	MaxIterPerStep int
}

// Run executes a test spec end-to-end and returns the final trace.
func Run(ctx context.Context, spec *TestSpec, opts Options) (*Trace, error) {
	if opts.MaxIterPerStep == 0 {
		opts.MaxIterPerStep = 8
	}
	c := client.New(opts.ControlURL, opts.APIKey)

	trace := &Trace{
		TestName:  spec.Name,
		TestPath:  spec.SourcePath,
		StartedAt: time.Now().UTC(),
		Status:    StatusRunning,
		LLM:       opts.Agent.Name(),
	}
	defer func() {
		trace.FinishedAt = time.Now().UTC()
		_ = SaveTrace(opts.OutDir, trace)
	}()

	// 1. Spin up sandbox session.
	sess, err := c.CreateSession(ctx, &client.CreateSessionRequest{
		Template:   spec.Template,
		TTLSeconds: spec.TTLSeconds,
		OpenURL:    spec.URL,
	})
	if err != nil {
		trace.Status = StatusError
		trace.Diagnosis = "create session: " + err.Error()
		return trace, fmt.Errorf("create session: %w", err)
	}
	defer func() {
		ctxStop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.DeleteSession(ctxStop, sess.ID)
	}()

	// Give Chromium a moment to render the initial page after OpenURL.
	if spec.URL != "" {
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return trace, ctx.Err()
		}
	}

	// 2. For each step, run the agent loop.
	totalTurns := 0
	steps := spec.EffectiveSteps()
	for i, goal := range steps {
		ts := TraceStep{
			Index:     i,
			Goal:      goal,
			StartedAt: time.Now().UTC(),
		}
		slog.Info("runner: step start", "i", i, "goal", goal)

		history := []llm.Turn{}
		stepDone := false
		for iter := 0; iter < opts.MaxIterPerStep && totalTurns < spec.MaxSteps; iter++ {
			png, err := c.Screenshot(ctx, sess.ID)
			if err != nil {
				ts.Failure = "screenshot: " + err.Error()
				break
			}
			fname := fmt.Sprintf("step-%02d-turn-%02d.png", i, iter)
			_, _ = SaveScreenshot(opts.OutDir, fname, png)
			ts.Screenshots = append(ts.Screenshots, fname)

			dec, err := opts.Agent.Act(ctx, llm.Step{
				Goal:        goal,
				History:     history,
				Screenshot:  png,
				PageContext: spec.URL,
			})
			if err != nil {
				ts.Failure = "agent.Act: " + err.Error()
				break
			}
			turn := llm.Turn{
				StepIndex:   i,
				Reasoning:   dec.Reasoning,
				Action:      dec.Action,
				Done:        dec.Done,
				ScreenshotB: len(png),
			}
			history = append(history, turn)
			ts.Turns = append(ts.Turns, turn)
			totalTurns++

			slog.Info("runner: turn",
				"step", i, "iter", iter,
				"done", dec.Done,
				"reason", strings.TrimSpace(dec.Reasoning))

			if dec.Done {
				stepDone = true
				break
			}
			if dec.Action == nil {
				ts.Failure = "agent returned no action and not done"
				break
			}
			if _, err := c.Action(ctx, sess.ID, dec.Action); err != nil {
				ts.Failure = fmt.Sprintf("action %s: %v", dec.Action.Type, err)
				break
			}
			// Allow the page to settle before next observation.
			// Click/key/drag may trigger navigation or canvas re-render, which
			// is why we give 1.2s; scroll is much faster.
			settle := 1200 * time.Millisecond
			switch dec.Action.Type {
			case protocol.ActionWait:
				settle = 100 * time.Millisecond // already waited inside sandbox
			case protocol.ActionScroll:
				settle = 400 * time.Millisecond
			}
			select {
			case <-time.After(settle):
			case <-ctx.Done():
				return trace, ctx.Err()
			}
		}
		if !stepDone && ts.Failure == "" {
			ts.Failure = fmt.Sprintf("step did not complete within %d iterations", opts.MaxIterPerStep)
		}
		ts.Done = stepDone
		ts.FinishedAt = time.Now().UTC()
		trace.Steps = append(trace.Steps, ts)

		if ts.Failure != "" {
			trace.Status = StatusFail
			break
		}
	}

	// 3. If steps all succeeded, evaluate expectations.
	if trace.Status == StatusRunning {
		for _, exp := range spec.Expect {
			png, err := c.Screenshot(ctx, sess.ID)
			if err != nil {
				trace.Status = StatusError
				trace.Diagnosis = "verify screenshot: " + err.Error()
				return trace, err
			}
			fname := fmt.Sprintf("verify-%s.png", safeFilename(exp))
			_, _ = SaveScreenshot(opts.OutDir, fname, png)

			res, err := opts.Agent.Verify(ctx, exp, png)
			if err != nil {
				trace.Status = StatusError
				trace.Diagnosis = "verify: " + err.Error()
				return trace, err
			}
			tv := TraceVerify{
				Expectation: exp,
				Passed:      res.Passed,
				Evidence:    res.Evidence,
				Screenshot:  fname,
				EvaluatedAt: time.Now().UTC(),
			}
			trace.Verifies = append(trace.Verifies, tv)
			if !res.Passed {
				trace.Status = StatusFail
			}
		}
	}

	// 4. On any failure, ask the LLM to diagnose.
	if trace.Status == StatusFail {
		png, err := c.Screenshot(ctx, sess.ID)
		if err == nil {
			summary := summariseFailure(trace)
			lastTurns := flattenTurns(trace.Steps)
			diag, derr := opts.Agent.Diagnose(ctx, summary, lastTurns, png)
			if derr == nil {
				trace.Diagnosis = diag
			}
		}
	} else if trace.Status == StatusRunning {
		trace.Status = StatusPass
	}

	// 5. Render report HTML next to the trace.
	if err := WriteReport(filepath.Join(opts.OutDir, "report.html"), trace); err != nil {
		slog.Warn("write report", "err", err)
	}
	return trace, nil
}

// summariseFailure builds the text passed to Diagnose.
func summariseFailure(t *Trace) string {
	var b strings.Builder
	b.WriteString("Test: " + t.TestName + "\n")
	b.WriteString("Status: " + string(t.Status) + "\n")
	for _, s := range t.Steps {
		if s.Failure != "" {
			fmt.Fprintf(&b, "Step %d (%q) failed: %s\n", s.Index, s.Goal, s.Failure)
		}
	}
	for _, v := range t.Verifies {
		if !v.Passed {
			fmt.Fprintf(&b, "Expectation %q failed: %s\n", v.Expectation, v.Evidence)
		}
	}
	return b.String()
}

func flattenTurns(steps []TraceStep) []llm.Turn {
	var out []llm.Turn
	for _, s := range steps {
		out = append(out, s.Turns...)
	}
	// Cap at last 8 turns to keep the prompt small.
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return out
}

// safeFilename trims a string into a filesystem-friendly slug.
func safeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
		if b.Len() >= 32 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "expect"
	}
	return out
}
