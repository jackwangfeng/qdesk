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

func TestClickPassesXYButtonClicks(t *testing.T) {
	var captured json.RawMessage
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodClick, func(p json.RawMessage) (json.RawMessage, error) {
			captured = p
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.click",
		json.RawMessage(`{"x":12.5,"y":34.5,"button":"left","clicks":2}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected: %+v", out)
	}
	want := `"x":12.5`
	if !strings.Contains(string(captured), want) {
		t.Errorf("captured payload missing %s: %s", want, captured)
	}
	if !strings.Contains(string(captured), `"clicks":2`) {
		t.Errorf("clicks not propagated: %s", captured)
	}
}

func TestTypeSendsText(t *testing.T) {
	var captured json.RawMessage
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(p json.RawMessage) (json.RawMessage, error) {
			captured = p
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"你好"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected: %+v", out)
	}
	if !strings.Contains(string(captured), "你好") {
		t.Errorf("text not propagated: %s", captured)
	}
}

func TestKeyAndScrollPassthrough(t *testing.T) {
	gotKey := ""
	gotScroll := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodKey, func(p json.RawMessage) (json.RawMessage, error) {
			gotKey = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodScroll, func(p json.RawMessage) (json.RawMessage, error) {
			gotScroll = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.key",
		json.RawMessage(`{"combo":"return"}`)); err != nil {
		t.Fatalf("key err: %v", err)
	}
	if !strings.Contains(gotKey, "return") {
		t.Errorf("key combo not propagated: %s", gotKey)
	}
	if _, err := srv.callTool(context.Background(), "wechat.scroll",
		json.RawMessage(`{"x":100,"y":200,"dy":-3,"dx":0}`)); err != nil {
		t.Fatalf("scroll err: %v", err)
	}
	if !strings.Contains(gotScroll, `"dy":-3`) {
		t.Errorf("scroll dy not propagated: %s", gotScroll)
	}
}

func TestTypeRoutesASCIIThroughCGEventType(t *testing.T) {
	gotType := false
	gotPaste := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(_ json.RawMessage) (json.RawMessage, error) {
			gotPaste = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"hello world"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gotType {
		t.Errorf("ASCII text should route through MethodType")
	}
	if gotPaste {
		t.Errorf("ASCII text should NOT trigger clipboardPaste")
	}
}

func TestTypeRoutesNonASCIIThroughClipboardPaste(t *testing.T) {
	var pasteText string
	gotType := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(p json.RawMessage) (json.RawMessage, error) {
			var v struct{ Text string }
			_ = json.Unmarshal(p, &v)
			pasteText = v.Text
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.type",
		json.RawMessage(`{"text":"你好"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotType {
		t.Errorf("non-ASCII text should NOT route through MethodType")
	}
	if pasteText != "你好" {
		t.Errorf("clipboardPaste payload mismatch: got=%q want=%q", pasteText, "你好")
	}
}
