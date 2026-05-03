package control

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Config configures the control plane HTTP server.
type Config struct {
	DBPath      string
	Image       string // sandbox image, e.g. "qdesk/ubuntu-chrome:dev"
	DevKey      string // bypass key for local dev, "" disables
	DefaultTTL  time.Duration
	MaxTTL      time.Duration
	GCInterval  time.Duration
}

// Server is the control plane.
type Server struct {
	cfg       Config
	store     *Store
	rt        Runtime
	mu        sync.Mutex
	inflight  map[string]struct{} // sessions currently being created
}

// NewServer wires the dependencies.
func NewServer(cfg Config, store *Store, rt Runtime) *Server {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 10 * time.Minute
	}
	if cfg.MaxTTL == 0 {
		cfg.MaxTTL = 60 * time.Minute
	}
	if cfg.GCInterval == 0 {
		cfg.GCInterval = 30 * time.Second
	}
	return &Server{
		cfg:      cfg,
		store:    store,
		rt:       rt,
		inflight: make(map[string]struct{}),
	}
}

// Handler returns the http.Handler with all routes wired up.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("POST /v1/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	mux.HandleFunc("GET /v1/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("GET /v1/sessions/{id}/screenshot", s.handleProxy)
	mux.HandleFunc("POST /v1/sessions/{id}/actions", s.handleProxy)

	// Wrap with auth middleware (but /v1/health bypasses inside the middleware).
	return AuthMiddleware(s.store, s.cfg.DevKey)(mux)
}

// StartGC kicks off the background reaper. Returns a stop func.
func (s *Server) StartGC(ctx context.Context) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(s.cfg.GCInterval)
		defer t.Stop()
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				s.reapExpired(ctx)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func (s *Server) reapExpired(ctx context.Context) {
	rows, err := s.store.ListExpiredSessions(ctx, time.Now())
	if err != nil {
		slog.Error("gc list expired", "err", err)
		return
	}
	for _, r := range rows {
		slog.Info("gc reap", "session", r.ID, "container", r.ContainerID)
		_ = s.rt.Stop(ctx, r.ContainerID)
		_ = s.store.UpdateSessionStatus(ctx, r.ID, protocol.SessionEnded, "ttl expired")
	}
}

// ----- handlers -----

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req protocol.CreateSessionRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Template == "" {
		req.Template = "ubuntu-chrome"
	}
	if req.Template != "ubuntu-chrome" {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown template %q (only 'ubuntu-chrome' supported in Phase 0)", req.Template))
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl == 0 {
		ttl = s.cfg.DefaultTTL
	}
	if ttl > s.cfg.MaxTTL {
		ttl = s.cfg.MaxTTL
	}

	sessionID := "sbx_" + randID(8)
	now := time.Now()
	row := &SessionRow{
		ID:        sessionID,
		Status:    protocol.SessionPending,
		Template:  req.Template,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Metadata:  req.Metadata,
		OpenURL:   req.OpenURL,
	}

	// Reserve the row with status=pending immediately so a concurrent
	// duplicate can't double-spawn.
	if err := s.store.CreateSession(r.Context(), row); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store: "+err.Error())
		return
	}
	_ = s.store.AuditAction(r.Context(), sessionID, APIKeyIDFromContext(r.Context()),
		"create_session", req.Template)

	// Synchronous start so the caller gets back an endpoint that already
	// passed /health. Bound by a 30s timeout.
	startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res := req.Resolution
	if res == [2]int{0, 0} {
		res = [2]int{1920, 1080}
	}
	handle, err := s.rt.Start(startCtx, StartOpts{
		Image:      s.cfg.Image,
		Name:       "qdesk-" + sessionID,
		Resolution: res,
		OpenURL:    req.OpenURL,
	})
	if err != nil {
		_ = s.store.UpdateSessionStatus(r.Context(), sessionID, protocol.SessionFailed, err.Error())
		writeJSONError(w, http.StatusInternalServerError, "start: "+err.Error())
		return
	}
	row.ContainerID = handle.ContainerID
	row.HostPort = handle.HostPort
	// Persist the host port + container id (small follow-up update).
	if _, err := s.store.db.ExecContext(r.Context(),
		`UPDATE sessions SET container_id = ?, host_port = ? WHERE id = ?`,
		row.ContainerID, row.HostPort, sessionID); err != nil {
		slog.Warn("update session metadata", "err", err)
	}

	if err := s.rt.WaitReady(startCtx, handle.HostPort); err != nil {
		_ = s.rt.Stop(context.Background(), handle.ContainerID)
		_ = s.store.UpdateSessionStatus(r.Context(), sessionID, protocol.SessionFailed, "wait ready: "+err.Error())
		writeJSONError(w, http.StatusInternalServerError, "wait ready: "+err.Error())
		return
	}
	if req.OpenURL != "" {
		if err := s.rt.OpenURL(startCtx, handle.ContainerID, req.OpenURL); err != nil {
			slog.Warn("open url", "session", sessionID, "err", err)
		}
	}
	row.Status = protocol.SessionReady
	if err := s.store.UpdateSessionStatus(r.Context(), sessionID, row.Status, ""); err != nil {
		slog.Warn("update status", "err", err)
	}

	writeJSON(w, http.StatusCreated, row.ToProtocol())
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListActiveSessions(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := protocol.ListSessionsResponse{Sessions: make([]protocol.Session, 0, len(rows))}
	for _, row := range rows {
		out.Sessions = append(out.Sessions, row.ToProtocol())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row.ToProtocol())
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row.ContainerID != "" {
		if err := s.rt.Stop(r.Context(), row.ContainerID); err != nil {
			slog.Warn("stop container", "session", id, "err", err)
		}
	}
	if err := s.store.UpdateSessionStatus(r.Context(), id, protocol.SessionEnded, ""); err != nil {
		slog.Warn("update status", "err", err)
	}
	_ = s.store.AuditAction(r.Context(), id, APIKeyIDFromContext(r.Context()), "delete_session", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleProxy forwards /screenshot and /actions to the right agentd.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row.Status != protocol.SessionReady {
		writeJSONError(w, http.StatusConflict,
			fmt.Sprintf("session status is %q, not ready", row.Status))
		return
	}
	// Strip "/v1/sessions/{id}" so the proxied request hits "/screenshot" or "/actions".
	prefix := "/v1/sessions/" + id
	proxyToAgentd(row.HostPort, prefix).ServeHTTP(w, r)
	_ = s.store.AuditAction(r.Context(), id, APIKeyIDFromContext(r.Context()),
		strings.ToLower(r.Method)+":"+strings.TrimPrefix(r.URL.Path, prefix), "")
}

// ----- helpers -----

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

