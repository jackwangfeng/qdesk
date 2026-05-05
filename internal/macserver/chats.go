package macserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jeffwang/qdesk/internal/macproto"
)

// ChatRow is the structured representation we surface to the LLM.
type ChatRow struct {
	Name           string `json:"name"`
	UnreadCount    int    `json:"unread_count"`
	LastMsgPreview string `json:"last_msg_preview"`
	axPath         string // not serialised; used internally for open_chat
}

func (s *MCPServer) toolListChats(ctx context.Context) (*ToolResult, error) {
	rows, err := s.fetchChats(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	body, _ := json.MarshalIndent(rows, "", "  ")
	return &ToolResult{Content: []ContentItem{{Type: "text", Text: string(body)}}}, nil
}

func (s *MCPServer) toolOpenChat(ctx context.Context, args json.RawMessage) (*ToolResult, error) {
	var in struct{ Name string }
	if err := json.Unmarshal(args, &in); err != nil {
		return errToolResult(err), nil
	}
	if in.Name == "" {
		return errToolResult(errors.New("name is required")), nil
	}
	rows, err := s.fetchChats(ctx)
	if err != nil {
		return errToolResult(err), nil
	}
	match := matchChat(rows, in.Name)
	if match == nil {
		// v1 simplification: no automatic cmd+f search-box fallback.
		// The LLM can compose that itself: wechat.key cmd+f, wechat.type, wechat.key return.
		return errToolResult(&HelperError{
			Code:    macproto.CodeChatNotFound,
			Message: fmt.Sprintf("no chat matched %q in sidebar (%d shown). Try cmd+f search box manually.", in.Name, len(rows)),
		}), nil
	}
	clickBody, _ := json.Marshal(macproto.AXClickRequest{
		BundleID: wechatBundleID, Path: match.axPath,
	})
	if _, err := s.helper.Call(ctx, macproto.MethodAXClick, clickBody); err != nil {
		return errToolResult(err), nil
	}
	return &ToolResult{Content: []ContentItem{{Type: "text",
		Text: fmt.Sprintf("opened chat %q", match.Name)}}}, nil
}

func (s *MCPServer) fetchChats(ctx context.Context) ([]ChatRow, error) {
	body, _ := json.Marshal(macproto.AXTreeRequest{
		BundleID: wechatBundleID, Query: "role=AXRow",
	})
	raw, err := s.helper.Call(ctx, macproto.MethodAXTree, body)
	if err != nil {
		return nil, err
	}
	var tree macproto.AXTreeResponse
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	if len(tree.Nodes) == 0 {
		return nil, &HelperError{
			Code:    macproto.CodeAXTreeEmpty,
			Message: "WeChat sidebar AX tree is empty (open the main window first: cmd+1)",
		}
	}
	out := make([]ChatRow, 0, len(tree.Nodes))
	for _, n := range tree.Nodes {
		if n.Title == "" {
			continue
		}
		unread := 0
		if n.Value != "" {
			if v, err := strconv.Atoi(strings.TrimSpace(n.Value)); err == nil {
				unread = v
			}
		}
		out = append(out, ChatRow{
			Name:           n.Title,
			UnreadCount:    unread,
			LastMsgPreview: n.Description,
			axPath:         n.Path,
		})
	}
	return out, nil
}

// matchChat finds the best chat row for the given query name.
// Order: exact match > prefix match > substring match. Returns nil if none.
func matchChat(rows []ChatRow, name string) *ChatRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	for i := range rows {
		if strings.HasPrefix(rows[i].Name, name) {
			return &rows[i]
		}
	}
	for i := range rows {
		if strings.Contains(rows[i].Name, name) {
			return &rows[i]
		}
	}
	return nil
}
