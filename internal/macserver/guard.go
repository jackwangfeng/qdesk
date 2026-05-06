package macserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// requireWeChatForeground returns nil if WeChat is the front app, or a
// structured error otherwise. Used by the wechat.* action tools.
func requireWeChatForeground(ctx context.Context, h HelperClient) error {
	return requireForeground(ctx, h, wechatBundleID, macproto.CodeWeChatNotForeground,
		"call wechat.ensure_foreground first")
}

// requireForeground is the generic version: checks that the given bundle
// ID is the foreground app. Empty bundleID means "no check" (caller
// trusts the LLM to have called mac.activate first).
//
// The mac.* tools use this with a per-call target_bundle_id parameter
// so the LLM can drive arbitrary apps without ripping out the guard.
func requireForeground(ctx context.Context, h HelperClient, bundleID, errCode, hint string) error {
	if bundleID == "" {
		return nil
	}
	raw, err := h.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return fmt.Errorf("frontApp: %w", err)
	}
	var fa macproto.FrontAppResponse
	if err := json.Unmarshal(raw, &fa); err != nil {
		return fmt.Errorf("decode frontApp: %w", err)
	}
	if fa.BundleID != bundleID {
		return &guardErr{
			Code: errCode,
			Msg:  fmt.Sprintf("front app is %s (%q), wanted %s; %s", fa.BundleID, fa.Name, bundleID, hint),
		}
	}
	return nil
}

type guardErr struct {
	Code string
	Msg  string
}

func (e *guardErr) Error() string { return e.Code + ": " + e.Msg }
