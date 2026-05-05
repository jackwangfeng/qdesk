package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolsListIncludesExpectedTools(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	got := string(b)
	for _, want := range []string{
		"wechat.screenshot", "wechat.click", "wechat.type", "wechat.key",
		"wechat.scroll", "wechat.ensure_foreground", "wechat.open_chat",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tools/list missing %q; got=%s", want, got)
		}
	}
}

func TestToolsListDoesNotIncludeListChats(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	})
	b, _ := json.Marshal(resp.Result)
	if strings.Contains(string(b), "wechat.list_chats") {
		t.Errorf("wechat.list_chats was removed in v1.1; it must not appear in tools/list")
	}
}

func TestInitializeReturnsServerInfo(t *testing.T) {
	srv := NewMCPServer(NewFakeSupervisor())
	resp := srv.Handle(context.Background(), &RPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(b), "qdesk-mac") {
		t.Errorf("expected serverInfo.name=qdesk-mac; got %s", b)
	}
}
