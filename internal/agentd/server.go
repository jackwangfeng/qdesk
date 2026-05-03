package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Version is reported by /health. Override at link time with -ldflags.
var Version = "0.1.0"

// AppState is the dependency container shared by all handlers.
type AppState struct {
	Screen ScreenSource
	Input  InputDriver
}

// NewRouter wires the AppState into an http.Handler with all routes.
func NewRouter(state *AppState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", state.handleHealth)
	mux.HandleFunc("GET /screenshot", state.handleScreenshot)
	mux.HandleFunc("POST /actions", state.handleActions)
	return mux
}

func (s *AppState) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, protocol.HealthResponse{
		Status:  "ok",
		Version: Version,
	})
}

func (s *AppState) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	bytes, err := s.Screen.Capture(r.Context())
	if err != nil {
		slog.Error("screenshot failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes)
}

func (s *AppState) handleActions(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var a protocol.Action
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if a.Type == "" {
		writeJSONError(w, http.StatusBadRequest, "action 'type' is required")
		return
	}
	if err := s.Input.Execute(r.Context(), &a); err != nil {
		var bad InvalidAction
		if errors.As(err, &bad) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writeJSONError(w, 499, err.Error()) // client closed request
			return
		}
		slog.Error("action failed", "type", a.Type, "err", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, protocol.ActionResult{
		OK:            true,
		ScreenChanged: true,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
