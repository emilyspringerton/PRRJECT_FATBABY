package broker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type routeContextKey struct{}

// AuthMiddleware authenticates inbound requests -- by Host header (per-tenant subdomains), by
// URL path (real HTTP Basic Auth, browser-facing routes like JEWEL), or by tenant bearer token
// (the original M2M shape) -- and attaches route metadata. Host-based routes are checked FIRST
// (a tenant subdomain's own paths must never be shadowed by an unrelated global PathPrefix
// route), then path-based, then bearer token -- see Route's own doc comment in model.go for why
// Host routes skip this middleware's own Basic Auth (the upstream tenant's own real auth is
// the actual gate for those).
func AuthMiddleware(reg *Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route, ok := reg.ResolveByHost(hostWithoutPort(r.Host)); ok {
			ctx := context.WithValue(r.Context(), routeContextKey{}, route)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if route, ok := reg.ResolveByPath(r.URL.Path); ok {
			if !checkBasicAuth(route, r) {
				w.Header().Set("WWW-Authenticate", `Basic realm="`+route.TenantID+`"`)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			ctx := context.WithValue(r.Context(), routeContextKey{}, route)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		token := bearerToken(r.Header.Get("Authorization"))
		route, ok := reg.Resolve(token)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unknown tenant")
			return
		}
		if len(route.AllowedPaths) > 0 {
			allowed := false
			for _, p := range route.AllowedPaths {
				if strings.HasPrefix(r.URL.Path, p) {
					allowed = true
					break
				}
			}
			if !allowed {
				writeJSONError(w, http.StatusForbidden, "path not allowed")
				return
			}
		}
		ctx := context.WithValue(r.Context(), routeContextKey{}, route)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RouteFromContext extracts route from context.
func RouteFromContext(ctx context.Context) (*Route, bool) {
	r, ok := ctx.Value(routeContextKey{}).(*Route)
	return r, ok
}

// checkBasicAuth reports whether r carries valid HTTP Basic Auth credentials for route. A route
// with no BasicAuthUser configured is treated as intentionally open (path-based routing alone is
// the gate) -- every path-based route this repo ships as of this writing sets one.
func checkBasicAuth(route *Route, r *http.Request) bool {
	if route.BasicAuthUser == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(route.BasicAuthUser)) == 1
	passMatch := bcrypt.CompareHashAndPassword([]byte(route.BasicAuthPasswordHash), []byte(pass)) == nil
	return userMatch && passMatch
}

// hostWithoutPort strips a ":port" suffix from an http.Request.Host value, if present -- a real
// inbound Host header from a browser or nginx's own proxy_set_header Host $host commonly carries
// no port, but a direct request (e.g. curl to a non-443/80 port during local testing) does.
// net.SplitHostPort errors on a bare hostname with no port at all, which is the common real
// case, so that error path falls back to returning host unchanged rather than treating it as a
// real failure.
func hostWithoutPort(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return h
}

func bearerToken(v string) string {
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
