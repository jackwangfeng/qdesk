package agentd

import (
	"context"
	"reflect"
	"testing"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

func TestMockInputRecordsClick(t *testing.T) {
	m := &MockInput{}
	a := &protocol.Action{Type: protocol.ActionClick, X: 10, Y: 20, Button: protocol.MouseLeft}
	if err := m.Execute(context.Background(), a); err != nil {
		t.Fatalf("execute: %v", err)
	}
	rec := m.Snapshot()
	if len(rec) != 1 {
		t.Fatalf("recorded len: got=%d want=1", len(rec))
	}
	if !reflect.DeepEqual(rec[0], a) {
		t.Errorf("recorded mismatch: got=%+v want=%+v", rec[0], a)
	}
}

func TestMockInputRecordsType(t *testing.T) {
	m := &MockInput{}
	a := &protocol.Action{Type: protocol.ActionType_, Text: "hello"}
	if err := m.Execute(context.Background(), a); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := m.Snapshot()[0]; !reflect.DeepEqual(got, a) {
		t.Errorf("got=%+v want=%+v", got, a)
	}
}

func TestMockInputRecordsKey(t *testing.T) {
	m := &MockInput{}
	a := &protocol.Action{Type: protocol.ActionKey, Keys: []string{"ctrl", "s"}}
	if err := m.Execute(context.Background(), a); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := m.Snapshot()[0]; !reflect.DeepEqual(got, a) {
		t.Errorf("got=%+v want=%+v", got, a)
	}
}

func TestMockInputRecordsScroll(t *testing.T) {
	m := &MockInput{}
	a := &protocol.Action{Type: protocol.ActionScroll, X: 100, Y: 200, DY: -3}
	if err := m.Execute(context.Background(), a); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := m.Snapshot()[0]; !reflect.DeepEqual(got, a) {
		t.Errorf("got=%+v want=%+v", got, a)
	}
}

func TestMockInputRecordsDrag(t *testing.T) {
	m := &MockInput{}
	a := &protocol.Action{
		Type: protocol.ActionDrag,
		From: &protocol.Point{X: 10, Y: 20},
		To:   &protocol.Point{X: 30, Y: 40},
	}
	if err := m.Execute(context.Background(), a); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := m.Snapshot()[0]; !reflect.DeepEqual(got, a) {
		t.Errorf("got=%+v want=%+v", got, a)
	}
}

func TestButtonNumDefaultsToLeft(t *testing.T) {
	cases := []struct {
		in   protocol.MouseButton
		want string
	}{
		{"", "1"},
		{protocol.MouseLeft, "1"},
		{protocol.MouseMiddle, "2"},
		{protocol.MouseRight, "3"},
	}
	for _, c := range cases {
		if got := buttonNum(c.in); got != c.want {
			t.Errorf("buttonNum(%q): got=%s want=%s", c.in, got, c.want)
		}
	}
}
