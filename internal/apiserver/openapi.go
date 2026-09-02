package apiserver

import (
	"encoding/json"
	"net/http"
)

// handleOpenAPI serves the OpenAPI 3.1 spec at GET /v1/openapi.json.
// Deliberately NOT wrapped in withMiddleware (no auth required to read the
// spec itself, same as IDUNA's OpenAPIHandler) -- only the actual data
// endpoints require a Bearer token. CORS is open since this is a public,
// read-only document meant to be fetched cross-origin by a Swagger UI page
// served from newssite's own domain.
func handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(signalAPIOpenAPISpec)
}

// signalAPIOpenAPISpec is the OpenAPI 3.1 description of the FatBaby Signal
// API (cmd/signalapi). Keep in sync with actual routes registered in
// server.go's New().
var signalAPIOpenAPISpec = map[string]any{
	"openapi": "3.1.0",
	"info": map[string]any{
		"title":       "FatBaby Signal API",
		"description": "Structured financial governance signals, EPS results, entity graph, and earnings calendar data for EINHORN_INDUSTRIAL's FatBaby pipeline. Most endpoints require a Bearer token (static API key or an IDUNA-issued JWT with fatbaby.read).",
		"version":     "1.0.0",
		"contact": map[string]any{
			"name":  "Emily Prime",
			"email": "emilyspringerton@gmail.com",
		},
	},
	"servers": []map[string]any{
		// Relative URL, resolved by Swagger UI against whatever page is
		// hosting it -- correct whether that's news.okemily.com (proxied
		// same-origin via newssite's /signalapi/* handler) or any other
		// domain newssite ends up served on. Listed first since Swagger UI
		// defaults to the first entry; this is the one real visitors need.
		{"url": "/signalapi", "description": "Same-origin, proxied by newssite"},
		{"url": "http://localhost:9091", "description": "Direct, local development only"},
	},
	"components": map[string]any{
		"securitySchemes": map[string]any{
			"bearerAuth": map[string]any{
				"type":         "http",
				"scheme":       "bearer",
				"description":  "Either a static signalapi API key, or an IDUNA-issued JWT carrying the fatbaby.read permission.",
			},
		},
	},
	"security": []map[string]any{{"bearerAuth": []string{}}},
	"paths": map[string]any{
		"/v1/health": map[string]any{
			"get": map[string]any{
				"summary":     "Health check",
				"description": "No auth required.",
				"security":    []map[string]any{},
				"responses":   okResponse("Service status."),
			},
		},
		"/v1/signals": map[string]any{
			"get": map[string]any{
				"summary":   "Summary of all signals currently indexed",
				"responses": okResponse("Signal index summary."),
			},
		},
		"/v1/signals/{ticker}": map[string]any{
			"get": map[string]any{
				"summary":     "Signals for one ticker",
				"description": "Dispatches on the path suffix after /v1/signals/ -- a ticker symbol.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
				},
				"responses": okResponse("Signals for the ticker."),
			},
		},
		"/v1/governance-signals": map[string]any{
			"get": map[string]any{
				"summary":     "Query governance signals (MySQL read model)",
				"description": "Returns 503 if the MySQL read model isn't configured.",
				"parameters": []map[string]any{
					queryParam("ticker", "string", "Filter to one ticker."),
					queryParam("type", "string", "Filter to one event_type."),
					queryParam("since", "string", "Filter filing_date >= this (YYYY-MM-DD)."),
					queryParam("until", "string", "Filter filing_date <= this (YYYY-MM-DD)."),
					queryParam("limit", "integer", "Max rows, default 50, capped at the server's configured MaxLimit."),
				},
				"responses": okResponse("Governance signal rows, newest first."),
			},
		},
		"/v1/eps/{ticker}": map[string]any{
			"get": map[string]any{
				"summary":     "EPS results for a ticker (MySQL read model)",
				"description": "Returns 503 if the MySQL read model isn't configured.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
					queryParam("periods", "integer", "Number of periods, default 8, max 20."),
				},
				"responses": okResponse("EPS result rows, newest first."),
			},
		},
		"/v1/entities/{ticker}": map[string]any{
			"get": map[string]any{
				"summary":     "Entity graph document for a ticker (MongoDB read model)",
				"description": "Returns 503 if MongoDB isn't configured, 404 if the ticker has no entity document.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
				},
				"responses": okResponse("The entity document."),
			},
		},
		"/v1/entities/{ticker}/related": map[string]any{
			"get": map[string]any{
				"summary":     "Related tickers by co-occurrence",
				"description": "Up to 10 related tickers ordered by co-occurrence weight. Returns 503 if the co-occurrence store isn't configured.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
				},
				"responses": okResponse("Related tickers."),
			},
		},
		"/v1/movers-history/{ticker}": map[string]any{
			"get": map[string]any{
				"summary":     "Gainers/losers snapshot history for a ticker",
				"description": "Daily market_movers_snapshot appearances (day_gainers/day_losers screener), newest first. Returns 503 if the movers index isn't configured.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
					queryParam("limit", "integer", "Max entries, default 30."),
				},
				"responses": okResponse("Movers snapshot entries, newest first."),
			},
		},
		"/v1/earnings-calendar": map[string]any{
			"get": map[string]any{
				"summary":   "Upcoming/past earnings calendar entries",
				"responses": okResponse("Earnings calendar rows."),
			},
		},
		"/v1/press-releases/{ticker}": map[string]any{
			"get": map[string]any{
				"summary":     "Press releases for a ticker",
				"description": "Requires the doc index to be configured.",
				"parameters": []map[string]any{
					pathParam("ticker", "Ticker symbol, e.g. AAPL"),
					queryParam("limit", "integer", "Max results (default 20)."),
					queryParam("provider", "string", "Filter by wire service (\"prnewswire\", \"businesswire\"); omitted returns all providers."),
				},
				"responses": okResponse("Press release documents."),
			},
		},
		"/v1/velocity-alerts": map[string]any{
			"get": map[string]any{
				"summary":   "Signal velocity alerts",
				"responses": okResponse("Velocity alert rows."),
			},
		},
		"/v1/data-quality": map[string]any{
			"get": map[string]any{
				"summary":   "Pipeline data-quality metrics",
				"responses": okResponse("Data quality report."),
			},
		},
	},
}

func okResponse(desc string) map[string]any {
	return map[string]any{
		"200": map[string]any{
			"description": desc,
			"content": map[string]any{
				"application/json": map[string]any{},
			},
		},
	}
}

func pathParam(name, desc string) map[string]any {
	return map[string]any{
		"name": name, "in": "path", "required": true, "description": desc,
		"schema": map[string]any{"type": "string"},
	}
}

func queryParam(name, typ, desc string) map[string]any {
	return map[string]any{
		"name": name, "in": "query", "required": false, "description": desc,
		"schema": map[string]any{"type": typ},
	}
}
