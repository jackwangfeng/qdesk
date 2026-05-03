package agentd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

func newTestState() (*AppState, *MockInput, *MockScreen) {
	in := &MockInput{}
	sc := NewMockScreen([]byte{0x89, 0x50, 0x4E, 0x47})
	return &AppState{Screen: sc, Input: in}, in, sc
}

func TestHealthReturnsOK(t *testing.T) {
	state, _, _ := newTestState()
	rr := httptest.NewRecorder()
	NewRouter(state).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got=%d want=200", rr.Code)
	}
	var h protocol.HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status: got=%s want=ok", h.Status)
	}
	if h.Version == "" {
		t.Errorf("version is empty")
	}
}

func TestScreenshotReturnsPNGBytes(t *testing.T) {
	state, _, _ := newTestState()
	rr := httptest.NewRecorder()
	NewRouter(state).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/screenshot", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got=%d want=200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content-type: got=%s want=image/png", got)
	}
	b, _ := io.ReadAll(rr.Body)
	if len(b) < 4 || !bytes.Equal(b[:4], []byte{0x89, 0x50, 0x4E, 0x47}) {
		t.Errorf("PNG magic missing: got=% x", b[:min(4, len(b))])
	}
}

func TestClickActionIsDispatched(t *testing.T) {
	state, in, _ := newTestState()
	body, _ := json.Marshal(protocol.Action{
		Type:   protocol.ActionClick,
		X:      100,
		Y:      200,
		Button: protocol.MouseLeft,
	})
	req := httptest.NewRequest(http.MethodPost, "/actions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	NewRouter(state).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got=%d body=%s", rr.Code, rr.Body.String())
	}
	var res protocol.ActionResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK {
		t.Errorf("ok: got=false want=true")
	}
	if got := in.Snapshot(); len(got) != 1 {
		t.Errorf("recorded len: got=%d want=1", len(got))
	}
}

func TestMalformedActionReturns400(t *testing.T) {
	state, _, _ := newTestState()
	req := httptest.NewRequest(http.MethodPost, "/actions",
		strings.NewReader(`{"type": "click", "totally": "wrong field"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	NewRouter(state).ServeHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("expected 4xx, got=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestActionMissingTypeReturns400(t *testing.T) {
	state, _, _ := newTestState()
	req := httptest.NewRequest(http.MethodPost, "/actions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	NewRouter(state).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got=%d want=400", rr.Code)
	}
}
