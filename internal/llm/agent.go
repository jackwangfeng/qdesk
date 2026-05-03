// Package llm defines the abstract VisionAgent contract that runner uses,
// plus the concrete Gemini / Claude / GPT-4o backends.
//
// All adapters return decisions in the same wire shape (Decision below).
// Runner is model-agnostic.
package llm

import (
	"context"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Turn is a single observe-act exchange recorded in trace history.
type Turn struct {
	StepIndex   int               `json:"step_index"`
	Reasoning   string            `json:"reasoning"`
	Action      *protocol.Action  `json:"action,omitempty"`
	Done        bool              `json:"done"`
	ScreenshotB int               `json:"screenshot_bytes"` // size only, image stored elsewhere
}

// Step is the input the model sees when deciding what to do next.
type Step struct {
	// Goal is the natural-language description of what the model is trying
	// to accomplish at the current step (one of test.steps[i]).
	Goal string

	// PageContext is optional metadata about the page (URL, title) when
	// available — useful so the model doesn't waste a turn working it out.
	PageContext string

	// History is the running record of prior turns within the same step.
	History []Turn

	// Screenshot is the current display as PNG bytes.
	Screenshot []byte
}

// Decision is what the model returns each turn.
type Decision struct {
	// Reasoning is a short human-readable rationale for the action.
	Reasoning string `json:"reasoning"`
	// Done is true when the model thinks the current step's Goal is complete.
	Done bool `json:"done"`
	// Action is the next action to execute when Done is false. May be nil
	// when Done is true.
	Action *protocol.Action `json:"action,omitempty"`
}

// VerifyResult is the output of Verify.
type VerifyResult struct {
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

// VisionAgent is the interface every model adapter implements.
type VisionAgent interface {
	// Name identifies the backing model (e.g. "gemini-2.5-flash").
	Name() string

	// Act observes the current state and returns the next decision.
	Act(ctx context.Context, step Step) (*Decision, error)

	// Verify checks whether an expectation holds given a screenshot.
	// expectation is one item from test.expect.
	Verify(ctx context.Context, expectation string, screenshot []byte) (*VerifyResult, error)

	// Diagnose summarises why the test failed. Called only on failure.
	Diagnose(ctx context.Context, summary string, history []Turn, screenshot []byte) (string, error)
}
