package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// MacDriver drives the user's local macOS via qdesk-mac --listen.
// One MacDriver per Run; sets up by calling mac.activate on the
// configured bundle_id, then translates each protocol.Action into the
// matching mac.* MCP tool call (with target_bundle_id as guard).
type MacDriver struct {
	endpoint string
	apiKey   string
	bundleID string
	http     *http.Client
}

// newMacDriver constructs a MacDriver from spec + Options. Resolves the
// API key from the env var named in spec.Mac.APIKeyEnv at construction
// time; opts.MacEndpoint (if set) overrides spec.Mac.Endpoint so CI can
// inject a per-runner URL.
func newMacDriver(spec *TestSpec, opts Options) (Driver, error) {
	if spec.Mac == nil {
		return nil, fmt.Errorf("mac-host target requires a mac: block")
	}
	endpoint := spec.Mac.Endpoint
	if opts.MacEndpoint != "" {
		endpoint = opts.MacEndpoint
	}
	key := os.Getenv(spec.Mac.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("env var %s is empty", spec.Mac.APIKeyEnv)
	}
	return &MacDriver{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   key,
		bundleID: spec.Mac.BundleID,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (d *MacDriver) Setup(ctx context.Context, _ *TestSpec) (DriverSession, error) {
	s := &macSession{
		endpoint: d.endpoint,
		apiKey:   d.apiKey,
		bundleID: d.bundleID,
		http:     d.http,
		nextID:   1,
	}
	// Activate the target app once so subsequent guarded calls succeed.
	if _, err := s.callTool(ctx, "mac.activate", map[string]any{"bundle_id": d.bundleID}); err != nil {
		return nil, fmt.Errorf("mac.activate %s: %w", d.bundleID, err)
	}
	return s, nil
}

// macSession is the in-flight DriverSession for MacDriver.
type macSession struct {
	endpoint string
	apiKey   string
	bundleID string
	http     *http.Client
	nextID   int
}

func (s *macSession) Screenshot(ctx context.Context) ([]byte, error) {
	// Bring the target to foreground before snapping. The screenshot
	// is full-screen, so the agent will see WHATEVER is in front —
	// including iTerm2 if focus drifted. Re-activating each time
	// keeps the agent's view consistent with the bundle it's driving.
	if _, err := s.callTool(ctx, "mac.activate", map[string]any{"bundle_id": s.bundleID}); err != nil {
		return nil, fmt.Errorf("re-activate %s: %w", s.bundleID, err)
	}
	res, err := s.callTool(ctx, "mac.screenshot", map[string]any{})
	if err != nil {
		return nil, err
	}
	for _, c := range res.Content {
		if c.Type == "image" && c.Data != "" {
			png, err := base64.StdEncoding.DecodeString(c.Data)
			if err != nil {
				return nil, fmt.Errorf("decode screenshot base64: %w", err)
			}
			return png, nil
		}
	}
	return nil, fmt.Errorf("mac.screenshot returned no image content")
}

func (s *macSession) Action(ctx context.Context, a *protocol.Action) error {
	// Re-activate the target before every action that hits the GUI.
	// Without this, a 5-15s vision call lets iTerm2 (or whatever was
	// running qdesk-run) drift back to foreground; the per-action
	// target_bundle_id guard then correctly refuses, and the test
	// fails for the wrong reason. Wait is local-only and skips this.
	if a.Type != protocol.ActionWait {
		if _, err := s.callTool(ctx, "mac.activate", map[string]any{"bundle_id": s.bundleID}); err != nil {
			return fmt.Errorf("re-activate %s: %w", s.bundleID, err)
		}
	}
	switch a.Type {
	case protocol.ActionClick:
		args := map[string]any{
			"x": a.X, "y": a.Y,
			"target_bundle_id": s.bundleID,
		}
		if a.Button != "" {
			args["button"] = string(a.Button)
		}
		_, err := s.callTool(ctx, "mac.click", args)
		return err
	case protocol.ActionType_:
		_, err := s.callTool(ctx, "mac.type", map[string]any{
			"text":             a.Text,
			"target_bundle_id": s.bundleID,
		})
		return err
	case protocol.ActionKey:
		// protocol.Action.Keys carries ["ctrl","s"]; mac.key wants "ctrl+s".
		// Normalize cross-platform aliases (LLMs trained on Linux/Win
		// often emit "meta" / "command" / "option" / "win"; on Mac all
		// four mean cmd or alt).
		_, err := s.callTool(ctx, "mac.key", map[string]any{
			"combo":            normalizeMacCombo(a.Keys),
			"target_bundle_id": s.bundleID,
		})
		return err
	case protocol.ActionScroll:
		_, err := s.callTool(ctx, "mac.scroll", map[string]any{
			"x": a.X, "y": a.Y, "dx": a.DX, "dy": a.DY,
			"target_bundle_id": s.bundleID,
		})
		return err
	case protocol.ActionWait:
		ms := a.MS
		if ms == 0 {
			ms = 500
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case protocol.ActionDrag:
		return fmt.Errorf("drag is not supported on mac-host target in v0")
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
}

func (s *macSession) Close(_ context.Context) error { return nil }

// normalizeMacCombo maps cross-platform modifier names to what
// qdesk-mac-helper understands. The vision LLM is trained on a mix of
// Linux/Windows keyboard literature and frequently emits "meta" or
// "win" for what should be "cmd" on macOS; "option" instead of "alt".
func normalizeMacCombo(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		switch strings.ToLower(k) {
		case "meta", "command", "win", "windows", "super":
			out[i] = "cmd"
		case "option":
			out[i] = "alt"
		default:
			out[i] = strings.ToLower(k)
		}
	}
	return strings.Join(out, "+")
}

// rpcResult is what mac.* tool calls return inside the JSON-RPC envelope.
type rpcResult struct {
	Content []rpcContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type rpcContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// callTool issues a tools/call JSON-RPC over HTTP and unwraps the
// ToolResult. An IsError=true result is returned as a Go error so the
// caller doesn't need to inspect each content item.
func (s *macSession) callTool(ctx context.Context, name string, args map[string]any) (*rpcResult, error) {
	id := s.nextID
	s.nextID++

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/mcp", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: HTTP %d: %s", name, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var env struct {
		Result *rpcResult `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", name, err)
	}
	if env.Error != nil {
		return nil, fmt.Errorf("%s: rpc error %d: %s", name, env.Error.Code, env.Error.Message)
	}
	if env.Result == nil {
		return nil, fmt.Errorf("%s: empty result", name)
	}
	if env.Result.IsError {
		msg := "tool error"
		if len(env.Result.Content) > 0 {
			msg = env.Result.Content[0].Text
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return env.Result, nil
}
