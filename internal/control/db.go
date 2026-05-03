package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Store wraps a SQLite database holding session + API-key + audit state.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the database at the given path and applies
// the schema migrations.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite likes single-writer
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

func initSchema(db *sql.DB) error {
	const schema = `
	CREATE TABLE IF NOT EXISTS sessions (
		id           TEXT PRIMARY KEY,
		status       TEXT NOT NULL,
		template     TEXT NOT NULL,
		container_id TEXT,
		host_port    INTEGER,
		created_at   INTEGER NOT NULL,
		expires_at   INTEGER NOT NULL,
		metadata     TEXT,
		open_url     TEXT,
		last_error   TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

	CREATE TABLE IF NOT EXISTS api_keys (
		id          TEXT PRIMARY KEY,
		key_hash    TEXT NOT NULL UNIQUE,
		name        TEXT,
		created_at  INTEGER NOT NULL,
		revoked_at  INTEGER
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT,
		api_key_id  TEXT,
		action      TEXT,
		ts          INTEGER NOT NULL,
		meta        TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_log(session_id);
	`
	_, err := db.Exec(schema)
	return err
}

// SessionRow holds the persisted shape of a session.
type SessionRow struct {
	ID          string
	Status      protocol.SessionStatus
	Template    string
	ContainerID string
	HostPort    int
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Metadata    map[string]string
	OpenURL     string
	LastError   string
}

// ToProtocol converts a SessionRow to the wire-shape Session.
func (r *SessionRow) ToProtocol() protocol.Session {
	return protocol.Session{
		ID:        r.ID,
		Status:    r.Status,
		Template:  r.Template,
		CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		Metadata:  r.Metadata,
		OpenURL:   r.OpenURL,
		LastError: r.LastError,
	}
}

// CreateSession inserts a new session row.
func (s *Store) CreateSession(ctx context.Context, r *SessionRow) error {
	meta, _ := json.Marshal(r.Metadata)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, status, template, container_id, host_port,
			created_at, expires_at, metadata, open_url, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, string(r.Status), r.Template, r.ContainerID, r.HostPort,
		r.CreatedAt.Unix(), r.ExpiresAt.Unix(), string(meta), r.OpenURL, r.LastError)
	return err
}

// GetSession returns a session by id, or sql.ErrNoRows.
func (s *Store) GetSession(ctx context.Context, id string) (*SessionRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, template, container_id, host_port,
			created_at, expires_at, metadata, open_url, last_error
		FROM sessions WHERE id = ?
	`, id)
	return scanSession(row)
}

// ListActiveSessions returns all sessions whose status is not "ended" or "failed".
func (s *Store) ListActiveSessions(ctx context.Context) ([]*SessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, template, container_id, host_port,
			created_at, expires_at, metadata, open_url, last_error
		FROM sessions WHERE status IN ('pending','ready')
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*SessionRow
	for rows.Next() {
		r, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListExpiredSessions returns ready/pending sessions past their TTL.
func (s *Store) ListExpiredSessions(ctx context.Context, now time.Time) ([]*SessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, template, container_id, host_port,
			created_at, expires_at, metadata, open_url, last_error
		FROM sessions
		WHERE status IN ('pending','ready') AND expires_at < ?
	`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionRow
	for rows.Next() {
		r, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateSessionStatus mutates the status (and optionally LastError).
func (s *Store) UpdateSessionStatus(ctx context.Context, id string, status protocol.SessionStatus, lastErr string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status = ?, last_error = ? WHERE id = ?`,
		string(status), lastErr, id)
	return err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(r rowScanner) (*SessionRow, error) {
	var sr SessionRow
	var status string
	var createdAt, expiresAt int64
	var metaJSON, openURL, lastErr, containerID sql.NullString
	var hostPort sql.NullInt64
	if err := r.Scan(&sr.ID, &status, &sr.Template, &containerID, &hostPort,
		&createdAt, &expiresAt, &metaJSON, &openURL, &lastErr); err != nil {
		return nil, err
	}
	sr.Status = protocol.SessionStatus(status)
	sr.ContainerID = containerID.String
	sr.HostPort = int(hostPort.Int64)
	sr.CreatedAt = time.Unix(createdAt, 0)
	sr.ExpiresAt = time.Unix(expiresAt, 0)
	sr.OpenURL = openURL.String
	sr.LastError = lastErr.String
	if metaJSON.Valid && metaJSON.String != "" {
		_ = json.Unmarshal([]byte(metaJSON.String), &sr.Metadata)
	}
	return &sr, nil
}

// AuditAction inserts an audit log row.
func (s *Store) AuditAction(ctx context.Context, sessionID, apiKeyID, action, meta string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (session_id, api_key_id, action, ts, meta)
		VALUES (?, ?, ?, ?, ?)
	`, sessionID, apiKeyID, action, time.Now().Unix(), meta)
	return err
}

// CreateAPIKey stores a hashed API key. Returns the id; caller knows the
// plaintext to give to the human.
func (s *Store) CreateAPIKey(ctx context.Context, id, hash, name string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, key_hash, name, created_at)
		VALUES (?, ?, ?, ?)
	`, id, hash, name, time.Now().Unix())
	return err
}

// LookupAPIKey returns the (id, name) of the key matching hash if active.
func (s *Store) LookupAPIKey(ctx context.Context, hash string) (string, string, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(name,'') FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL`, hash)
	var id, name string
	if err := row.Scan(&id, &name); err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return id, name, true, nil
}
