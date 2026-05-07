package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// fakeMacServer records every JSON-RPC tools/call body and returns
// canned ToolResult payloads keyed by the tool name. Stand-in for a
// real qdesk-mac --listen.
type fakeMacServer struct {
	t        *testing.T
	gotTools []string
	gotArgs  []map[string]any
	gotAuth  string
	respond  map[string]map[string]any // tool name -> ToolResult
}

func newFakeMacServer(t *testing.T) (*fakeMacServer, *httptest.Server) {
	t.Helper()
	f := &fakeMacServer{
		t:       t,
		respond: map[string]map[string]any{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		f.gotTools = append(f.gotTools, p.Name)
		f.gotArgs = append(f.gotArgs, p.Arguments)
		result, ok := f.respond[p.Name]
		if !ok {
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func macSpec(endpoint string) *TestSpec {
	return &TestSpec{
		Target: TargetMacHost,
		Mac: &MacHostConfig{
			Endpoint:  endpoint,
			APIKeyEnv: "QDESK_TEST_KEY_NOT_SET", // overridden in test setup
			BundleID:  "com.tencent.xinWeChat",
		},
		Goal:   "test",
		Expect: []string{"x"},
	}
}

func TestMacDriverSetupCallsActivateAndPropagatesAuth(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "secret-token")
	fake, srv := newFakeMacServer(t)

	spec := macSpec(srv.URL)
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"

	d, err := newMacDriver(spec, Options{})
	if err != nil {
		t.Fatalf("newMacDriver: %v", err)
	}
	sess, err := d.Setup(context.Background(), spec)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Close(context.Background())

	if len(fake.gotTools) != 1 || fake.gotTools[0] != "mac.activate" {
		t.Errorf("Setup should call mac.activate, got %v", fake.gotTools)
	}
	if fake.gotArgs[0]["bundle_id"] != "com.tencent.xinWeChat" {
		t.Errorf("activate called with wrong bundle: %v", fake.gotArgs[0])
	}
	if fake.gotAuth != "Bearer secret-token" {
		t.Errorf("auth header wrong: %q", fake.gotAuth)
	}
}

func TestMacDriverScreenshotDecodesBase64(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "x")
	fake, srv := newFakeMacServer(t)

	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47, 0x01, 0x02} // not a real PNG
	fake.respond["mac.screenshot"] = map[string]any{
		"content": []map[string]any{
			{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(wantPNG)},
			{"type": "text", "text": "frontApp.bundleId=... size=1024x768"},
		},
	}

	spec := macSpec(srv.URL)
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"
	d, _ := newMacDriver(spec, Options{})
	sess, _ := d.Setup(context.Background(), spec)

	got, err := sess.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if string(got) != string(wantPNG) {
		t.Errorf("PNG bytes round-tripped wrong: %x vs %x", got, wantPNG)
	}
	// Screenshot also re-activates: setup activate + screenshot's
	// activate + screenshot itself = 3 calls.
	if len(fake.gotTools) != 3 ||
		fake.gotTools[0] != "mac.activate" ||
		fake.gotTools[1] != "mac.activate" ||
		fake.gotTools[2] != "mac.screenshot" {
		t.Errorf("expected [activate, activate, screenshot]; got %v", fake.gotTools)
	}
}

func TestMacDriverActionTranslation(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "x")
	fake, srv := newFakeMacServer(t)
	spec := macSpec(srv.URL)
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"
	d, _ := newMacDriver(spec, Options{})
	sess, _ := d.Setup(context.Background(), spec)

	cases := []struct {
		name     string
		action   *protocol.Action
		wantTool string
		assert   func(t *testing.T, args map[string]any)
	}{
		{"click", &protocol.Action{Type: protocol.ActionClick, X: 10, Y: 20, Button: protocol.MouseRight}, "mac.click",
			func(t *testing.T, a map[string]any) {
				if a["x"] != float64(10) || a["y"] != float64(20) {
					t.Errorf("click coords wrong: %v", a)
				}
				if a["button"] != "right" {
					t.Errorf("button=%v", a["button"])
				}
				if a["target_bundle_id"] != "com.tencent.xinWeChat" {
					t.Errorf("guard not propagated: %v", a)
				}
			}},
		{"type", &protocol.Action{Type: protocol.ActionType_, Text: "hi"}, "mac.type",
			func(t *testing.T, a map[string]any) {
				if a["text"] != "hi" {
					t.Errorf("text=%v", a["text"])
				}
			}},
		{"key", &protocol.Action{Type: protocol.ActionKey, Keys: []string{"cmd", "s"}}, "mac.key",
			func(t *testing.T, a map[string]any) {
				if a["combo"] != "cmd+s" {
					t.Errorf("combo=%v", a["combo"])
				}
			}},
		{"key-meta-normalized", &protocol.Action{Type: protocol.ActionKey, Keys: []string{"meta", "f"}}, "mac.key",
			func(t *testing.T, a map[string]any) {
				if a["combo"] != "cmd+f" {
					t.Errorf("meta should be normalized to cmd; combo=%v", a["combo"])
				}
			}},
		{"scroll", &protocol.Action{Type: protocol.ActionScroll, X: 5, Y: 6, DY: 3}, "mac.scroll",
			func(t *testing.T, a map[string]any) {
				if a["dy"] != float64(3) {
					t.Errorf("dy=%v", a["dy"])
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake.gotTools = nil
			fake.gotArgs = nil
			if err := sess.Action(context.Background(), tc.action); err != nil {
				t.Fatalf("Action: %v", err)
			}
			// Each Action re-activates first, so we expect 2 calls:
			// [mac.activate, <wantTool>].
			if len(fake.gotTools) != 2 ||
				fake.gotTools[0] != "mac.activate" ||
				fake.gotTools[1] != tc.wantTool {
				t.Fatalf("expected [mac.activate, %s]; got %v", tc.wantTool, fake.gotTools)
			}
			tc.assert(t, fake.gotArgs[1])
		})
	}
}

func TestMacDriverDragRejected(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "x")
	_, srv := newFakeMacServer(t)
	spec := macSpec(srv.URL)
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"
	d, _ := newMacDriver(spec, Options{})
	sess, _ := d.Setup(context.Background(), spec)

	err := sess.Action(context.Background(), &protocol.Action{
		Type: protocol.ActionDrag,
		From: &protocol.Point{X: 0, Y: 0}, To: &protocol.Point{X: 10, Y: 10},
	})
	if err == nil || !strings.Contains(err.Error(), "drag") {
		t.Errorf("expected drag-not-supported error, got %v", err)
	}
}

