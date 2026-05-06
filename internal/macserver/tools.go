package macserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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
	case "wechat.open_chat":
		if err := requireWeChatForeground(ctx, s.helper); err != nil {
			return errToolResult(err), nil
		}
		return s.toolOpenChat(ctx, args)

	// Generic mac.* — drive any app by bundle ID; per-call optional guard.
	case "mac.front_app":
		return s.toolMacFrontApp(ctx)
	case "mac.activate":
		return s.toolMacActivate(ctx, args)
	case "mac.screenshot":
		return s.toolMacScreenshot(ctx)
	case "mac.click":
		return s.toolMacClick(ctx, args)
	case "mac.type":
		return s.toolMacType(ctx, args)
	case "mac.key":
		return s.toolMacKey(ctx, args)
	case "mac.scroll":
		return s.toolMacScroll(ctx, args)
	case "mac.clipboard_paste":
		return s.toolMacClipboardPaste(ctx, args)

	default:
		return errToolResult(fmt.Errorf("unknown tool: %s", name)), nil
	}
}

// macTargetGuard runs the optional foreground check used by every mac.*
// action tool. Returns nil when target_bundle_id is empty (no check) or
// when the front app matches.
func (s *MCPServer) macTargetGuard(ctx context.Context, target string) error {
	return requireForeground(ctx, s.helper, target,
		"foreground-mismatch",
		"call mac.activate with this bundle_id first")
}

func (s *MCPServer) toolMacFrontApp(ctx context.Context) (*ToolResult, error) {
	raw, err := s.helper.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return errToolResult(err), nil
	}
	var fa macproto.FrontAppResponse
	if err := json.Unmarshal(raw, &fa); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("frontApp.bundleId=%s name=%q pid=%d", fa.BundleID, fa.Name, fa.PID)}}}, nil
}

func (s *MCPServer) toolMacActivate(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		BundleID string `json:"bundle_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if in.BundleID == "" {
		return errToolResult(fmt.Errorf("bundle_id is required")), nil
	}
	body, _ := json.Marshal(macproto.ActivateRequest{BundleID: in.BundleID})
	if _, err := s.helper.Call(ctx, macproto.MethodActivate, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("activated %s", in.BundleID)}}}, nil
}

func (s *MCPServer) toolMacScreenshot(ctx context.Context) (*ToolResult, error) {
	// Same payload as wechat.screenshot — no guard.
	return s.toolScreenshot(ctx)
}

func (s *MCPServer) toolMacClick(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y           float64
		Button         string
		Clicks         int
		TargetBundleID string `json:"target_bundle_id"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if err := s.macTargetGuard(ctx, in.TargetBundleID); err != nil {
		return errToolResult(err), nil
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

func (s *MCPServer) toolMacType(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text           string
		TargetBundleID string `json:"target_bundle_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := s.macTargetGuard(ctx, in.TargetBundleID); err != nil {
		return errToolResult(err), nil
	}
	if isASCII(in.Text) {
		body, _ := json.Marshal(macproto.TypeRequest{Text: in.Text})
		if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
			return errToolResult(err), nil
		}
		return &ToolResult{Content: []ContentItem{{Type: "text",
			Text: fmt.Sprintf("typed %d characters (CGEvent unicode)", len([]rune(in.Text)))}}}, nil
	}
	body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d characters (clipboard fallback for non-ASCII)", len([]rune(in.Text)))}}}, nil
}

func (s *MCPServer) toolMacKey(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Combo          string
		TargetBundleID string `json:"target_bundle_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := s.macTargetGuard(ctx, in.TargetBundleID); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.KeyRequest{Combo: in.Combo})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("sent key %q", in.Combo)}}}, nil
}

func (s *MCPServer) toolMacScroll(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		X, Y, DX, DY   float64
		TargetBundleID string `json:"target_bundle_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := s.macTargetGuard(ctx, in.TargetBundleID); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.ScrollRequest{X: in.X, Y: in.Y, DX: in.DX, DY: in.DY})
	if _, err := s.helper.Call(ctx, macproto.MethodScroll, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("scrolled (dx=%.1f dy=%.1f) at (%.1f, %.1f)", in.DX, in.DY, in.X, in.Y)}}}, nil
}

func (s *MCPServer) toolMacClipboardPaste(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct {
		Text           string
		TargetBundleID string `json:"target_bundle_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if err := s.macTargetGuard(ctx, in.TargetBundleID); err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d characters", len([]rune(in.Text)))}}}, nil
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
	if isASCII(in.Text) {
		body, _ := json.Marshal(macproto.TypeRequest{Text: in.Text})
		if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
			return errToolResult(err), nil
		}
		return &ToolResult{Content: []ContentItem{{Type: "text",
			Text: fmt.Sprintf("typed %d characters (CGEvent unicode)", len([]rune(in.Text)))}}}, nil
	}
	// Non-ASCII: WeChat's IME drops CGEvent unicode chars; route through
	// the helper's clipboard-paste path which restores the prior clipboard.
	body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Text})
	if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("pasted %d characters (clipboard fallback for non-ASCII)", len([]rune(in.Text)))}}}, nil
}

