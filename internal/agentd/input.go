package agentd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// InputDriver dispatches an Action to the underlying input mechanism.
type InputDriver interface {
	Execute(ctx context.Context, a *protocol.Action) error
}

// XdotoolInput shells out to xdotool against the configured DISPLAY.
type XdotoolInput struct {
	Display string
}

func (x *XdotoolInput) cmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "xdotool", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+x.Display)
	return cmd
}

func (x *XdotoolInput) run(ctx context.Context, args ...string) error {
	out, err := x.cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func buttonNum(b protocol.MouseButton) string {
	switch b {
	case protocol.MouseMiddle:
		return "2"
	case protocol.MouseRight:
		return "3"
	default: // MouseLeft or empty default
		return "1"
	}
}

// Execute runs the action against the X display via xdotool.
//
// All variants of protocol.Action are handled; unrecognised types return
// an InvalidAction error.
func (x *XdotoolInput) Execute(ctx context.Context, a *protocol.Action) error {
	switch a.Type {
	case protocol.ActionClick:
		return x.run(ctx,
			"mousemove", strconv.Itoa(a.X), strconv.Itoa(a.Y),
			"click", buttonNum(a.Button),
		)
	case protocol.ActionType_:
		return x.run(ctx, "type", "--delay", "10", a.Text)
	case protocol.ActionKey:
		// xdotool key combo uses '+' separator: "ctrl+s"
		return x.run(ctx, "key", strings.Join(a.Keys, "+"))
	case protocol.ActionScroll:
		// Vertical scroll only for now: button 4 = up, 5 = down.
		btn := "4"
		if a.DY < 0 {
			btn = "5"
		}
		ticks := a.DY
		if ticks < 0 {
			ticks = -ticks
		}
		if ticks == 0 {
			ticks = 1
		}
		if err := x.run(ctx, "mousemove", strconv.Itoa(a.X), strconv.Itoa(a.Y)); err != nil {
			return err
		}
		for i := 0; i < ticks; i++ {
			if err := x.run(ctx, "click", btn); err != nil {
				return err
			}
		}
		return nil
	case protocol.ActionDrag:
		if a.From == nil || a.To == nil {
			return InvalidAction("drag requires both 'from' and 'to'")
		}
		return x.run(ctx,
			"mousemove", strconv.Itoa(a.From.X), strconv.Itoa(a.From.Y),
			"mousedown", "1",
			"mousemove", strconv.Itoa(a.To.X), strconv.Itoa(a.To.Y),
			"mouseup", "1",
		)
	case protocol.ActionWait:
		// Local sleep, no xdotool subprocess.
		select {
		case <-time.After(time.Duration(a.MS) * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return InvalidAction(fmt.Sprintf("unknown action type %q", a.Type))
	}
}

// InvalidAction is returned when the action payload is malformed.
type InvalidAction string

func (e InvalidAction) Error() string { return string(e) }

// MockInput is a test double that records every executed action.
type MockInput struct {
	mu       sync.Mutex
	Recorded []*protocol.Action
}

func (m *MockInput) Execute(_ context.Context, a *protocol.Action) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Copy to detach from caller-mutable input.
	cp := *a
	m.Recorded = append(m.Recorded, &cp)
	return nil
}

// Snapshot returns a defensive copy of recorded actions.
func (m *MockInput) Snapshot() []*protocol.Action {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*protocol.Action, len(m.Recorded))
	copy(out, m.Recorded)
	return out
}
