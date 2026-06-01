package idunaauth

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
)

// buildTestJWT constructs a minimal ES256 JWT signed with the given key.
func buildTestJWT(t *testing.T, key *ecdsa.PrivateKey, kid string, claims Claims) string {
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
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	// Pad to 32 bytes each.
	sig := make([]byte, 64)
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// buildJWKSServer starts a test HTTP server serving a JWKS for the given key.
func buildJWKSServer(t *testing.T, key *ecdsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	pub := key.PublicKey
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	xPad := make([]byte, 32)
	yPad := make([]byte, 32)
	copy(xPad[32-len(xBytes):], xBytes)
	copy(yPad[32-len(yBytes):], yBytes)
	jwks := jwksResponse{Keys: []jwk{{
		Kty: "EC",
		Crv: "P-256",
		Kid: kid,
		X:   base64.RawURLEncoding.EncodeToString(xPad),
		Y:   base64.RawURLEncoding.EncodeToString(yPad),
	}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
}

func TestVerifier_ValidToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-key-1"
	srv := buildJWKSServer(t, key, kid)
	defer srv.Close()

	v, err := NewVerifier(srv.URL)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	claims := Claims{
		Sub:         "user-123",
		Roles:       []string{"analyst"},
		Permissions: []string{"fatbaby.read"},
		Exp:         time.Now().Add(time.Hour).Unix(),
	}
	token := buildTestJWT(t, key, kid, claims)
	got, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Sub != "user-123" {
		t.Errorf("Sub = %q, want %q", got.Sub, "user-123")
	}
	if !got.HasPermission("fatbaby.read") {
		t.Error("HasPermission(fatbaby.read) = false, want true")
	}
}

func TestVerifier_ExpiredToken(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	const kid = "test-key-exp"
	srv := buildJWKSServer(t, key, kid)
	defer srv.Close()

	v, _ := NewVerifier(srv.URL)
	claims := Claims{Sub: "user-456", Exp: time.Now().Add(-time.Hour).Unix()}
	token := buildTestJWT(t, key, kid, claims)
	_, err := v.Verify(token)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestVerifier_WrongKey(t *testing.T) {
	key1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	key2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	const kid = "test-key-mismatch"
	srv := buildJWKSServer(t, key1, kid) // serves key1
	defer srv.Close()

	v, _ := NewVerifier(srv.URL)
	// Sign with key2 but claim to use kid that maps to key1.
	claims := Claims{Sub: "user-789", Exp: time.Now().Add(time.Hour).Unix()}
	token := buildTestJWT(t, key2, kid, claims)
	_, err := v.Verify(token)
	if err == nil {
		t.Error("expected signature verification failure, got nil error")
	}
}

func TestVerifier_UnknownKid(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := buildJWKSServer(t, key, "known-kid")
	defer srv.Close()

	v, _ := NewVerifier(srv.URL)
	claims := Claims{Sub: "u", Exp: time.Now().Add(time.Hour).Unix()}
	token := buildTestJWT(t, key, "unknown-kid", claims)
	_, err := v.Verify(token)
	if err == nil || !strings.Contains(err.Error(), "unknown key id") {
		t.Errorf("expected unknown key id error, got %v", err)
	}
}

func TestClaims_HasPermission(t *testing.T) {
	c := Claims{Permissions: []string{"fatbaby.read", "signal.write"}}
	if !c.HasPermission("fatbaby.read") {
		t.Error("HasPermission(fatbaby.read) false")
	}
	if c.HasPermission("admin") {
		t.Error("HasPermission(admin) true, want false")
	}
}

func TestBase64URLDecode_Padding(t *testing.T) {
	// Verify that base64url without padding decodes correctly.
	input := "aGVsbG8" // "hello" in base64url without padding
	got, err := base64URLDecode(input)
	if err != nil {
		t.Fatalf("base64URLDecode: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestECKeyFromJWK_P256(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := key.PublicKey
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	xPad := make([]byte, 32)
	yPad := make([]byte, 32)
	copy(xPad[32-len(xBytes):], xBytes)
	copy(yPad[32-len(yBytes):], yBytes)
	k := jwk{
		Kty: "EC", Crv: "P-256", Kid: "k",
		X: base64.RawURLEncoding.EncodeToString(xPad),
		Y: base64.RawURLEncoding.EncodeToString(yPad),
	}
	recovered, err := ecKeyFromJWK(k)
	if err != nil {
		t.Fatalf("ecKeyFromJWK: %v", err)
	}
	if recovered.X.Cmp(pub.X) != 0 || recovered.Y.Cmp(pub.Y) != 0 {
		t.Error("recovered key coordinates do not match original")
	}
}

// TestVerifier_JWKS_TTL verifies that the JWKS cache refreshes after TTL.
func TestVerifier_JWKS_TTL(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		pub := key.PublicKey
		xBytes := pub.X.Bytes()
		yBytes := pub.Y.Bytes()
		xPad := make([]byte, 32)
		yPad := make([]byte, 32)
		copy(xPad[32-len(xBytes):], xBytes)
		copy(yPad[32-len(yBytes):], yBytes)
		resp := jwksResponse{Keys: []jwk{{
			Kty: "EC", Crv: "P-256", Kid: "ttl-key",
			X: base64.RawURLEncoding.EncodeToString(xPad),
			Y: base64.RawURLEncoding.EncodeToString(yPad),
		}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	v, err := NewVerifier(srv.URL)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	v.ttl = 50 * time.Millisecond

	claims := Claims{Sub: "u", Exp: time.Now().Add(time.Hour).Unix()}
	token := buildTestJWT(t, key, "ttl-key", claims)

	if _, err := v.Verify(token); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	initialCount := fetchCount

	// Force TTL expiry.
	v.mu.Lock()
	v.fetchedAt = v.fetchedAt.Add(-time.Second)
	v.mu.Unlock()

	if _, err := v.Verify(token); err != nil {
		t.Fatalf("second verify after TTL: %v", err)
	}
	if fetchCount <= initialCount {
		t.Error("expected a JWKS re-fetch after TTL expiry")
	}
}

// Ensure big.Int is available (imported via math/big in jwks.go).
var _ = new(big.Int)
