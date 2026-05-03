package agentd

import (
	"bytes"
	"context"
	"testing"
)

func TestMockScreenReturnsBytesAndCounts(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47}
	m := NewMockScreen(png)
	out, err := m.Capture(context.Background())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !bytes.Equal(out, png) {
		t.Errorf("bytes mismatch: got=%v want=%v", out, png)
	}
	if m.CallCount() != 1 {
		t.Errorf("call count: got=%d want=1", m.CallCount())
	}
}
