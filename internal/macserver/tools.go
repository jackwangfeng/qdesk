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
	case "wechat.click", "wechat.type", "wechat.key", "wechat.scroll":
		// Action tools all share the foreground guard; implementations live
		// in the next task. For now return a typed not-implemented.
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return errToolResult(fmt.Errorf("tool not yet implemented: %s", name)), nil
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