// isASCII returns true iff every rune in s is in 0x00-0x7F.
func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return false
		}
	}
	return true
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

// Tunable timing for toolOpenChat. Empirical defaults: 400ms after
// cmd+f, 250ms after typing — bumped slightly from v1.1's 300/200,
// which the real-WeChat E2E showed worked but with no margin.
// Override via env vars (Go duration strings, e.g. "600ms"):
//
//	QDESK_MAC_OPENCHAT_CMDF_DELAY
//	QDESK_MAC_OPENCHAT_TYPE_DELAY
var (
	openChatPostCmdFDelay = envDuration("QDESK_MAC_OPENCHAT_CMDF_DELAY", 400*time.Millisecond)
	openChatPostTypeDelay = envDuration("QDESK_MAC_OPENCHAT_TYPE_DELAY", 250*time.Millisecond)
)

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// toolOpenChat opens a WeChat conversation by name via the keyboard
// search bar (cmd+f). Bypasses the Accessibility tree, which WeChat 4.x
// no longer exposes for the chat sidebar.
//
// Sequence:
//  1. key cmd+f                         (open WeChat search; ~400ms to render)
//  2. type / clipboardPaste <name>      (search filters live)
//  3. key return                        (open the top match)
//
// We do NOT verify which chat actually opened — WeChat's own search
// matching is opaque. The LLM should call wechat.screenshot afterward
// to confirm the right chat is in front.
func (s *MCPServer) toolOpenChat(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Name string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if in.Name == "" {
		return errToolResult(fmt.Errorf("name is required")), nil
	}

	// 1. cmd+f
	cmdfBody, _ := json.Marshal(macproto.KeyRequest{Combo: "cmd+f"})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, cmdfBody); err != nil {
		return errToolResult(err), nil
	}
	time.Sleep(openChatPostCmdFDelay)

	// 2. type the name (route Chinese through clipboardPaste)
	if isASCII(in.Name) {
		body, _ := json.Marshal(macproto.TypeRequest{Text: in.Name})
		if _, err := s.helper.Call(ctx, macproto.MethodType, body); err != nil {
			return errToolResult(err), nil
		}
	} else {
		body, _ := json.Marshal(macproto.ClipboardPasteRequest{Text: in.Name})
		if _, err := s.helper.Call(ctx, macproto.MethodClipboardPaste, body); err != nil {
			return errToolResult(err), nil
		}
	}
	time.Sleep(openChatPostTypeDelay)

	// 3. return to open the top result
	retBody, _ := json.Marshal(macproto.KeyRequest{Combo: "return"})
	if _, err := s.helper.Call(ctx, macproto.MethodKey, retBody); err != nil {
		return errToolResult(err), nil
	}

	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("issued cmd+f / paste %q / return — verify with wechat.screenshot which chat actually opened", in.Name)}}}, nil
}
