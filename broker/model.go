package broker

// Route defines one tenant-to-upstream contract.
//
// Three independent ways a request can match a Route:
//   - Tenant bearer token (TenantKey) -- the original M2M shape, e.g. emily-agent calling
//     gpt2-alpine-c's serve.py. Authorization: Bearer <TenantKey>.
//   - PathPrefix -- for browser-facing services (e.g. JEWEL) that can't reasonably be expected
//     to send a custom bearer token. Matched by longest-prefix on r.URL.Path, gated by real HTTP
//     Basic Auth (BasicAuthUser/BasicAuthPasswordHash) instead. Added 2026-08-26, founder: "set
//     up a single nginx proxy" + "use fatbaby proxy broker to manage proxies instead of always
//     asking for a new feature" -- one sudo-gated nginx location proxying everything to this
//     broker, with the broker itself (no sudo needed to redeploy, see ops/systemd/
//     fatbaby-broker.service) owning per-service routing/auth from here on.
//   - Host -- exact Host-header match, for per-tenant subdomains (Emily for Business console
//     onboarding, S243-02/06: "we can use the fatbaby proxies for offering custom subdomains for
//     partners/customers"). Added 2026-09-03. Unlike PathPrefix, a Host route is NOT gated by
//     this broker's own Basic Auth -- the upstream is expected to be that tenant's own real
//     IDUNA instance, which does its own real JWT/OAuth auth; the broker's job for a Host route
//     is purely "get the packet to the right tenant's upstream," not authenticate it itself.
//     Checked before PathPrefix (see AuthMiddleware) so a tenant subdomain's own paths are never
//     shadowed by an unrelated global PathPrefix route.
//
// A Route may set any of the three matchers, but in practice a given route is exactly one.
type Route struct {
	TenantID      string            `json:"tenant_id"`
	TenantKey     string            `json:"tenant_key"`
	UpstreamBase  string            `json:"upstream_base"`
	UpstreamKey   string            `json:"upstream_key"`
	StripHeaders  []string          `json:"strip_headers"`
	InjectHeaders map[string]string `json:"inject_headers"`
	AllowedPaths  []string          `json:"allowed_paths"`
	MaxBodyBytes  int64             `json:"max_body_bytes"`
	Enabled       bool              `json:"enabled"`

	// PathPrefix, when non-empty, makes this route reachable by URL path instead of (or in
	// addition to) tenant bearer token -- see the type doc comment above.
	PathPrefix string `json:"path_prefix"`
	// BasicAuthUser/BasicAuthPasswordHash gate a PathPrefix route with real HTTP Basic Auth.
	// PasswordHash is a bcrypt hash (golang.org/x/crypto/bcrypt) -- never a plaintext password,
	// same as any real htpasswd-style credential store.
	BasicAuthUser         string `json:"basic_auth_user"`
	BasicAuthPasswordHash string `json:"basic_auth_password_hash"`
	// SupportsUpgrade opts a route into real WebSocket proxying (ProxyHandler.handleUpgrade) --
	// off by default since the original bearer-token M2M routes never need it.
	SupportsUpgrade bool `json:"supports_upgrade"`
	// Host, when non-empty, makes this route reachable by an exact Host-header match (e.g.
	// "acme.console.okemily.com") -- see the type doc comment above. Not gated by this broker's
	// own Basic Auth; deliberately relies on the upstream tenant's own real auth.
	Host string `json:"host"`
}

// RouteTenant is a minimal tenant identity used by feedserver session auth.
type RouteTenant struct {
	ID  string
	Key string
}
