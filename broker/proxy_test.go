package broker

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func testRoute(u string) *Route {
	return &Route{TenantID: "t1", TenantKey: "tk", UpstreamBase: u, UpstreamKey: "up", Enabled: true}
}

func TestProxy_AuthRewrite(t *testing.T) {
	got := ""
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization"); w.WriteHeader(204) }))
	defer up.Close()
	p := &ProxyHandler{Client: up.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), routeContextKey{}, testRoute(up.URL)))
	p.ServeHTTP(rr, req)
	if got != "Bearer up" {
		t.Fatalf("got %q", got)
	}
}

func TestProxy_AllowedPathsEnforced(t *testing.T) {
	reg := &Registry{}
	m := map[string]*Route{"tk": {TenantID: "t", TenantKey: "tk", AllowedPaths: []string{"/v1/"}, Enabled: true}}
	reg.ptr.Store(&m)
	h := AuthMiddleware(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v2/x", nil)
	req.Header.Set("Authorization", "Bearer tk")
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestRegistry_HotReload(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "routes.json")
	_ = os.WriteFile(p, []byte(`{"routes":[{"tenant_id":"a","tenant_key":"k1","upstream_base":"http://x","enabled":true}]}`), 0o644)
	r, _ := LoadRegistry(p)
	if _, ok := r.Resolve("k1"); !ok {
		t.Fatal("missing k1")
	}
	_ = os.WriteFile(p, []byte(`{"routes":[{"tenant_id":"a","tenant_key":"k2","upstream_base":"http://x","enabled":true}]}`), 0o644)
	_ = r.Reload()
	if _, ok := r.Resolve("k1"); ok {
		t.Fatal("k1 still present")
	}
	if _, ok := r.Resolve("k2"); !ok {
		t.Fatal("k2 missing")
	}
}

func TestAuthMiddleware_PathBasedRouteRequiresBasicAuth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	byPath := []*Route{{TenantID: "jewel", UpstreamBase: "http://x", PathPrefix: "/jewel/", BasicAuthUser: "jewel", BasicAuthPasswordHash: string(hash), Enabled: true}}
	reg := &Registry{}
	reg.byPath.Store(&byPath)
	h := AuthMiddleware(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/jewel/lab", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("no credentials: got %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate challenge header")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/jewel/lab", nil)
	req.SetBasicAuth("jewel", "wrong-password")
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("wrong password: got %d, want 401", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/jewel/lab", nil)
	req.SetBasicAuth("jewel", "s3cret")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("correct credentials: got %d, want 200", rr.Code)
	}
}

// TestRegistry_ResolveByHost -- real, per-tenant subdomain routing (S243-06, 2026-09-03: "we can
// use the fatbaby proxies for offering custom subdomains for partners/customers"). Exact
// Host-header match, case-insensitive, distinct from an unrelated host.
func TestRegistry_ResolveByHost(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "routes.json")
	_ = os.WriteFile(p, []byte(`{"routes":[
		{"tenant_id":"acme","host":"acme.console.okemily.com","upstream_base":"http://acme-upstream","enabled":true},
		{"tenant_id":"widgetco","host":"widgetco.console.okemily.com","upstream_base":"http://widgetco-upstream","enabled":true}
	]}`), 0o644)
	r, err := LoadRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	route, ok := r.ResolveByHost("acme.console.okemily.com")
	if !ok || route.TenantID != "acme" {
		t.Fatalf("expected acme, got %+v ok=%v", route, ok)
	}
	// Real Host headers vary in case in the wild -- comparison must be case-insensitive.
	route, ok = r.ResolveByHost("ACME.console.okemily.com")
	if !ok || route.TenantID != "acme" {
		t.Fatalf("expected case-insensitive match for acme, got %+v ok=%v", route, ok)
	}
	route, ok = r.ResolveByHost("widgetco.console.okemily.com")
	if !ok || route.TenantID != "widgetco" {
		t.Fatalf("expected widgetco, got %+v ok=%v", route, ok)
	}
	_, ok = r.ResolveByHost("unrelated.example.com")
	if ok {
		t.Fatal("expected no match for an unrelated host")
	}
}

