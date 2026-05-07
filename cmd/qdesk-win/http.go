//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/internal/winserver"
)

// httpConfig is what main.go feeds into runHTTP.
type httpConfig struct {
	Listen               string
	APIKey               string
	TrustedCIDR          string // comma-separated CIDR list; empty = no filter
	TrustTailscaleHeader bool
}

// runHTTP serves the MCP JSON-RPC dispatch over HTTP for remote clients.
//
// Endpoints:
//
//	GET  /health  — no auth, returns {"ok": true}. Liveness/readiness probe.
//	POST /mcp     — bearer auth required; body is a single JSON-RPC request,
//	                response is the corresponding JSON-RPC response.
//
// One request per HTTP roundtrip; response framing follows Accept:
// `text/event-stream` → SSE single frame; otherwise application/json.
//
// Hardening notes (read before exposing publicly):
//   - No TLS. Put a reverse proxy (caddy / nginx) in front for TLS + ACL.
//   - Bearer key is plaintext over the wire; rotate by restart with a new key.
//   - No rate limiting. If you publish, add one upstream.
func runHTTP(ctx context.Context, srv *winserver.MCPServer, cfg httpConfig) error {
	if cfg.APIKey == "" {
		return errors.New("HTTP mode refuses to start with an empty --api-key (or QDESK_WIN_API_KEY env)")
	}
	cidrs, err := parseCIDRs(cfg.TrustedCIDR)
	if err != nil {
		return fmt.Errorf("--trusted-cidr: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`+"\n")
	})
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req winserver.RPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON-RPC: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := srv.Handle(r.Context(), &req)
		writeMCPResponse(w, r, resp)
	})

	// Compose middleware: CIDR filter (outermost) → bearer auth →
	// optional Tailscale identity logging → handler.
	var chained http.Handler = mcpHandler
	if cfg.TrustTailscaleHeader {
		chained = logTailscaleIdentity(chained)
	}
	chained = bearerAuth(cfg.APIKey, chained)
	if len(cidrs) > 0 {
		chained = cidrAllowlist(cidrs, chained)
	}
	mux.Handle("/mcp", chained)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
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

// writeMCPResponse picks the response framing based on what the client
// said it accepts. MCP's Streamable HTTP spec lets servers respond with
// either application/json (single response) or text/event-stream (one
// or more SSE events). We never stream — emit a single `data:` frame
// when SSE was requested, otherwise straight JSON.
func writeMCPResponse(w http.ResponseWriter, r *http.Request, resp *winserver.RPCResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if clientWantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(body)
		_, _ = w.Write([]byte("\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func clientWantsSSE(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(part)
		if i := strings.IndexByte(mt, ';'); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
		if strings.EqualFold(mt, "text/event-stream") {
			return true
		}
	}
	return false
}

// bearerAuth gates the wrapped handler on "Authorization: Bearer <key>"
// equal to the configured key.
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

// constantTimeEqual compares two strings in time independent of which
// char differs. Length difference still leaks but is unavoidable
// without padding.
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

// parseCIDRs splits a comma-separated CIDR list and validates each entry.
func parseCIDRs(csv string) ([]*net.IPNet, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []*net.IPNet
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// cidrAllowlist rejects requests whose remote IP isn't in any of the
// supplied networks. Reads X-Forwarded-For ONLY if the immediate peer
// is loopback so a leaked bearer key is still useless from the open
// internet when --trusted-cidr is set.
func cidrAllowlist(allowed []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteClientIP(r)
		if ip == nil {
			http.Error(w, "cannot determine client IP", http.StatusForbidden)
			return
		}
		for _, n := range allowed {
			if n.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "client IP not in trusted CIDR", http.StatusForbidden)
	})
}

func remoteClientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return nil
	}
	if peer.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				xff = xff[:idx]
			}
			if forwarded := net.ParseIP(strings.TrimSpace(xff)); forwarded != nil {
				return forwarded
			}
		}
	}
	return peer
}

// logTailscaleIdentity records who is calling when the request flows
// through `tailscale serve`. Tailscale injects Tailscale-User-Login
// and Tailscale-User-Name headers; trusting them is opt-in via
// --trust-tailscale-headers because anyone reaching qdesk-win
// directly (no Tailscale frontend) can otherwise spoof.
func logTailscaleIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := r.Header.Get("Tailscale-User-Login")
		name := r.Header.Get("Tailscale-User-Name")
		if login != "" || name != "" {
			logf("tailscale request from login=%q name=%q remote=%s path=%s",
				login, name, r.RemoteAddr, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
