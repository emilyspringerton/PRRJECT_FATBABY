Integration Blueprint: Downstream IAM Enforcement (EMILY & PRRJECT-FATBABY)Document ID: HQ-SPEC-IAM-095Subsystem Scope: Downstream Edge Verification, Programmatic Agent Registration, and Middleware GuardsStatus: APPROVED / IMPLEMENTATION MANDATE1. Vision & Technical PrerequisiteWith the core transformation of IDUNA into the central, authoritative ecosystem IAM provider, downstream boundary protection must pivot from checking external authentication matrices to stateless verification of signed IDUNA JSON Web Tokens (JWTs). This document defines the changes required for EMILY (cmd/emily-agent) and PRRJECT-FATBABY  to function inside the new chain of trust.  2. PRRJECT-FATBABY: Downstream Guard ArchitectureThe FARTHQ ingestion pipeline (secwatch, prwatch, processor, dashboard, newssite, feedserver, etc.)  operates statefully inside the network boundary but hosts edge-facing data and stream interfaces. The following changes must be applied to its Go-based backend components.  2.1 JSON Web Key Set (JWKS) Client MiddlewareA reusable security interceptor (internal/iamguard) must be deployed across all edge-exposed HTTP environments (dashboard, signalapi, newssite).  Verification Vector: Middleware must load the public key cluster exposed at IDUNA's .well-known/jwks.json endpoint.Signature Enforcement: Cryptographically parse bearer tokens using the RS256 or ES256 algorithm.Caching Routine: The public keys must be cached in memory with a cache expiration period of exactly 60 minutes to minimize inter-service network traffic.Gopackage iamguard

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	// Dedicated JWT package (e.g., github.com/golang-jwt/jwt/v5)
)

type Claims struct {
	Gamertag    string   `json:"gamertag"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	// Standard claims
}

type Guard struct {
	jwksURL    string
	httpClient *http.Client
	// Key caching properties
}

func NewGuard(jwksURL string) *Guard {
	return &Guard{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// RequirePermission intercepts the HTTP flow to inspect capability footprints
func (g *Guard) RequirePermission(requiredPermission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"Missing bearer token signature"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims, err := g.verifyToken(r.Context(), tokenStr)
			if err != nil {
				http.Error(w, `{"error":"FORBIDDEN","message":"Cryptographic verification failure"}`, http.StatusForbidden)
				return
			}

			// Validate presence of required permission string matching the spec format
			hasPerm := false
			for _, p := range claims.Permissions {
				if p == requiredPermission {
					hasPerm = true
					break
				}
			}

			if !hasPerm {
				http.Error(w, `{"error":"UNAUTHORIZED_CAPABILITY","message":"Insufficient domain clearance"}`, http.StatusUnauthorized)
				return
			}

			// Inject active identity payload context for downstream trace logs
			ctx := context.WithValue(r.Context(), "identity_claims", claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
2.2 Endpoint Protection Mapping MatrixApply the authorization middleware across the primary PRRJECT-FATBABY operational boundaries:Target ComponentEndpoint PatternEnforcement Node RequirementPurpose / Scopecmd/signalapi   GET /api/v1/signalsfatbaby.readQueries live signal intelligence structures.cmd/dashboard   GET /streamfatbaby.readOpens real-time Server-Sent Event (SSE) streams.  cmd/emily-agent   POST /chat   fatbaby.operatorControls direct text-based operator interface commands.  cmd/emily-agent   POST /tick   governance.adminTriggers unattended health sweeps and diagnostic passes.  3. EMILY PRIME: Autonomous Agent Identity GovernanceAutonomous entities are classified as first-class cryptographic actors under Section 2 of the central architecture specification (agents and agent_permissions relational matrices). EMILY (cmd/emily-agent)  must be updated to leverage these changes to avoid pipeline service lockouts.  3.1 Token Acquisition LifecycleInstead of running with unauthenticated local access permissions, emily-agent must be provisioned with machine-to-machine credentials tied to its internal identifier:Canonical Identity Name: EMILYAgent Type Identifier: LLM_AGENTAuthentication Method: Secures an asymmetrical private key file (/var/emily-agent/secrets/agent_identity.pem) or signed token payload. At startup, the agent uses these credentials to obtain an updated short-lived JWT from IDUNA.Token Refresh Routine: The agent must evaluate the exp claim of its in-memory token during its execution loop (POST /tick handling)  and request a new token if it is within 5 minutes of expiration.  3.2 Tool Execution Parameter UpdateEmily’s operational tools (fatbaby_start_process, fatbaby_run_entity_graph_once, fatbaby_write_observation)  must inject its signed access_token into the HTTP Authorization: Bearer headers of all outgoing requests across the inner loop infrastructure.  [ Emily Agent Core ]
       │
       │ 1. Executes internal monitoring rule block (logs, signals, anomalies)
       ▼
[ Tools Engine ] 
       │
       ├─► Appends token signature to request header
       ▼
[ Outgoing Request HTTP Client ] ──► Authorization: Bearer <IDUNA_AGENT_JWT>
       │
       ▼
[ Destination Core Process API ] (e.g., entity-graph, observation-watcher)
4. Implementation Plan & ChecklistThe implementation must be performed incrementally across both target codebases to ensure continuous uptime of the automated processing loops.Phase 1: Dependency Setup & Configurations[ ] Add the required JWT validation dependencies to go.mod in PRRJECT-FATBABY.[ ] Create config/iam_config.json inside the repository to externalize the IDUNA endpoint location mapping fields:JSON{
  "iduna_jwks_url": "https://iam.farthq.internal/.well-known/jwks.json",
  "required_audience": "farthq-ecosystem"
}
Phase 2: Core Middleware Integration[ ] Build the verification module under internal/iamguard/middleware.go.[ ] Implement explicit unit tests confirming that missing headers trigger a 401 Unauthorized response and valid signatures pass successfully.[ ] Inject the verification step into the server initializations within cmd/signalapi/main.go and cmd/dashboard/main.go.Phase 3: Agent Token Loop Integration[ ] Update cmd/emily-agent/main.go to handle agent certificate structures or pre-shared keys.[ ] Build the token verification loop inside cmd/emily-agent to automatically handle expiration checks.[ ] Update all 19 tool calls within cmd/emily-agent/signal_intelligence.go and execution handlers to pass bearer tokens correctly.Phase 4: Verification & Sign-Off[ ] Execute the full verification suite via go test ./....  [ ] Append a dated engineering confirmation line to CHANGELOG.md for tracking.  
