package macproto

import (
	"encoding/json"
	"testing"
)

func TestHealthResponseRoundTrip(t *testing.T) {
	in := HealthResponse{
		OK:                     true,
		ScreenRecordingGranted: true,
		AccessibilityGranted:   false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out HealthResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", out, in)
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	req := Request{ID: 7, Method: MethodHealth, Params: json.RawMessage(`{}`)}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != req.ID || out.Method != req.Method {
		t.Errorf("envelope mismatch: got=%+v want=%+v", out, req)
	}
}

func TestClipboardPasteRequestRoundTrip(t *testing.T) {
	in := ClipboardPasteRequest{Text: "你好 hello"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ClipboardPasteRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", out, in)
	}
}

func TestMethodClipboardPasteString(t *testing.T) {
	if MethodClipboardPaste != "clipboardPaste" {
		t.Errorf("MethodClipboardPaste must be %q so the Swift helper can match it", "clipboardPaste")
	}
}
