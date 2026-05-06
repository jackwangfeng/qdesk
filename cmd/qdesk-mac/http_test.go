package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeffwang/qdesk/internal/macserver"
)

// Helper: build the same handler runHTTP installs, but without spawning
// the listener / caffeinate. Lets us httptest.NewServer it.
func newTestHandler(t *testing.T, apiKey string) http.Handler {
	t.Helper()
	fake := macserver.NewFakeSupervisor()
	srv := macserver.NewMCPServer(fake)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`+"\n")
	})
	mux.Handle("/mcp", bearerAuth(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		var req macserver.RPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := srv.Handle(r.Context(), &req)
		writeMCPResponse(w, r, resp)
	})))
	return mux
}

func TestMCPSSEResponseWhenAccepted(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type: got=%q want=text/event-stream", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "data: ") {
		t.Errorf("SSE frame missing 'data: ' prefix: %s", body)
	}
	if !strings.HasSuffix(string(body), "\n\n") {
		t.Errorf("SSE frame missing trailing blank line: %q", body)
	}
	if !strings.Contains(string(body), "wechat.screenshot") {
		t.Errorf("SSE frame missing tool list: %s", body)
	}
}

func TestMCPJSONResponseWhenAcceptIsJSON(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type: got=%q want=application/json", got)
	}
}

func TestMCPSSEPreferenceWhenAcceptListsBoth(t *testing.T) {
	// Streamable HTTP spec: clients often send "Accept: application/json,
	// text/event-stream". We pick SSE if it's listed at all (permissive).
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type: got=%q want=text/event-stream", got)
	}
}

func TestClientWantsSSE(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"application/json", false},
		{"text/event-stream", true},
		{"text/event-stream;q=0.9", true},
		{"application/json, text/event-stream", true},
		{"text/html, application/xhtml+xml", false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest("POST", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		if got := clientWantsSSE(r); got != c.want {
			t.Errorf("clientWantsSSE(%q) = %v, want %v", c.accept, got, c.want)
		}
	}
}

func TestHealthEndpointNoAuth(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status: got=%d want=200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("body: got=%s", string(body))
	}
}

func TestMCPRejectsMissingBearer(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status: got=%d want=401", resp.StatusCode)
	}
}

func TestMCPRejectsWrongBearer(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	req.Header.Set("Authorization", "Bearer wrong-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status: got=%d want=401", resp.StatusCode)
	}
}

func TestMCPInitializeWithBearer(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status: got=%d want=200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"name":"qdesk-mac"`) {
		t.Errorf("body missing serverInfo: %s", string(body))
	}
}

func TestMCPToolsListWithBearer(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"wechat.screenshot", "wechat.open_chat", "wechat.type"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %q in body: %s", want, body)
		}
	}
}

func TestMCPRejectsGet(t *testing.T) {
	ts := httptest.NewServer(newTestHandler(t, "secret"))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got=%d want=405", resp.StatusCode)
	}
}

func TestRunHTTPRequiresAPIKey(t *testing.T) {
	// runHTTP refuses to start with empty key; verify the explicit error.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := macserver.NewFakeSupervisor()
	srv := macserver.NewMCPServer(fake)
	err := runHTTP(ctx, srv, "127.0.0.1:0", "", false)
	if err == nil {
		t.Fatal("expected error when api-key is empty")
	}
	if !strings.Contains(err.Error(), "api-key") {
		t.Errorf("error doesn't mention api-key: %v", err)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"a", "a", true},
		{"abc", "abc", true},
		{"a", "b", false},
		{"abc", "abd", false},
		{"abc", "ab", false},
		{"ab", "abc", false},
	}
	for _, c := range cases {
		if got := constantTimeEqual(c.a, c.b); got != c.want {
			t.Errorf("constantTimeEqual(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
