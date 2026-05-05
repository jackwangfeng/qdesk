package macserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// helperBinaryPath returns the path to the built helper, skipping the test if
// the build hasn't been run yet.
func helperBinaryPath(t *testing.T) string {
	t.Helper()
	repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}
	p := filepath.Join(string(repoRoot[:len(repoRoot)-1]), "bin", "qdesk-mac-helper")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("helper binary not built (run `make mac-build`): %v", err)
	}
	return p
}

func TestSupervisorHealthRoundTrip(t *testing.T) {
	bin := helperBinaryPath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := Spawn(ctx, bin)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer s.Close()

	raw, err := s.Call(ctx, macproto.MethodHealth, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var h macproto.HealthResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// h.OK depends on host TCC state; just verify the shape decoded.
	t.Logf("health: %+v", h)
}
