// Package iamguard provides the http.Handler middleware layer that enforces
// IDUNA-issued JWT access control across PRRJECT-FATBABY edge endpoints.
//
// Architecture: this package wraps internal/idunaauth (JWKS fetching + ES256
// verification) with the HTTP middleware shape described in
// docs/iam-integration-spec.md §2.1. Zero external dependencies.
//
// Usage:
//
//	// Build the guard once at startup (performs JWKS pre-fetch).
//	guard, err := iamguard.New(os.Getenv("IDUNA_JWKS_URL"))
//	if err != nil { log.Fatal(err) }
//
//	mux.Handle("/events", guard.RequirePermission("fatbaby.read")(sseHandler))
//	mux.Handle("/chat",   guard.RequirePermission("fatbaby.operator")(chatHandler))
//
// If IDUNA_JWKS_URL is not set, New returns a no-op guard that passes all
// requests through. This preserves backward-compatibility with unauthenticated
// local deployments.
package iamguard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/example/prrject-fatbaby/internal/idunaauth"
)

// contextKey is the unexported type for claims stored in request context.
type contextKey struct{}

// Guard enforces IDUNA JWT permissions on http.Handler chains.
// A nil or no-op guard passes all requests through without verification.
type Guard struct {
	verifier *idunaauth.Verifier // nil → no-op (unauthenticated mode)
}

// New constructs a Guard backed by IDUNA's JWKS endpoint at jwksBaseURL.
// Pass an empty string (or zero value) to get a no-op guard — useful when
// IDUNA is not configured (local / CI environments).
//
// The guard performs an initial JWKS fetch to validate connectivity; if that
// fails and jwksBaseURL is non-empty, the error is returned so callers can
// choose to fatal or warn.
func New(jwksBaseURL string) (*Guard, error) {
	if jwksBaseURL == "" {
		return &Guard{}, nil
	}
	v, err := idunaauth.NewVerifier(jwksBaseURL)
	if err != nil {
		return nil, err
	}
	return &Guard{verifier: v}, nil
}

// NewFromEnv constructs a Guard by reading IDUNA_JWKS_URL from the environment.
// Falls back to config/iam_config.json if the env var is absent.
// Returns a no-op guard (no error) when no configuration is found.
func NewFromEnv() (*Guard, error) {
	url := os.Getenv("IDUNA_JWKS_URL")
	if url == "" {
		url = loadJWKSURLFromConfig()
	}
	return New(url)
}

// loadJWKSURLFromConfig reads iduna_jwks_url from config/iam_config.json.
// Returns empty string if the file is absent or malformed.
func loadJWKSURLFromConfig() string {
	b, err := os.ReadFile("config/iam_config.json")
	if err != nil {
		return ""
	}
	var cfg struct {
		IDUNAJWKSUrl string `json:"iduna_jwks_url"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ""
	}
	return cfg.IDUNAJWKSUrl
}

// IsActive returns true when the guard has a configured verifier (i.e. is NOT in
// no-op mode). Useful for startup logging.
func (g *Guard) IsActive() bool {
	return g != nil && g.verifier != nil
}

// RequirePermission returns an http.Handler middleware that verifies the
// incoming Bearer JWT and checks that it carries requiredPermission.
//
// On failure it returns:
//   - 401 Unauthorized — missing or unparseable Authorization header
//   - 403 Forbidden    — valid JWT but insufficient permissions
//
// On success it injects the verified Claims into the request context
// (retrieve with ClaimsFromContext) and calls next.
//
// When the guard is in no-op mode (IDUNA not configured), the middleware is
// transparent and passes every request through.
func (g *Guard) RequirePermission(requiredPermission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !g.IsActive() {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing bearer token signature")
				return
			}
			tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

			claims, err := g.verifier.Verify(tokenStr)
			if err != nil {
				writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Cryptographic verification failure")
				return
			}

			if !claims.HasPermission(requiredPermission) {
				writeJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED_CAPABILITY", "Insufficient domain clearance")
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts the verified Claims injected by RequirePermission.
// Returns the zero-value Claims and false if no claims are present.
func ClaimsFromContext(ctx context.Context) (idunaauth.Claims, bool) {
	v, ok := ctx.Value(contextKey{}).(idunaauth.Claims)
	return v, ok
}

// VerifyTokenSub verifies the JWT and returns the sub claim.
// Returns empty string and an error when the guard is in no-op mode or verification fails.
func (g *Guard) VerifyTokenSub(token string) (string, error) {
	if !g.IsActive() {
		return "", fmt.Errorf("guard: no-op mode")
	}
	claims, err := g.verifier.Verify(token)
	if err != nil {
		return "", err
	}
	return claims.Sub, nil
}

// VerifyTokenPermissions verifies the JWT and returns the sub claim and the permissions slice.
// Returns empty values and an error when the guard is in no-op mode or verification fails.
func (g *Guard) VerifyTokenPermissions(token string) (string, []string, error) {
	if !g.IsActive() {
		return "", nil, fmt.Errorf("guard: no-op mode")
	}
	claims, err := g.verifier.Verify(token)
	if err != nil {
		return "", nil, err
	}
	return claims.Sub, claims.Permissions, nil
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