func TestMacDriverWaitDoesNotCallTool(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "x")
	fake, srv := newFakeMacServer(t)
	spec := macSpec(srv.URL)
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"
	d, _ := newMacDriver(spec, Options{})
	sess, _ := d.Setup(context.Background(), spec)

	fake.gotTools = nil
	if err := sess.Action(context.Background(), &protocol.Action{Type: protocol.ActionWait, MS: 1}); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(fake.gotTools) != 0 {
		t.Errorf("Wait must not hit the network, got %v", fake.gotTools)
	}
}

func TestMacDriverEnvKeyMissing(t *testing.T) {
	os.Unsetenv("QDESK_TEST_KEY_NOT_SET")
	spec := macSpec("http://x")
	_, err := newMacDriver(spec, Options{})
	if err == nil || !strings.Contains(err.Error(), "QDESK_TEST_KEY_NOT_SET") {
		t.Errorf("expected env-missing error; got %v", err)
	}
}

func TestMacDriverEndpointOverride(t *testing.T) {
	t.Setenv("QDESK_TEST_KEY", "x")
	_, srv := newFakeMacServer(t)
	spec := macSpec("http://wrong-endpoint")
	spec.Mac.APIKeyEnv = "QDESK_TEST_KEY"

	d, err := newMacDriver(spec, Options{MacEndpoint: srv.URL})
	if err != nil {
		t.Fatalf("newMacDriver: %v", err)
	}
	if _, err := d.Setup(context.Background(), spec); err != nil {
		t.Fatalf("Setup should hit override URL, not yaml endpoint: %v", err)
	}
}
