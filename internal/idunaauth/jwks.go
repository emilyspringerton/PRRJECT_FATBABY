// Package idunaauth provides JWT verification against IDUNA's JWKS endpoint.
// It uses only the Go standard library (crypto/ecdsa, crypto/elliptic, encoding/json).
//
// Supported algorithm: ES256 (ECDSA + P-256 + SHA-256), which is what IDUNA issues.
// RS256 tokens are rejected; the middleware falls back to API key auth when no
// IDUNA_JWKS_URL is configured, so existing deployments are unaffected.
//
// Usage:
//
//	v, err := idunaauth.NewVerifier("https://iam.farthq.internal")
//	if err != nil { /* handle */ }
//	claims, err := v.Verify(tokenString)
package idunaauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Claims is the minimal set of claims extracted from a verified IDUNA JWT.
type Claims struct {
	Sub         string   `json:"sub"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	Email       string   `json:"email,omitempty"`
	Exp         int64    `json:"exp"`
}

// Verifier fetches IDUNA's JWKS and verifies ES256 JWTs. Thread-safe.
type Verifier struct {
	jwksURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*ecdsa.PublicKey // kid → key
	fetchedAt time.Time
	ttl       time.Duration
}

// NewVerifier constructs a Verifier that fetches JWKS from baseURL/api/v1/jwks.
func NewVerifier(baseURL string) (*Verifier, error) {
	v := &Verifier{
		jwksURL:    strings.TrimRight(baseURL, "/") + "/api/v1/jwks",
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ttl:        60 * time.Minute, // spec HQ-SPEC-IAM-094 §2.1: 60-minute cache
	}
	if err := v.refresh(context.Background()); err != nil {
		return nil, fmt.Errorf("idunaauth: initial JWKS fetch: %w", err)
	}
	return v, nil
}

// Verify parses and validates a raw JWT string. Returns Claims on success.
func (v *Verifier) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("idunaauth: malformed JWT")
	}
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("idunaauth: decode header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, fmt.Errorf("idunaauth: parse header: %w", err)
	}
	if header.Alg != "ES256" {
		return Claims{}, fmt.Errorf("idunaauth: unsupported algorithm %q (want ES256)", header.Alg)
	}

	key, err := v.getKey(context.Background(), header.Kid)
	if err != nil {
		return Claims{}, err
	}

	sigBytes, err := base64URLDecode(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("idunaauth: decode signature: %w", err)
	}
	r, s, err := decodeECDSASignature(sigBytes)
	if err != nil {
		return Claims{}, fmt.Errorf("idunaauth: parse signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(key, digest[:], r, s) {
		return Claims{}, fmt.Errorf("idunaauth: signature verification failed")
	}

	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("idunaauth: decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return Claims{}, fmt.Errorf("idunaauth: parse claims: %w", err)
	}
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return Claims{}, fmt.Errorf("idunaauth: token expired")
	}
	return claims, nil
}

// HasPermission returns true when claims include the given permission.
func (c Claims) HasPermission(perm string) bool {
	for _, p := range c.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// JWKS fetching
// ---------------------------------------------------------------------------

// jwksResponse is the minimal JWKS JSON structure IDUNA returns.
type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"` // "EC"
	Crv string `json:"crv"` // "P-256"
	Kid string `json:"kid"`
	X   string `json:"x"` // base64url-encoded X coordinate
	Y   string `json:"y"` // base64url-encoded Y coordinate
}

func (v *Verifier) getKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	stale := time.Since(v.fetchedAt) > v.ttl
	v.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("idunaauth: unknown key id %q", kid)
	}
	return key, nil
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}
	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}
	keys := make(map[string]*ecdsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue
		}
		pub, err := ecKeyFromJWK(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	v.mu.Lock()
	v.keys = keys
	v.fetchedAt = time.Now()
	v.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// EC key and signature helpers
// ---------------------------------------------------------------------------

func ecKeyFromJWK(k jwk) (*ecdsa.PublicKey, error) {
	xBytes, err := base64URLDecode(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yBytes, err := base64URLDecode(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// decodeECDSASignature decodes a raw IEEE P1363 signature (r||s, each 32 bytes for P-256).
func decodeECDSASignature(sig []byte) (r, s *big.Int, err error) {
	if len(sig) != 64 {
		return nil, nil, fmt.Errorf("expected 64-byte P1363 signature, got %d", len(sig))
	}
	r = new(big.Int).SetBytes(sig[:32])
	s = new(big.Int).SetBytes(sig[32:])
	return r, s, nil
}

func base64URLDecode(s string) ([]byte, error) {
	// Accept standard and URL-safe base64, with or without padding.
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}
