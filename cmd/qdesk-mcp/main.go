// qdesk-mcp is a Model Context Protocol (MCP) stdio server that exposes
// qdesk's verification capabilities to AI coding tools (Claude Code,
// Cursor, etc.).
//
// Wire protocol: JSON-RPC 2.0 over stdin/stdout. One JSON object per line.
// Spec: https://modelcontextprotocol.io
//
// Tools exposed:
//
//	qdesk_health        — check that control plane is reachable
//	qdesk_screenshot    — open a URL in a sandbox, return PNG (Claude sees it)
//	qdesk_quick_test    — inline goal + url + expects, run + return summary
//	qdesk_run_test      — run an existing .qdesk.yaml file by path
//
// Install in Claude Code:
//
//	claude mcp add --transport stdio qdesk -- /usr/local/bin/qdesk-mcp \
//	    --control http://127.0.0.1:8090 --api-key $QDESK_DEV_KEY \
//	    --gemini-key $GEMINI_API_KEY
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jeffwang/qdesk/internal/llm"
	"github.com/jeffwang/qdesk/internal/runner"
	"github.com/jeffwang/qdesk/pkg/client"
	"github.com/jeffwang/qdesk/pkg/protocol"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "qdesk-mcp"
	serverVersion   = "0.1.0"
)

// ---------- JSON-RPC envelopes ----------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ---------- MCP types ----------

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type contentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type toolResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ---------- server state ----------

type server struct {
	controlURL string
	apiKey     string
	geminiKey  string
	model      string
}

func main() {
	control := flag.String("control", envOr("QDESK_CONTROL_URL", "http://127.0.0.1:8090"), "qdesk-control URL")
	apiKey := flag.String("api-key", envOr("QDESK_API_KEY", os.Getenv("QDESK_DEV_KEY")), "qdesk-control bearer token")
	geminiKey := flag.String("gemini-key", os.Getenv("GEMINI_API_KEY"), "Gemini API key")
	model := flag.String("llm", "gemini-2.5-flash", "default LLM model")
	flag.Parse()

	s := &server{
		controlURL: *control,
		apiKey:     *apiKey,
		geminiKey:  *geminiKey,
		model:      *model,
	}

	// MCP servers MUST NOT write to stdout for anything other than RPC frames.
	// Use stderr for diagnostics.
	logf("qdesk-mcp starting; control=%s llm=%s", s.controlURL, s.model)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := in.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, os.ErrClosed) || err.Error() == "EOF" {
				return
			}
			logf("read: %v", err)
			return
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			logf("invalid JSON-RPC: %v", err)
			continue
		}
		// Notifications (no id) — handle silently, no response.
		if len(req.ID) == 0 {
			s.handleNotification(&req)
			continue
		}
		resp := s.handle(ctx, &req)
		if err := writeFrame(out, resp); err != nil {
			logf("write: %v", err)
			return
		}
	}
}

func (s *server) handle(ctx context.Context, req *rpcRequest) *rpcResponse {
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			resp.Result = &toolResult{
				Content: []contentItem{{Type: "text", Text: "error: " + err.Error()}},
				IsError: true,
			}
			return resp
		}
		resp.Result = out
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func (s *server) handleNotification(req *rpcRequest) {
	switch req.Method {
	case "notifications/initialized":
		// expected after initialize; nothing to do
	case "notifications/cancelled":
		// best-effort; we don't track per-request cancellation yet
	}
}

// ---------- tool definitions ----------

