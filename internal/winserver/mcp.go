package winserver

import (
	"context"
	"encoding/json"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "qdesk-win"
	serverVersion   = "0.1.0"
)

// RPCRequest mirrors the qdesk-mcp envelope shape used by macserver.
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

type MCPServer struct {
	native Native
}

func NewMCPServer(n Native) *MCPServer { return &MCPServer{native: n} }

func (s *MCPServer) Handle(ctx context.Context, req *RPCRequest) *RPCResponse {
	resp := &RPCResponse{JSONRPC: "2.0", ID: req.ID}
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
			Name:        "windows.front_app",
			Description: "Return HWND, PID, exe basename and window title of the current foreground window. Use to discover what's in front before screenshotting or sending input.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "windows.activate",
			Description: "Bring a window to the foreground. Provide hwnd, exe (basename like \"notepad.exe\"), or title_regex. Priority: hwnd > exe > title_regex. Returns actually_foreground=false if Windows refused the focus change (caller must not assume success).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hwnd":        map[string]any{"type": "integer"},
					"exe":         map[string]any{"type": "string"},
					"title_regex": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "windows.screenshot",
			Description: "Capture the primary monitor as PNG (physical pixels). No foreground guard. Returns the image plus the foreground exe + title.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "windows.click",
			Description: "Click at PHYSICAL pixel coordinates. Optional expected_exe verifies that exe is in front before posting; omit to skip the check.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":            map[string]any{"type": "integer"},
					"y":            map[string]any{"type": "integer"},
					"button":       map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "default": "left"},
					"double":       map[string]any{"type": "boolean", "default": false},
					"modifiers":    map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"ctrl", "shift", "alt", "win"}}},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "windows.type",
			Description: "Type Unicode text at the current focus. ASCII-only text uses SendInput KEYEVENTF_UNICODE; non-ASCII auto-routes through the clipboard fallback (some old Win32 controls drop unicode events). Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":         map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "windows.key",
			Description: "Send a key combo at the current focus, e.g. \"return\", \"escape\", \"ctrl+f\", \"win+r\", \"alt+tab\". Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"combo":        map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"combo"},
			},
		},
		{
			Name:        "windows.scroll",
			Description: "Wheel-scroll at physical pixel point (x, y). Positive dy scrolls up. dx (horizontal wheel) is accepted but many apps ignore it. Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":            map[string]any{"type": "integer"},
					"y":            map[string]any{"type": "integer"},
					"dy":           map[string]any{"type": "integer"},
					"dx":           map[string]any{"type": "integer", "default": 0},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"x", "y", "dy"},
			},
		},
		{
			Name:        "windows.clipboard_paste",
			Description: "Set the system clipboard to `text`, post ctrl+v at the focused window, wait briefly, then restore the original clipboard. Returns clipboard_restored=false if backup or restore failed (paste still happens). Optional expected_exe verifies the right exe is in front.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":         map[string]any{"type": "string"},
					"expected_exe": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		},
	}
}

func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	return s.callToolReal(ctx, name, args)
}
