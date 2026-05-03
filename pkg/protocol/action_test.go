package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestClickActionRoundTrips(t *testing.T) {
	a := Action{Type: ActionClick, X: 10, Y: 20, Button: MouseLeft}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"click","x":10,"y":20,"button":"left"}`
	if string(got) != want {
		t.Errorf("marshal mismatch:\n  got:  %s\n  want: %s", got, want)
	}

	var back Action
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, a) {
		t.Errorf("round-trip:\n  got:  %+v\n  want: %+v", back, a)
	}
}

func TestClickButtonOmittedWhenLeft(t *testing.T) {
	// When Button is empty (default), it must be omitted on the wire so the
	// daemon's "default to left" rule applies.
	a := Action{Type: ActionClick, X: 5, Y: 6}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"click","x":5,"y":6}`
	if string(got) != want {
		t.Errorf("marshal mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestTypeActionRoundTrips(t *testing.T) {
	a := Action{Type: ActionType_, Text: "hello"}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"type","text":"hello"}`
	if string(got) != want {
		t.Errorf("marshal mismatch: got=%s want=%s", got, want)
	}

	var back Action
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, a) {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", back, a)
	}
}

func TestKeyActionRoundTrips(t *testing.T) {
	a := Action{Type: ActionKey, Keys: []string{"ctrl", "s"}}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Action
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, a) {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", back, a)
	}
}

func TestDragActionRoundTrips(t *testing.T) {
	a := Action{
		Type: ActionDrag,
		From: &Point{X: 1, Y: 2},
		To:   &Point{X: 3, Y: 4},
	}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"drag","from":{"x":1,"y":2},"to":{"x":3,"y":4}}`
	if string(got) != want {
		t.Errorf("marshal mismatch:\n  got:  %s\n  want: %s", got, want)
	}

	var back Action
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, a) {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", back, a)
	}
}

func TestScrollActionRoundTrips(t *testing.T) {
	a := Action{Type: ActionScroll, X: 100, Y: 200, DX: 0, DY: -3}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Action
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, a) {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", back, a)
	}
}

func TestWaitActionRoundTrips(t *testing.T) {
	a := Action{Type: ActionWait, MS: 100}
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"wait","ms":100}`
	if string(got) != want {
		t.Errorf("marshal mismatch: got=%s want=%s", got, want)
	}
}