func (s *server) tools() []toolDef {
	return []toolDef{
		{
			Name:        "qdesk_health",
			Description: "Check that the qdesk control plane is reachable. Call this first if any other tool returns a connection error.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "qdesk_screenshot",
			Description: "Open a URL in a fresh Linux sandbox with Chromium and return the rendered screenshot as an image. " +
				"Use this when you want to see what a page looks like — for example after building a UI change " +
				"or to inspect a third-party site. The session is torn down automatically.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to load. Inside the sandbox use http://host.docker.internal:PORT to reach a service running on the host.",
					},
					"wait_seconds": map[string]any{
						"type":        "integer",
						"description": "How long to wait after page load before taking the screenshot. Default 4. Bump for slow apps.",
						"minimum":     0,
						"maximum":     30,
					},
				},
				"required": []string{"url"},
			},
		},
		{
			Name: "qdesk_quick_test",
			Description: "Run an inline AI-driven UI test without writing a YAML file. Provide a goal (English description of what to do), " +
				"a url, and 1-4 expectations (English assertions verified after the goal). Returns pass/fail, AI diagnosis on failure, " +
				"and the final screenshot. Cost ~$0.005, latency ~30-60s. Use after making a UI change to verify it works visually.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Short label for the test (used in the report).",
					},
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to load. Use http://host.docker.internal:PORT to reach the host's services.",
					},
					"goal": map[string]any{
						"type":        "string",
						"description": "Natural-language description of what the agent should do, e.g. 'click the red Save button and confirm a Saved toast appears'.",
					},
					"expect": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "1-4 specific, observable assertions to verify after the goal completes.",
					},
					"llm": map[string]any{
						"type":        "string",
						"description": "Optional model override. Default gemini-2.5-flash. Use gemini-2.5-pro if Flash mis-clicks.",
					},
					"max_steps": map[string]any{
						"type":        "integer",
						"description": "Cap on agent turns. Default 10.",
					},
				},
				"required": []string{"url", "goal", "expect"},
			},
		},
		{
			Name:        "qdesk_run_test",
			Description: "Run an existing .qdesk.yaml file by absolute path. Returns the same shape as qdesk_quick_test.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"yaml_path": map[string]any{
						"type":        "string",
						"description": "Absolute path to a .qdesk.yaml file.",
					},
				},
				"required": []string{"yaml_path"},
			},
		},
	}
}

// ---------- tool dispatch ----------

