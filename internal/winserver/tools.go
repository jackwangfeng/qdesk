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
	case "windows.click":
		return s.toolClick(ctx, args)
	case "windows.key":
		return s.toolKey(ctx, args)
	case "windows.scroll":
		return s.toolScroll(ctx, args)
	case "windows.type":
		return s.toolType(ctx, args)
	case "windows.clipboard_paste":
		return s.toolClipboardPaste(ctx, args)
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

func (s *MCPServer) toolClick(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y        int
		Button      string
		Double      bool
		Modifiers   []string
		ExpectedExe string `json:"expected_exe"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errToolResult(err), nil
		}
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if in.Button == "" {
		in.Button = "left"
	}
	if err := s.native.Click(ctx, ClickReq{
		X: in.X, Y: in.Y, Button: in.Button, Double: in.Double, Modifiers: in.Modifiers,
	}); err != nil {
		return errToolResult(err), nil
	}
	dbl := ""
	if in.Double {
		dbl = " (double)"
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("clicked %s%s at (%d, %d)", in.Button, dbl, in.X, in.Y),
	}}}, nil
}

func (s *MCPServer) toolKey(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Combo       string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if err := s.native.Key(ctx, in.Combo); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("sent key %q", in.Combo),
	}}}, nil
}

func (s *MCPServer) toolScroll(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y, DX, DY int
		ExpectedExe  string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if err := s.native.Scroll(ctx, ScrollReq{X: in.X, Y: in.Y, DX: in.DX, DY: in.DY}); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("scrolled (dx=%d dy=%d) at (%d, %d)", in.DX, in.DY, in.X, in.Y),
	}}}, nil
}

func (s *MCPServer) toolType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text        string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	if isASCII(in.Text) {
		if err := s.native.Type(ctx, in.Text); err != nil {
			return errToolResult(err), nil
		}
		return &ToolResult{Content: []ContentItem{{Type: "text",
			Text: fmt.Sprintf("typed %d chars (path=unicode)", len([]rune(in.Text))),
		}}}, nil
	}
	resp, err := s.native.ClipboardPaste(ctx, in.Text)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("typed %d chars (path=clipboard) clipboard_restored=%t",
			len([]rune(in.Text)), resp.Restored),
	}}}, nil
}

func (s *MCPServer) toolClipboardPaste(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text        string
		ExpectedExe string `json:"expected_exe"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := requireForeground(ctx, s.native, in.ExpectedExe); err != nil {
		return errToolResult(err), nil
	}
	resp, err := s.native.ClipboardPaste(ctx, in.Text)
	if err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d chars clipboard_restored=%t",
			len([]rune(in.Text)), resp.Restored),
	}}}, nil
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return false
		}
	}
	return true
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}
