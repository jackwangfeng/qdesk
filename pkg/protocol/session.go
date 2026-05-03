package protocol

import "time"

// SessionStatus is the lifecycle state of a sandbox session.
type SessionStatus string

const (
	SessionPending SessionStatus = "pending"  // accepted, container starting
	SessionReady   SessionStatus = "ready"    // /health passed, ready to drive
	SessionEnded   SessionStatus = "ended"    // explicit DELETE or TTL expiry
	SessionFailed  SessionStatus = "failed"   // start error or unrecoverable runtime failure
)

// CreateSessionRequest is the body of POST /v1/sessions.
type CreateSessionRequest struct {
	// Template identifies the sandbox image. Phase 0 only supports "ubuntu-chrome".
	Template string `json:"template,omitempty"`
	// TTLSeconds bounds session lifetime. Defaults applied server-side.
	TTLSeconds int `json:"ttl_seconds,omitempty"`
	// Resolution overrides the default 1920x1080.
	Resolution [2]int `json:"resolution,omitempty"`
	// Metadata is free-form, opaque to the control plane (audit/labels).
	Metadata map[string]string `json:"metadata,omitempty"`
	// OpenURL is a convenience: if set, control plane launches Chromium pointing at it.
	OpenURL string `json:"open_url,omitempty"`
}

// Session is the canonical representation returned by the control plane.
type Session struct {
	ID         string            `json:"session_id"`
	Status     SessionStatus     `json:"status"`
	Template   string            `json:"template"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	OpenURL    string            `json:"open_url,omitempty"`
	// LastError is non-empty when Status == SessionFailed.
	LastError  string            `json:"last_error,omitempty"`
}

// ListSessionsResponse is the body of GET /v1/sessions.
type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
}
