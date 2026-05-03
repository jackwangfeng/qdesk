package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ctxKey is a private type used to scope context values to this package.
type ctxKey int

const (
	ctxKeyAPIKeyID ctxKey = iota
)

// APIKeyIDFromContext returns the api-key id stored by the auth middleware.
func APIKeyIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAPIKeyID).(string)
	return v
}

// HashAPIKey returns the SHA-256 hex digest stored in the database.
// Plaintext keys never touch the DB.
func HashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// AuthMiddleware enforces "Authorization: Bearer <key>" on every request.
// The middleware also accepts a single dev-mode shared key bypass via
// devKey for ergonomic local usage.
func AuthMiddleware(store *Store, devKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always allow /v1/health without auth.
			if r.URL.Path == "/v1/health" {
				next.ServeHTTP(w, r)
				return
			}
			tok := bearerToken(r)
			if tok == "" {
				writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			if devKey != "" && tok == devKey {
				ctx := context.WithValue(r.Context(), ctxKeyAPIKeyID, "dev")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			id, _, ok, err := store.LookupAPIKey(r.Context(), HashAPIKey(tok))
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "auth lookup: "+err.Error())
				return
			}
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyAPIKeyID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}
