package macserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jeffwang/qdesk/internal/macproto"
)

func (s *MCPServer) callTool(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	switch name {
	case "wechat.ensure_foreground":
		return s.toolEnsureForeground(ctx)
	case "wechat.screenshot":
		return s.toolScreenshot(ctx)
	case "wechat.click":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolClick(ctx, args)
	case "wechat.type":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolType(ctx, args)
	case "wechat.key":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolKey(ctx, args)
	case "wechat.scroll":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolScroll(ctx, args)
	case "wechat.list_chats", "wechat.open_chat":
		return errToolResult(fmt.Errorf("tool not yet implemented: %s", name)), nil
	default:
		return errToolResult(fmt.Errorf("unknown tool: %s", name)), nil
	}
}

func (s *MCPServer) toolEnsureForeground(ctx context.Context) (*ToolResult, error) {
	body, _ := json.Marshal(macproto.ActivateRequest{BundleID: wechatBundleID})
	if _, err := s.helper.Call(ctx, macproto.MethodActivate, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{
		Content: []ContentItem{{Type: "text", Text: "WeChat brought to foreground."}},
	}, nil
}

func (s *MCPServer) toolScreenshot(ctx context.Context) (*ToolResult, error) {
	// Get foreground info first so we can include it in the response.
	frontRaw, err := s.helper.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return errToolResult(err), nil
	}
	var front macproto.FrontAppResponse
	_ = json.Unmarshal(frontRaw, &front)

	shotRaw, err := s.helper.Call(ctx, macproto.MethodScreenshot, json.RawMessage(`{"format":"png"}`))
	if err != nil {
		return errToolResult(err), nil
	}
	var shot macproto.ScreenshotResponse
	if err := json.Unmarshal(shotRaw, &shot); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{
		Content: []ContentItem{
			{Type: "image", MIMEType: "image/png", Data: shot.PNGBase64},
			{Type: "text", Text: fmt.Sprintf(
				"frontApp.bundleId=%s name=%q  size=%dx%d (logical) scale=%.1f",
				front.BundleID, front.Name, shot.Width, shot.Height, shot.ScaleFactor)},
		},
	}, nil
}

func errToolResult(err error) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: err.Error()}},
	}
}

func (s *MCPServer) toolClick(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y   float64
		Button string
		Clicks int
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Button == "" {
		in.Button = "left"
	}
	if in.Clicks == 0 {
		in.Clicks = 1
	}
	body, _ := json.Marshal(macproto.ClickRequest{
		X: in.X, Y: in.Y, Button: in.Button, Clicks: in.Clicks,
	})
	if _, err := s.helper.Call(ctx, macproto.MethodClick, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("clicked %s×%d at (%.1f, %.1f)", in.Button, in.Clicks, in.X, in.Y)}}}, nil
}

func (s *MCPServer) toolType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Text string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.TypeRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("typed %d characters", len([]rune(in.Text)))}}}, nil
}

func (s *MCPServer) toolKey(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Combo string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.KeyRequest{Combo: in.Combo})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("sent key %q", in.Combo)}}}, nil
}

func (s *MCPServer) toolScroll(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ X, Y, DX, DY float64 }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.ScrollRequest{X: in.X, Y: in.Y, DX: in.DX, DY: in.DY})
	if _, err := s.helper.Call(ctx, macproto.MethodScroll, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("scrolled (dx=%.1f dy=%.1f) at (%.1f, %.1f)", in.DX, in.DY, in.X, in.Y)}}}, nil
}
