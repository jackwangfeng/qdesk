package winserver

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *MCPServer) callToolReal(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "windows.activate":
		return s.toolActivate(ctx, args)
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

func (s *MCPServer) toolActivate(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		HWND       uintptr `json:"hwnd"`
		Exe        string  `json:"exe"`
		TitleRegex string  `json:"title_regex"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errToolResult(err), nil
		}
	}
	if in.HWND == 0 && in.Exe == "" && in.TitleRegex == "" {
		return errToolResult(fmt.Errorf("activate: provide one of hwnd, exe, or title_regex")), nil
	}
	resp, err := s.native.Activate(ctx, ActivateReq{HWND: in.HWND, Exe: in.Exe, TitleRegex: in.TitleRegex})
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("activated hwnd=0x%x actually_foreground=%t", resp.HWND, resp.ActuallyForeground),
	}}}, nil
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}
