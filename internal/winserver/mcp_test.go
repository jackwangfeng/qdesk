package winserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitializeReturnsServerInfo(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	req := &RPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"}
	resp := srv.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result not a map: %T", resp.Result)
	}
	si, _ := m["serverInfo"].(map[string]any)
	if si["name"] != "qdesk-win" {
		t.Fatalf("serverInfo.name=%v, want qdesk-win", si["name"])
	}
}

func TestToolsListContainsAllWindowsTools(t *testing.T) {
	srv := NewMCPServer(&FakeNative{})
	req := &RPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	resp := srv.Handle(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m := resp.Result.(map[string]any)
	tools := m["tools"].([]ToolDef)
	want := []string{
		"windows.front_app", "windows.activate", "windows.screenshot",
		"windows.click", "windows.type", "windows.key",
		"windows.scroll", "windows.clipboard_paste",
	}
	got := map[string]bool{}
	for _, td := range tools {
		got[td.Name] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing tool %q in tools/list (got %s)", w, strings.Join(toolNames(tools), ","))
		}
	}
}

func toolNames(tools []ToolDef) []string {
	out := make([]string, len(tools))
	for i, td := range tools {
		out[i] = td.Name
	}
	return out
}
