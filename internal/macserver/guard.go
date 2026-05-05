package macserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// requireWeChatForeground returns nil if WeChat is the front app, or a
// structured error otherwise. Used by every action tool.
func requireWeChatForeground(ctx context.Context, h HelperClient) error {
	raw, err := h.Call(ctx, macproto.MethodFrontApp, json.RawMessage(`{}`))
	if err != nil {
		return fmt.Errorf("frontApp: %w", err)
	}
	var fa macproto.FrontAppResponse
	if err := json.Unmarshal(raw, &fa); err != nil {
		return fmt.Errorf("decode frontApp: %w", err)
	}
	if fa.BundleID != wechatBundleID {
		return &guardErr{
			Code: macproto.CodeWeChatNotForeground,
			Msg:  fmt.Sprintf("front app is %s (%s); call wechat.ensure_foreground first", fa.BundleID, fa.Name),
		}
	}
	return nil
}

type guardErr struct {
	Code string
	Msg  string
}

func (e *guardErr) Error() string { return e.Code + ": " + e.Msg }
