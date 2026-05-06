package macserver

import (
	"context"
	"encoding/json"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "qdesk-mac"
	wechatBundleID  = "com.tencent.xinWeChat"
)

// RPCRequest mirrors the existing qdesk-mcp envelope so we keep one shape.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// MCPServer is the stdio MCP server for qdesk-mac.
type MCPServer struct {
	helper HelperClient
}

func NewMCPServer(h HelperClient) *MCPServer {
	return &MCPServer{helper: h}
}

func (s *MCPServer) Handle(ctx context.Context, req *RPCRequest) *RPCResponse {
	resp := &RPCResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": "0.1.0"},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			resp.Result = &ToolResult{
				Content: []ContentItem{{Type: "text", Text: "error: " + err.Error()}},
				IsError: true,
			}
			return resp
		}
		resp.Result = out
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &RPCError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func (s *MCPServer) tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "wechat.screenshot",
			Description: "Take a full-screen screenshot. Returns a PNG plus the bundle ID and name of the current foreground app — check this to see whether you need to call wechat.ensure_foreground first.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.ensure_foreground",
			Description: "Bring WeChat to the foreground. Returns an error if WeChat is not running (the user must launch it manually; this tool does not auto-launch apps).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.click",
			Description: "Click at LOGICAL global screen coordinates. Requires WeChat to be the foreground app — call wechat.ensure_foreground first if not.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":      map[string]any{"type": "number"},
					"y":      map[string]any{"type": "number"},
					"button": map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "default": "left"},
					"clicks": map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "default": 1},
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "wechat.type",
			Description: "Type Unicode text (including Chinese) at the current focus. Bypasses IME via CGEvent unicode mode.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []string{"text"},
			},
		},
		{
			Name:        "wechat.key",
			Description: "Send a key combo, e.g. \"return\", \"escape\", \"cmd+v\".",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"combo": map[string]any{"type": "string"}},
				"required":   []string{"combo"},
			},
		},
		{
			Name:        "wechat.scroll",
			Description: "Wheel-scroll at LOGICAL screen point (x, y). Positive dy scrolls up.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":  map[string]any{"type": "number"},
					"y":  map[string]any{"type": "number"},
					"dy": map[string]any{"type": "number"},
					"dx": map[string]any{"type": "number", "default": 0},
				},
				"required": []string{"x", "y", "dy"},
			},
		},
		{
			Name:        "wechat.open_chat",
			Description: "Open the conversation with the given chat name. Drives WeChat's own search bar (cmd+f) — does not depend on the Accessibility tree, which WeChat 4.x no longer exposes for the sidebar. Does not guarantee the right chat opens; verify with wechat.screenshot.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []string{"name"},
			},
		},

		// ----- Generic mac.* surface — drive any macOS app, not just WeChat -----

		{
			Name:        "mac.front_app",
			Description: "Return the bundle ID, name, and PID of the current foreground app. Use this to discover what's in front before screenshotting or sending input.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "mac.activate",
			Description: "Bring the given macOS app to the foreground. The app must already be running — this tool does not auto-launch. Common bundle IDs: com.apple.iphonesimulator (Simulator.app), com.apple.Safari, com.tencent.xinWeChat, com.apple.finder.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"bundle_id": map[string]any{"type": "string"}},
				"required":   []string{"bundle_id"},
			},
		},
		{
			Name:        "mac.screenshot",
			Description: "Capture the full Mac screen as PNG. No foreground guard — the screenshot includes whatever apps are visible. Returns the image plus the bundle ID and name of the current foreground app.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "mac.click",
			Description: "Click at LOGICAL global screen coordinates. Optional target_bundle_id verifies that app is in front before posting; omit to skip the check (LLM is responsible for activating the right app first).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":                map[string]any{"type": "number"},
					"y":                map[string]any{"type": "number"},
					"button":           map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "default": "left"},
					"clicks":           map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "default": 1},
					"target_bundle_id": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "mac.type",
			Description: "Type Unicode text at the current focus. Optional target_bundle_id verifies the right app is in front. Non-ASCII text automatically routes through the clipboard-paste fallback (some apps' IMEs drop CGEvent unicode events).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":             map[string]any{"type": "string"},
					"target_bundle_id": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "mac.key",
			Description: "Send a key combo at the current focus, e.g. \"return\", \"escape\", \"cmd+v\", \"cmd+shift+t\". Optional target_bundle_id verifies the right app is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"combo":            map[string]any{"type": "string"},
					"target_bundle_id": map[string]any{"type": "string"},
				},
				"required": []string{"combo"},
			},
		},
		{
			Name:        "mac.scroll",
			Description: "Wheel-scroll at LOGICAL screen point (x, y). Positive dy scrolls up. Optional target_bundle_id verifies the right app is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":                map[string]any{"type": "number"},
					"y":                map[string]any{"type": "number"},
					"dy":               map[string]any{"type": "number"},
					"dx":               map[string]any{"type": "number", "default": 0},
					"target_bundle_id": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y", "dy"},
			},
		},
		{
			Name:        "mac.clipboard_paste",
			Description: "Set the system clipboard to `text`, post cmd+v at the focused app, wait briefly, then restore the original clipboard. Use this to deliver text that the focused app's IME would drop via mac.type. Optional target_bundle_id verifies the right app is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":             map[string]any{"type": "string"},
					"target_bundle_id": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
	}
}

