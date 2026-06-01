package iamguard

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/idunaauth"
)

// helpers re-implemented here to avoid cross-package test imports.

func buildTestJWT(t *testing.T, key *ecdsa.PrivateKey, kid string, claims idunaauth.Claims) string {
	t.Helper()
	header := map[string]string{"alg": "ES256", "kid": kid, "typ": "JWT"}
	hj, _ := json.Marshal(header)
	cj, _ := json.Marshal(claims)
	hEnc := base64.RawURLEncoding.EncodeToString(hj)
	pEnc := base64.RawURLEncoding.EncodeToString(cj)
	msg := hEnc + "." + pEnc
	digest := sha256.Sum256([]byte(msg))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rb := r.Bytes()
	sb := s.Bytes()
	sig := make([]byte, 64)
	copy(sig[32-len(rb):32], rb)
	copy(sig[64-len(sb):64], sb)
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func buildJWKSServer(t *testing.T, key *ecdsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	pub := key.PublicKey
	xb := pub.X.Bytes()
	yb := pub.Y.Bytes()
	xp := make([]byte, 32)
	yp := make([]byte, 32)
	copy(xp[32-len(xb):], xb)
	copy(yp[32-len(yb):], yb)
	type jwk struct {
		Kty, Crv, Kid, X, Y string
	}
	payload := map[string]interface{}{"keys": []jwk{{
		Kty: "EC", Crv: "P-256", Kid: kid,
		X: base64.RawURLEncoding.EncodeToString(xp),
		Y: base64.RawURLEncoding.EncodeToString(yp),
	}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
}

func TestGuard_RequirePermission_NoOp(t *testing.T) {
	g := &Guard{}
	called := false
	handler := g.RequirePermission("fatbaby.read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Error("no-op guard should pass through to next handler")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestGuard_RequirePermission_MissingHeader(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := buildJWKSServer(t, key, "k1")
	defer srv.Close()

	g, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := g.RequirePermission("fatbaby.read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for missing header", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "UNAUTHORIZED") {
		t.Errorf("body missing UNAUTHORIZED code: %s", rr.Body.String())
	}
}

func TestGuard_RequirePermission_ValidToken_HasPerm(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := buildJWKSServer(t, key, "k2")
	defer srv.Close()

	g, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token := buildTestJWT(t, key, "k2", idunaauth.Claims{
		Sub:         "user-1",
		Permissions: []string{"fatbaby.read", "signal.write"},
		Exp:         time.Now().Add(time.Hour).Unix(),
	})

	handler := g.RequirePermission("fatbaby.read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("claims not injected into context")
		}
		if claims.Sub != "user-1" {
			t.Errorf("sub = %q, want user-1", claims.Sub)
		}
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for valid token with permission", rr.Code)
	}
}

func TestGuard_RequirePermission_ValidToken_MissingPerm(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := buildJWKSServer(t, key, "k3")
	defer srv.Close()

	g, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token := buildTestJWT(t, key, "k3", idunaauth.Claims{
		Sub:         "user-2",
		Permissions: []string{"fatbaby.read"},
		Exp:         time.Now().Add(time.Hour).Unix(),
	})

	handler := g.RequirePermission("governance.admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/tick", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for insufficient permissions", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "UNAUTHORIZED_CAPABILITY") {
		t.Errorf("body missing UNAUTHORIZED_CAPABILITY: %s", rr.Body.String())
	}
}

func TestGuard_RequirePermission_InvalidToken(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := buildJWKSServer(t, key, "k4")
	defer srv.Close()

	g, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := g.RequirePermission("fatbaby.read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.jwt")
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for invalid token", rr.Code)
	}
}

func TestGuard_IsActive(t *testing.T) {
	if (&Guard{}).IsActive() {
		t.Error("empty guard should not be active")
	}
}

func TestClaimsFromContext_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, ok := ClaimsFromContext(req.Context())
	if ok {
		t.Error("expected ok=false for context without claims")
	}
}

// Ensure big.Int is available (used indirectly via idunaauth).
var _ = new(big.Int)
