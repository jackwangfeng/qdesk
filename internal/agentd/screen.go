package agentd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ScreenSource captures the current display as PNG bytes.
type ScreenSource interface {
	Capture(ctx context.Context) ([]byte, error)
}

// ScrotScreen shells out to scrot to produce a PNG.
//
// NOTE: writes to a fixed path. Concurrent calls will race on the file.
// Acceptable for Phase 0 (single-tenant container). Fix before multi-session
// support.
type ScrotScreen struct {
	Display string // e.g. ":99"
}

const scrotOutputPath = "/tmp/qdesk_capture.png"

func (s *ScrotScreen) Capture(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "scrot",
		"--silent",
		"--overwrite",
		scrotOutputPath,
	)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.Display)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("scrot failed: %w: %s", err, out)
	}
	bytes, err := os.ReadFile(scrotOutputPath)
	if err != nil {
		return nil, fmt.Errorf("read scrot output: %w", err)
	}
	return bytes, nil
}

// MockScreen is a test double that returns fixed bytes and counts calls.
type MockScreen struct {
	mu    sync.Mutex
	bytes []byte
	calls atomic.Uint32
}

// NewMockScreen returns a MockScreen seeded with the given PNG bytes.
func NewMockScreen(b []byte) *MockScreen {
	return &MockScreen{bytes: b}
}

func (m *MockScreen) Capture(_ context.Context) ([]byte, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.bytes))
	copy(out, m.bytes)
	return out, nil
}

// CallCount returns how many times Capture has been called.
func (m *MockScreen) CallCount() uint32 {
	return m.calls.Load()
}
