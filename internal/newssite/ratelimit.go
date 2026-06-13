package newssite

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	freeQueryLimit    = 10 // free tier: max page queries per IP per day
	freeAskEmilyLimit = 5  // free tier: max Ask Emily questions per IP per day
	authedAskLimit    = 20 // logged-in tier: max Ask Emily questions per user per day
)

func newAuthedAskRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{
		counts:  make(map[string]int),
		resetAt: nextMidnightUTC(),
		limit:   authedAskLimit,
	}
}

// ipRateLimiter tracks per-IP query counts with daily reset.
type ipRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	resetAt time.Time
	limit   int
}

func newIPRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{
		counts:  make(map[string]int),
		resetAt: nextMidnightUTC(),
		limit:   freeQueryLimit,
	}
}

func newAskRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{
		counts:  make(map[string]int),
		resetAt: nextMidnightUTC(),
		limit:   freeAskEmilyLimit,
	}
}

// check returns (allowed, remaining). Increments the count for ip.
func (rl *ipRateLimiter) check(ip string) (allowed bool, remaining int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UTC()
	if now.After(rl.resetAt) {
		rl.counts = make(map[string]int)
		rl.resetAt = nextMidnightUTC()
	}

	rl.counts[ip]++
	n := rl.counts[ip]
	lim := rl.limit
	if lim <= 0 {
		lim = freeQueryLimit
	}
	if n > lim {
		return false, 0
	}
	return true, lim - n
}

// remoteIP extracts the client IP, respecting X-Forwarded-For for proxied deployments.
func remoteIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// Take the first (leftmost) address — the original client.
		ip := strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isRateLimitedPath returns true for paths that count toward the free query limit.
// Excluded: front page, healthz, about, RSS feeds, static assets, API chart.
func isRateLimitedPath(path string) bool {
	return strings.HasPrefix(path, "/ticker/") ||
		strings.HasPrefix(path, "/doc/") ||
		strings.HasPrefix(path, "/person/") ||
		path == "/search" ||
		path == "/section/earnings" ||
		path == "/section/guidance" ||
		strings.HasPrefix(path, "/section/")
}

func nextMidnightUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}