// TestAuthMiddleware_HostBasedRouteSkipsBasicAuth -- a Host-routed tenant subdomain has no Basic
// Auth gate at the broker layer at all (see model.go's own doc comment: the upstream tenant's
// own real IDUNA instance is the real auth boundary for these), unlike a PathPrefix route.
func TestAuthMiddleware_HostBasedRouteSkipsBasicAuth(t *testing.T) {
	byHost := map[string]*Route{
		"acme.console.okemily.com": {TenantID: "acme", UpstreamBase: "http://x", Host: "acme.console.okemily.com", Enabled: true},
	}
	reg := &Registry{}
	reg.byHost.Store(&byHost)
	h := AuthMiddleware(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := RouteFromContext(r.Context())
		if !ok || route.TenantID != "acme" {
			t.Errorf("expected acme route in context, got %+v ok=%v", route, ok)
		}
		w.WriteHeader(200)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/anything/at/all", nil)
	req.Host = "acme.console.okemily.com"
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("no credentials needed for a Host route: got %d, want 200", rr.Code)
	}

	// A port suffix on the Host header (e.g. a direct non-443 request) must not break matching.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/anything", nil)
	req.Host = "acme.console.okemily.com:8443"
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("Host with port suffix: got %d, want 200", rr.Code)
	}
}

// TestAuthMiddleware_HostRouteCheckedBeforePathPrefix -- a tenant subdomain's own paths must
// never be shadowed by an unrelated global PathPrefix route (e.g. a shared "/jewel/" route),
// even if the tenant happens to request a path that collides with one.
func TestAuthMiddleware_HostRouteCheckedBeforePathPrefix(t *testing.T) {
	byHost := map[string]*Route{
		"acme.console.okemily.com": {TenantID: "acme", UpstreamBase: "http://acme-x", Host: "acme.console.okemily.com", Enabled: true},
	}
	byPath := []*Route{
		{TenantID: "jewel", UpstreamBase: "http://jewel-x", PathPrefix: "/jewel/", Enabled: true},
	}
	reg := &Registry{}
	reg.byHost.Store(&byHost)
	reg.byPath.Store(&byPath)
	h := AuthMiddleware(reg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, _ := RouteFromContext(r.Context())
		w.Header().Set("X-Tenant", route.TenantID)
		w.WriteHeader(200)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/jewel/lab", nil)
	req.Host = "acme.console.okemily.com"
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Tenant") != "acme" {
		t.Fatalf("expected the Host route (acme) to win over the PathPrefix route, got tenant %q", rr.Header().Get("X-Tenant"))
	}
}

func TestRegistry_ResolveByPath_LongestPrefixWins(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "routes.json")
	_ = os.WriteFile(p, []byte(`{"routes":[
		{"tenant_id":"a","path_prefix":"/jewel/","upstream_base":"http://a","enabled":true},
		{"tenant_id":"b","path_prefix":"/jewel/lab/","upstream_base":"http://b","enabled":true}
	]}`), 0o644)
	r, err := LoadRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	route, ok := r.ResolveByPath("/jewel/lab/tree")
	if !ok || route.TenantID != "b" {
		t.Fatalf("expected longest-prefix match 'b', got %+v ok=%v", route, ok)
	}
	route, ok = r.ResolveByPath("/jewel/api/kernelspecs")
	if !ok || route.TenantID != "a" {
		t.Fatalf("expected fallback match 'a', got %+v ok=%v", route, ok)
	}
	_, ok = r.ResolveByPath("/unrelated")
	if ok {
		t.Fatal("expected no match for unrelated path")
	}
}

func TestProxy_UpgradeRelaysRawBytesBothWays(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		if req.URL.Path != "/ws/echo" {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
		buf := make([]byte, 5)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		_, _ = conn.Write([]byte("echo:"))
		_, _ = conn.Write(buf)
	}()

	route := &Route{TenantID: "t1", TenantKey: "tk", UpstreamBase: "http://" + ln.Addr().String(), UpstreamKey: "up", SupportsUpgrade: true, Enabled: true}
	ph := &ProxyHandler{Client: http.DefaultClient}
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), routeContextKey{}, route)
		ph.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer frontend.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(frontend.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req, _ := http.NewRequest("GET", "/ws/echo", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("got status %d, want 101", resp.StatusCode)
	}
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("echo:hello"))
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "echo:hello" {
		t.Fatalf("got %q, want %q", got, "echo:hello")
	}
}

func TestProxy_UpgradeRefusedWhenRouteDoesNotOptIn(t *testing.T) {
	route := testRoute("http://127.0.0.1:1") // never dialed -- refused before any upstream contact
	ph := &ProxyHandler{Client: http.DefaultClient}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req = req.WithContext(context.WithValue(req.Context(), routeContextKey{}, route))
	ph.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("got %d, want %d", rr.Code, http.StatusNotImplemented)
	}
}

func TestProxy_StreamingPassthrough(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "chunk\n")
			f.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer up.Close()
	p := &ProxyHandler{Client: up.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), routeContextKey{}, testRoute(up.URL)))
	p.ServeHTTP(rr, req)
	if strings.Count(rr.Body.String(), "chunk") != 3 {
		t.Fatal("missing chunks")
	}
}
