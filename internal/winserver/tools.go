package winserver

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *MCPServer) callToolReal(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "windows.front_app":
		return s.toolFrontApp(ctx)
	case "windows.screenshot":
		return s.toolScreenshot(ctx)
	default:
		return errToolResult(fmt.Errorf("unknown tool: %s", name)), nil
	}
}

func (s *MCPServer) toolFrontApp(ctx context.Context) (*ToolResult, error) {
	fa, err := s.native.FrontApp(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("frontApp.exe=%s pid=%d title=%q hwnd=0x%x", fa.Exe, fa.PID, fa.Title, fa.HWND),
	}}}, nil
}

func (s *MCPServer) toolScreenshot(ctx context.Context) (*ToolResult, error) {
	fa, err := s.native.FrontApp(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	shot, err := s.native.Screenshot(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{
		{Type: "image", MIMEType: "image/png", Data: shot.PNGBase64},
		{Type: "text", Text: fmt.Sprintf("frontApp.exe=%s title=%q  size=%dx%d (physical pixels)",
			fa.Exe, fa.Title, shot.Width, shot.Height)},
	}}, nil
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}
