package newssite

import (
	"net/http/httptest"
	"testing"
)

func TestIPRateLimiter_AllowsUpToLimit(t *testing.T) {
	rl := newIPRateLimiter()
	for i := 1; i <= freeQueryLimit; i++ {
		ok, remaining := rl.check("192.0.2.1")
		if !ok {
			t.Fatalf("query %d: expected allowed, got denied", i)
		}
		if remaining != freeQueryLimit-i {
			t.Errorf("query %d: expected remaining=%d, got %d", i, freeQueryLimit-i, remaining)
		}
	}
}

func TestIPRateLimiter_DeniesOverLimit(t *testing.T) {
	rl := newIPRateLimiter()
	for i := 0; i < freeQueryLimit; i++ {
		rl.check("192.0.2.2")
	}
	ok, _ := rl.check("192.0.2.2")
	if ok {
		t.Fatal("expected denied after limit, got allowed")
	}
}

func TestIPRateLimiter_DifferentIPsIndependent(t *testing.T) {
	rl := newIPRateLimiter()
	for i := 0; i < freeQueryLimit; i++ {
		rl.check("192.0.2.3")
	}
	ok, _ := rl.check("192.0.2.4")
	if !ok {
		t.Fatal("different IP should not be affected by other IP's limit")
	}
}

func TestRemoteIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/ticker/AAPL", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	ip := remoteIP(r)
	if ip != "203.0.113.5" {
		t.Errorf("expected 203.0.113.5, got %s", ip)
	}
}

func TestIsRateLimitedPath(t *testing.T) {
	cases := []struct {
		path    string
		limited bool
	}{
		{"/ticker/AAPL", true},
		{"/doc/abc123", true},
		{"/search", true},
		{"/person/john-doe", true},
		{"/section/earnings", true},
		{"/", false},
		{"/about", false},
		{"/healthz", false},
		{"/api/tickers", false},
		{"/api/chart/AAPL", false},
		{"/feed.xml", false},
	}
	for _, c := range cases {
		got := isRateLimitedPath(c.path)
		if got != c.limited {
			t.Errorf("path %s: expected limited=%v, got %v", c.path, c.limited, got)
		}
	}
}