func (s *server) callTool(ctx context.Context, name string, raw json.RawMessage) (*toolResult, error) {
	switch name {
	case "qdesk_health":
		return s.toolHealth(ctx)
	case "qdesk_screenshot":
		return s.toolScreenshot(ctx, raw)
	case "qdesk_quick_test":
		return s.toolQuickTest(ctx, raw)
	case "qdesk_run_test":
		return s.toolRunTest(ctx, raw)
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func (s *server) toolHealth(ctx context.Context) (*toolResult, error) {
	c := client.New(s.controlURL, s.apiKey)
	if err := c.Health(ctx); err != nil {
		return nil, err
	}
	return textResult(fmt.Sprintf("control plane OK (%s)", s.controlURL)), nil
}

func (s *server) toolScreenshot(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var p struct {
		URL         string `json:"url"`
		WaitSeconds int    `json:"wait_seconds"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if p.WaitSeconds == 0 {
		p.WaitSeconds = 4
	}
	c := client.New(s.controlURL, s.apiKey)
	sess, err := c.CreateSession(ctx, &client.CreateSessionRequest{
		Template:   "ubuntu-chrome",
		TTLSeconds: 60,
		OpenURL:    p.URL,
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.DeleteSession(dctx, sess.ID)
	}()
	select {
	case <-time.After(time.Duration(p.WaitSeconds) * time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	png, err := c.Screenshot(ctx, sess.ID)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return &toolResult{
		Content: []contentItem{
			{Type: "text", Text: fmt.Sprintf("Screenshot of %s (%d bytes PNG):", p.URL, len(png))},
			{Type: "image", Data: base64.StdEncoding.EncodeToString(png), MIMEType: "image/png"},
		},
	}, nil
}

func (s *server) toolQuickTest(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var p struct {
		Name     string   `json:"name"`
		URL      string   `json:"url"`
		Goal     string   `json:"goal"`
		Expect   []string `json:"expect"`
		LLM      string   `json:"llm"`
		MaxSteps int      `json:"max_steps"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.URL == "" || p.Goal == "" || len(p.Expect) == 0 {
		return nil, fmt.Errorf("url, goal, and expect are all required")
	}
	if p.Name == "" {
		p.Name = "quick-test"
	}
	if p.MaxSteps == 0 {
		p.MaxSteps = 10
	}
	model := p.LLM
	if model == "" {
		model = s.model
	}

	spec := &runner.TestSpec{
		Name:       p.Name,
		Template:   "ubuntu-chrome",
		URL:        p.URL,
		Goal:       p.Goal,
		Expect:     p.Expect,
		MaxSteps:   p.MaxSteps,
		TTLSeconds: 300,
		LLM:        model,
		SourcePath: "(inline)",
	}
	return s.runSpec(ctx, spec, model)
}

func (s *server) toolRunTest(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var p struct {
		YAMLPath string `json:"yaml_path"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.YAMLPath == "" {
		return nil, fmt.Errorf("yaml_path is required")
	}
	if !filepath.IsAbs(p.YAMLPath) {
		return nil, fmt.Errorf("yaml_path must be absolute")
	}
	spec, err := runner.ParseFile(p.YAMLPath)
	if err != nil {
		return nil, err
	}
	model := spec.LLM
	if model == "" {
		model = s.model
	}
	return s.runSpec(ctx, spec, model)
}

// runSpec is shared by quick_test and run_test.
func (s *server) runSpec(ctx context.Context, spec *runner.TestSpec, model string) (*toolResult, error) {
	if s.geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not configured for qdesk-mcp")
	}
	if !strings.HasPrefix(model, "gemini") {
		return nil, fmt.Errorf("only gemini models supported in v0.1; got %q", model)
	}
	agent := &llm.Gemini{APIKey: s.geminiKey, Model: model}

	outDir := filepath.Join(os.TempDir(), "qdesk-mcp-runs",
		fmt.Sprintf("%s-%d", safe(spec.Name), time.Now().Unix()))

	trace, _ := runner.Run(ctx, spec, runner.Options{
		ControlURL: s.controlURL,
		APIKey:     s.apiKey,
		Agent:      agent,
		OutDir:     outDir,
	})
	if trace == nil {
		return nil, fmt.Errorf("runner returned nil trace")
	}

	// Compose the response: text summary + final screenshot if available.
	summary := formatTrace(trace, outDir)
	items := []contentItem{{Type: "text", Text: summary}}
	if shot := lastScreenshot(trace, outDir); shot != "" {
		if data, err := os.ReadFile(shot); err == nil {
			items = append(items, contentItem{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(data),
				MIMEType: "image/png",
			})
		}
	}
	return &toolResult{
		Content: items,
		IsError: trace.Status != runner.StatusPass,
	}, nil
}

// ---------- helpers ----------

func formatTrace(t *runner.Trace, outDir string) string {
	var b strings.Builder
	switch t.Status {
	case runner.StatusPass:
		fmt.Fprintf(&b, "✅ PASS — %s\n", t.TestName)
	case runner.StatusFail:
		fmt.Fprintf(&b, "❌ FAIL — %s\n", t.TestName)
	default:
		fmt.Fprintf(&b, "⚠️ %s — %s\n", strings.ToUpper(string(t.Status)), t.TestName)
	}
	dur := t.FinishedAt.Sub(t.StartedAt).Round(time.Millisecond)
	fmt.Fprintf(&b, "Model: %s  ·  Duration: %s  ·  %d step(s)\n\n", t.LLM, dur, len(t.Steps))

	for _, s := range t.Steps {
		mark := "✓"
		if !s.Done {
			mark = "✗"
		}
		fmt.Fprintf(&b, "%s Step %d: %s\n", mark, s.Index, oneLine(s.Goal))
		if s.Failure != "" {
			fmt.Fprintf(&b, "   failure: %s\n", s.Failure)
		}
	}
	if len(t.Verifies) > 0 {
		b.WriteString("\nExpectations:\n")
		for _, v := range t.Verifies {
			mark := "✓"
			if !v.Passed {
				mark = "✗"
			}
			fmt.Fprintf(&b, "  %s %s\n     %s\n", mark, v.Expectation, v.Evidence)
		}
	}
	if t.Diagnosis != "" {
		fmt.Fprintf(&b, "\nDiagnosis:\n%s\n", t.Diagnosis)
	}
	report := filepath.Join(outDir, "report.html")
	if _, err := os.Stat(report); err == nil {
		fmt.Fprintf(&b, "\nReport: file://%s\n", report)
	}
	return b.String()
}

func lastScreenshot(t *runner.Trace, outDir string) string {
	// Prefer last verify screenshot (post-action state); fall back to last step screenshot.
	if len(t.Verifies) > 0 {
		last := t.Verifies[len(t.Verifies)-1]
		if last.Screenshot != "" {
			return filepath.Join(outDir, last.Screenshot)
		}
	}
	for i := len(t.Steps) - 1; i >= 0; i-- {
		ss := t.Steps[i].Screenshots
		if len(ss) > 0 {
			return filepath.Join(outDir, ss[len(ss)-1])
		}
	}
	return ""
}

func textResult(s string) *toolResult {
	return &toolResult{Content: []contentItem{{Type: "text", Text: s}}}
}

func writeFrame(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

func envOr(key, dflt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return dflt
}

func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 80 {
		return s[:77] + "…"
	}
	return s
}

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "test"
	}
	return out
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "qdesk-mcp: "+format+"\n", args...)
}

// ensure the protocol package is linked (used in tests indirectly).
var _ = protocol.ActionClick
