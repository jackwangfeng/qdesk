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
			Name:        "wechat.list_chats",
			Description: "Return WeChat sidebar chat list as structured JSON: name, unread_count, last_msg_preview. Uses the Accessibility API — no vision needed.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "wechat.open_chat",
			Description: "Open the conversation with the given chat name. Uses fuzzy matching on the sidebar; falls back to the cmd+f search field if no sidebar match.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []string{"name"},
			},
		},
	}
}

// callTool is a stub; implementations land in Tasks 10-12.
func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	return nil, errNotImplemented(name)
}

func errNotImplemented(name string) error {
	return &notImplErr{name: name}
}

type notImplErr struct{ name string }

func (e *notImplErr) Error() string { return "tool not implemented: " + e.name }
