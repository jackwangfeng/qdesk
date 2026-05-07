package winserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func callTool(t *testing.T, srv *MCPServer, name string, args any) *ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := srv.callTool(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("callTool returned dispatch error: %v", err)
	}
	return out
}

func TestFrontAppToolReturnsExeAndTitle(t *testing.T) {
	n := &FakeNative{FrontAppFn: func() (FrontApp, error) {
		return FrontApp{HWND: 0xDEAD, PID: 1234, Exe: "notepad.exe", Title: "Untitled - Notepad"}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.front_app", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected IsError: %+v", res)
	}
	got := res.Content[0].Text
	for _, want := range []string{"notepad.exe", "1234", "Untitled - Notepad"} {
		if !strings.Contains(got, want) {
			t.Errorf("front_app text %q missing %q", got, want)
		}
	}
}

func TestScreenshotToolReturnsImageContent(t *testing.T) {
	n := &FakeNative{
		FrontAppFn: func() (FrontApp, error) {
			return FrontApp{Exe: "explorer.exe", Title: "Desktop"}, nil
		},
		ScreenshotFn: func() (Screenshot, error) {
			return Screenshot{PNGBase64: "iVBORw0KGgo=", Width: 1920, Height: 1080}, nil
		},
	}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.screenshot", map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected IsError: %+v", res)
	}
	if len(res.Content) < 2 {
		t.Fatalf("expected image + text content, got %+v", res.Content)
	}
	if res.Content[0].Type != "image" || res.Content[0].MIMEType != "image/png" {
		t.Errorf("first content not PNG image: %+v", res.Content[0])
	}
	if res.Content[0].Data != "iVBORw0KGgo=" {
		t.Errorf("PNG data not propagated: %q", res.Content[0].Data)
	}
	if !strings.Contains(res.Content[1].Text, "explorer.exe") || !strings.Contains(res.Content[1].Text, "1920x1080") {
		t.Errorf("screenshot text missing exe or dims: %q", res.Content[1].Text)
	}
}

func TestActivateRequiresAtLeastOneTarget(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	res := callTool(t, srv, "windows.activate", map[string]any{})
	if !res.IsError {
		t.Fatalf("expected IsError, got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "hwnd, exe, or title_regex") {
		t.Errorf("error message should mention required fields, got %q", res.Content[0].Text)
	}
}

func TestActivatePassesArgsToNative(t *testing.T) {
	var got ActivateReq
	n := &FakeNative{ActivateFn: func(r ActivateReq) (ActivateResp, error) {
		got = r
		return ActivateResp{HWND: 0xBEEF, ActuallyForeground: true}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.activate", map[string]any{
		"exe":         "notepad.exe",
		"title_regex": "Untitled.*",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if got.Exe != "notepad.exe" || got.TitleRegex != "Untitled.*" {
		t.Errorf("native got wrong req: %+v", got)
	}
	if !strings.Contains(res.Content[0].Text, "actually_foreground=true") {
		t.Errorf("text should report actually_foreground; got %q", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "0xbeef") {
		t.Errorf("text should report hwnd; got %q", res.Content[0].Text)
	}
}

func TestActivateReportsForegroundFailureWithoutErroring(t *testing.T) {
	n := &FakeNative{ActivateFn: func(r ActivateReq) (ActivateResp, error) {
		return ActivateResp{HWND: 0x1234, ActuallyForeground: false}, nil
	}}
	srv := NewMCPServer(n)
	res := callTool(t, srv, "windows.activate", map[string]any{"exe": "notepad.exe"})
	if res.IsError {
		t.Fatalf("activate should not IsError when only foreground steal failed; got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "actually_foreground=false") {
		t.Errorf("text should expose actually_foreground=false; got %q", res.Content[0].Text)
	}
}
