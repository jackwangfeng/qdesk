package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macproto"
)

func newServerWithFake(setup func(*FakeSupervisor)) *MCPServer {
	f := NewFakeSupervisor()
	if setup != nil {
		setup(f)
	}
	return NewMCPServer(f)
}

func TestEnsureForegroundActivatesWeChat(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder","pid":1}`), nil
		})
		f.SetHandler(macproto.MethodActivate, func(p json.RawMessage) (json.RawMessage, error) {
			if !strings.Contains(string(p), "com.tencent.xinWeChat") {
				t.Errorf("activate not called with WeChat bundle: %s", p)
			}
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.ensure_foreground", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Errorf("unexpected error: %+v", out)
	}
}

func TestScreenshotPassesThrough(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat","name":"WeChat","pid":99}`), nil
		})
		f.SetHandler(macproto.MethodScreenshot, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"pngBase64":"aGVsbG8=","width":1440,"height":900,"scaleFactor":2}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.screenshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error: %+v", out)
	}
	hasImage := false
	hasText := false
	for _, c := range out.Content {
		if c.Type == "image" && c.MIMEType == "image/png" && c.Data == "aGVsbG8=" {
			hasImage = true
		}
		if c.Type == "text" && strings.Contains(c.Text, "com.tencent.xinWeChat") {
			hasText = true
		}
	}
	if !hasImage || !hasText {
		t.Errorf("missing content: image=%v text=%v in %+v", hasImage, hasText, out.Content)
	}
}

func TestForegroundGuardRejectsWhenNotWeChat(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder","pid":1}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.click",
		json.RawMessage(`{"x":100,"y":200}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected isError; got: %+v", out)
	}
	if !strings.Contains(out.Content[0].Text, macproto.CodeWeChatNotForeground) {
		t.Errorf("expected wechat-not-foreground code in error; got %s", out.Content[0].Text)
	}
}
