package macserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// fixtureSidebar simulates the AX tree the helper would return for the
// WeChat chat sidebar. Each chat row is an AXRow with title="Name" and
// description="last message preview". Unread badge is in value="N".
const fixtureSidebar = `{
  "nodes": [
    {"path":"0/1/2/0","role":"AXRow","title":"张三","value":"2","description":"晚点到，10分钟","frame":{"x":0,"y":40,"w":280,"h":56}},
    {"path":"0/1/2/1","role":"AXRow","title":"李四","value":"","description":"OK","frame":{"x":0,"y":96,"w":280,"h":56}},
    {"path":"0/1/2/2","role":"AXRow","title":"产品讨论群","value":"5","description":"小王: 周三发版","frame":{"x":0,"y":152,"w":280,"h":56}}
  ]
}`

func TestListChatsParsesAXTree(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.list_chats", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error: %+v", out)
	}
	body := out.Content[0].Text
	for _, want := range []string{"张三", "李四", "产品讨论群", "unread_count\": 2", "unread_count\": 5"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body: %s", want, body)
		}
	}
}

func TestOpenChatExactMatch(t *testing.T) {
	clicked := ""
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
		f.SetHandler(macproto.MethodAXClick, func(p json.RawMessage) (json.RawMessage, error) {
			clicked = string(p)
			return json.RawMessage(`{"ok":true}`), nil
		})
	})
	if _, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"张三"}`)); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(clicked, `"path":"0/1/2/0"`) {
		t.Errorf("expected to click 张三 (path 0/1/2/0); clicked=%s", clicked)
	}
}

func TestOpenChatNotFoundReturnsError(t *testing.T) {
	srv := newServerWithFake(func(f *FakeSupervisor) {
		f.SetHandler(macproto.MethodFrontApp, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"bundleId":"com.tencent.xinWeChat"}`), nil
		})
		f.SetHandler(macproto.MethodAXTree, func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(fixtureSidebar), nil
		})
	})
	out, err := srv.callTool(context.Background(), "wechat.open_chat",
		json.RawMessage(`{"name":"陈八"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected error result; got %+v", out)
	}
	if !strings.Contains(out.Content[0].Text, macproto.CodeChatNotFound) {
		t.Errorf("missing chat-not-found code: %s", out.Content[0].Text)
	}
}
