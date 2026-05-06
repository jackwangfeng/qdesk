package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// ----- mac.front_app -----

func TestMacFrontAppReturnsCurrent(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.iphonesimulator","name":"Simulator","pid":4242}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "mac.front_app", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected: %+v", out)
	}
	body := out.Content[0].Text
	for _, want := range []string{"com.apple.iphonesimulator", "Simulator", "4242"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in: %s", want, body)
		}
	}
}

// ----- mac.activate -----

func TestMacActivateRequiresBundleID(t *testing.T) {
	srv := newServerWithFake(nil)
	out, err := srv.callTool(context.Background(), "mac.activate", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Errorf("expected isError on missing bundle_id; got %+v", out)
	}
}

func TestMacActivatePassesBundleIDToHelper(t *testing.T) {
	var captured string
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodActivate, func(p json.RawMessage) (json.RawMessage, error) {
			var v struct {
				BundleID string `json:"bundleId"`
			}
			_ = json.Unmarshal(p, &v)
			captured = v.BundleID
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.activate",
		json.RawMessage(`{"bundle_id":"com.apple.iphonesimulator"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if captured != "com.apple.iphonesimulator" {
		t.Errorf("activate bundle_id: got=%q want=com.apple.iphonesimulator", captured)
	}
}

// ----- mac.screenshot — no guard, includes whatever's in front -----

func TestMacScreenshotNoGuard(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		// Even with Finder in front (not WeChat), mac.screenshot proceeds.
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder"}`), nil
		})
		f.SetHandler(macproto.MethodScreenshot, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"pngBase64":"aGVsbG8=","width":1440,"height":900,"scaleFactor":2}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "mac.screenshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Errorf("mac.screenshot should not gate on foreground app; got %+v", out)
	}
}

// ----- mac.click guard behavior -----

func TestMacClickWithoutTargetSkipsGuard(t *testing.T) {
	frontCalled := false
	clickCalled := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			frontCalled = true
			return json.RawMessage(`{"bundleId":"com.apple.finder"}`), nil
		})
		f.SetHandler(macproto.MethodClick, func(_ json.RawMessage) (json.RawMessage, error) {
			clickCalled = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.click",
		json.RawMessage(`{"x":50,"y":60}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if frontCalled {
		t.Errorf("frontApp should NOT be called when target_bundle_id is empty")
	}
	if !clickCalled {
		t.Errorf("click should be dispatched when no target is set")
	}
}

func TestMacClickWithMatchingTargetPasses(t *testing.T) {
	clickCalled := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.iphonesimulator"}`), nil
		})
		f.SetHandler(macproto.MethodClick, func(_ json.RawMessage) (json.RawMessage, error) {
			clickCalled = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.click",
		json.RawMessage(`{"x":50,"y":60,"target_bundle_id":"com.apple.iphonesimulator"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !clickCalled {
		t.Errorf("click should fire when target matches front app")
	}
}

func TestMacClickWithMismatchTargetReturnsError(t *testing.T) {
	clickCalled := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder","name":"Finder"}`), nil
		})
		f.SetHandler(macproto.MethodClick, func(_ json.RawMessage) (json.RawMessage, error) {
			clickCalled = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "mac.click",
		json.RawMessage(`{"x":50,"y":60,"target_bundle_id":"com.apple.iphonesimulator"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Errorf("expected isError when front app != target; got %+v", out)
	}
	if clickCalled {
		t.Errorf("click should NOT fire when target mismatches")
	}
	if !strings.Contains(out.Content[0].Text, "foreground-mismatch") {
		t.Errorf("error must include foreground-mismatch code: %s", out.Content[0].Text)
	}
}

// ----- mac.type ASCII vs non-ASCII routing (with optional guard) -----

func TestMacTypeASCIIUsesType(t *testing.T) {
	gotType := false
	gotPaste := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(_ json.RawMessage) (json.RawMessage, error) {
			gotPaste = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.type",
		json.RawMessage(`{"text":"hello"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gotType || gotPaste {
		t.Errorf("ASCII routing wrong: type=%v paste=%v", gotType, gotPaste)
	}
}

func TestMacTypeNonASCIIUsesClipboardPaste(t *testing.T) {
	gotType := false
	gotPaste := false
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodType, func(_ json.RawMessage) (json.RawMessage, error) {
			gotType = true
			return json.RawMessage(`{"ok":true}`), nil
		})
		f.SetHandler(macproto.MethodClipboardPaste, func(_ json.RawMessage) (json.RawMessage, error) {
			gotPaste = true
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.type",
		json.RawMessage(`{"text":"你好"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotType || !gotPaste {
		t.Errorf("non-ASCII routing wrong: type=%v paste=%v", gotType, gotPaste)
	}
}

// ----- mac.key + mac.scroll + mac.clipboard_paste passthrough -----

func TestMacKeyPassthrough(t *testing.T) {
	got := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodKey, func(p json.RawMessage) (json.RawMessage, error) {
			got = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.key",
		json.RawMessage(`{"combo":"cmd+shift+t"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "cmd+shift+t") {
		t.Errorf("combo not propagated: %s", got)
	}
}

func TestMacScrollPassthrough(t *testing.T) {
	got := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodScroll, func(p json.RawMessage) (json.RawMessage, error) {
			got = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.scroll",
		json.RawMessage(`{"x":100,"y":200,"dy":-3}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, `"dy":-3`) {
		t.Errorf("dy not propagated: %s", got)
	}
}

func TestMacClipboardPasteRoutesToHelper(t *testing.T) {
	got := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodClipboardPaste, func(p json.RawMessage) (json.RawMessage, error) {
			got = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "mac.clipboard_paste",
		json.RawMessage(`{"text":"hello clipboard"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "hello clipboard") {
		t.Errorf("text not propagated: %s", got)
	}
}

// ----- tools/list now includes the mac.* surface -----

func TestToolsListIncludesMacSurface(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})
	b, _ := json.Marshal(resp.Result)
	got := string(b)
	for _, want := range []string{
		"mac.front_app", "mac.activate", "mac.screenshot",
		"mac.click", "mac.type", "mac.key", "mac.scroll",
		"mac.clipboard_paste",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tools/list missing %q", want)
		}
	}
}

// ----- wechat.* unchanged: foreground guard still hard-coded -----

func TestWechatGuardStillUnchanged(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.apple.finder"}`), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.click",
		json.RawMessage(`{"x":1,"y":2}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Errorf("wechat.click must still reject when WeChat isn't front")
	}
	if !strings.Contains(out.Content[0].Text, macproto.CodeWeChatNotForeground) {
		t.Errorf("missing wechat-not-foreground code: %s", out.Content[0].Text)
	}
}
