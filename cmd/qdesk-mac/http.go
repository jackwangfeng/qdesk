package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/macserver"
)

// runHTTP serves the MCP JSON-RPC dispatch over HTTP for remote clients.
//
// Endpoints:
//
//	GET  /health  — no auth, returns {"ok": true}. Liveness/readiness probe.
//	POST /mcp     — bearer auth required; body is a single JSON-RPC request,
//	                response is the corresponding JSON-RPC response.
//
// The transport is intentionally minimal: one request per HTTP roundtrip,
// no SSE, no session state. This matches the MCP "Streamable HTTP" shape
// for non-streaming tools. Each request is independent; clients should
// send `initialize` once, then any number of `tools/call`s.
//
// Concurrency: the underlying helper RPC is single-threaded (Supervisor
// serialises). Multiple HTTP clients still work — they just queue.
//
// Hardening notes (read before exposing publicly):
//   - No TLS. Put a reverse proxy (caddy / nginx) in front for TLS + ACL.
//   - Bearer key is plaintext over the wire; rotate by restart with a new key.
//   - No rate limiting. If you publish, add one upstream.
//   - Mac must be unlocked AND logged in — see docs/superpowers/specs/.
func runHTTP(ctx context.Context, srv *macserver.MCPServer, listen, apiKey string, withCaffeinate bool) error {
	if apiKey == "" {
		return errors.New("HTTP mode refuses to start with an empty --api-key (or QDESK_MAC_API_KEY env)")
	}

	if withCaffeinate {
		stop := startCaffeinate(ctx)
		defer stop()
	}

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
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req macserver.RPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := srv.Handle(r.Context(), &req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})))

	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http listen: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	}
}

// bearerAuth gates the wrapped handler on "Authorization: Bearer <key>"
// equal to the configured key. The key comparison is constant-time-ish; it
// is not cryptographic-grade, but it is good enough for a single shared
// secret over a reverse proxy.
func bearerAuth(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		got := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if !constantTimeEqual(got, key) {
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// constantTimeEqual compares two strings in time independent of which char
// differs. Length difference still leaks but is unavoidable without padding.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// startCaffeinate launches `caffeinate -di` so the Mac doesn't sleep or
// dim its display while we're serving. macOS lock screen blocks all
// CGEvent posts (Secure Event Input), so a slept/locked Mac would silently
// fail every tool call. The returned function kills the child.
func startCaffeinate(ctx context.Context) func() {
	cmd := exec.CommandContext(ctx, "caffeinate", "-di")
	if err := cmd.Start(); err != nil {
		logf("caffeinate failed to start: %v (continuing without it)", err)
		return func() {}
	}
	logf("caffeinate started; Mac will not sleep while qdesk-mac is running")
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
