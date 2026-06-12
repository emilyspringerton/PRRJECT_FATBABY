package apiserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GovernanceSignalRow is one row from the governance_signals MySQL table.
type GovernanceSignalRow struct {
	ID           int64   `json:"id"`
	Ticker       string  `json:"ticker"`
	EventType    string  `json:"event_type"`
	FilingID     string  `json:"filing_id"`
	EntityName   string  `json:"entity_name"`
	SignalScore  float64 `json:"signal_score"`
	Headline     string  `json:"headline"`
	FilingDate   string  `json:"filing_date"`
	IngestedAt   string  `json:"ingested_at"`
	EventstoreSeq uint64 `json:"eventstore_seq"`
}

// EPSResultRow is one row from the eps_results MySQL table.
type EPSResultRow struct {
	ID            int64    `json:"id"`
	Ticker        string   `json:"ticker"`
	Period        string   `json:"period"`
	EpsActual     *float64 `json:"eps_actual"`
	EpsEstimate   *float64 `json:"eps_estimate"`
	SurprisePct   *float64 `json:"surprise_pct"`
	RevenueActual *float64 `json:"revenue_actual"`
	ReportDate    string   `json:"report_date"`
	FilingID      string   `json:"filing_id"`
}

// handleGovernanceSignals serves GET /v1/governance-signals
//
//	?ticker=BAC
//	&type=auditor_change
//	&since=2026-01-01
//	&until=2026-06-12
//	&limit=50
func (s *server) handleGovernanceSignals(_ http.ResponseWriter, r *http.Request) (int, any) {
	if s.cfg.MySQL == nil {
		return http.StatusServiceUnavailable, map[string]string{"error": "MySQL not configured"}
	}
	q := r.URL.Query()
	ticker := q.Get("ticker")
	eventType := q.Get("type")
	since := q.Get("since")
	until := q.Get("until")
	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			if n > s.cfg.MaxLimit {
				n = s.cfg.MaxLimit
			}
			limit = n
		}
	}

	args := []any{}
	where := "WHERE 1=1"
	if ticker != "" {
		where += " AND ticker = ?"
		args = append(args, ticker)
	}
	if eventType != "" {
		where += " AND event_type = ?"
		args = append(args, eventType)
	}
	if since != "" {
		where += " AND filing_date >= ?"
		args = append(args, since)
	}
	if until != "" {
		where += " AND filing_date <= ?"
		args = append(args, until)
	}
	args = append(args, limit)

	query := "SELECT id, ticker, event_type, filing_id, entity_name, signal_score, headline, filing_date, ingested_at, eventstore_seq FROM governance_signals " + where + " ORDER BY filing_date DESC LIMIT ?"
	rows, err := s.cfg.MySQL.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.cfg.Logger.Printf("governance-signals query: %v", err)
		return http.StatusInternalServerError, map[string]string{"error": "query failed"}
	}
	defer rows.Close()

	var results []GovernanceSignalRow
	for rows.Next() {
		var row GovernanceSignalRow
		var ingestedAt time.Time
		if err := rows.Scan(&row.ID, &row.Ticker, &row.EventType, &row.FilingID,
			&row.EntityName, &row.SignalScore, &row.Headline, &row.FilingDate,
			&ingestedAt, &row.EventstoreSeq); err != nil {
			continue
		}
		row.IngestedAt = ingestedAt.UTC().Format(time.RFC3339)
		results = append(results, row)
	}
	if results == nil {
		results = []GovernanceSignalRow{}
	}
	return http.StatusOK, results
}

// handleEPS serves GET /v1/eps/{ticker}[?periods=N]
func (s *server) handleEPS(_ http.ResponseWriter, r *http.Request) (int, any) {
	if s.cfg.MySQL == nil {
		return http.StatusServiceUnavailable, map[string]string{"error": "MySQL not configured"}
	}
	ticker := r.PathValue("ticker")
	if ticker == "" {
		return http.StatusBadRequest, map[string]string{"error": "ticker required"}
	}
	periods := 8
	if p := r.URL.Query().Get("periods"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 20 {
			periods = n
		}
	}

	rows, err := s.cfg.MySQL.QueryContext(r.Context(),
		"SELECT id, ticker, period, eps_actual, eps_estimate, surprise_pct, revenue_actual, report_date, filing_id FROM eps_results WHERE ticker = ? ORDER BY report_date DESC LIMIT ?",
		ticker, periods,
	)
	if err != nil {
		s.cfg.Logger.Printf("eps query ticker=%s: %v", ticker, err)
		return http.StatusInternalServerError, map[string]string{"error": "query failed"}
	}
	defer rows.Close()

	var results []EPSResultRow
	for rows.Next() {
		var row EPSResultRow
		if err := rows.Scan(&row.ID, &row.Ticker, &row.Period,
			&row.EpsActual, &row.EpsEstimate, &row.SurprisePct, &row.RevenueActual,
			&row.ReportDate, &row.FilingID); err != nil {
			continue
		}
		results = append(results, row)
	}
	if results == nil {
		results = []EPSResultRow{}
	}
	return http.StatusOK, results
}

// handleEntity serves GET /v1/entities/{ticker}
func (s *server) handleEntity(_ http.ResponseWriter, r *http.Request) (int, any) {
	if s.cfg.Mongo == nil {
		return http.StatusServiceUnavailable, map[string]string{"error": "MongoDB not configured"}
	}
	ticker := r.PathValue("ticker")
	if ticker == "" {
		return http.StatusBadRequest, map[string]string{"error": "ticker required"}
	}
	coll := s.cfg.Mongo.Database(s.cfg.MongoDB).Collection("entities")
	var doc bson.M
	err := coll.FindOne(r.Context(), bson.M{"ticker": ticker}, options.FindOne()).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return http.StatusNotFound, map[string]string{"error": "entity not found"}
	}
	if err != nil {
		s.cfg.Logger.Printf("entity query ticker=%s: %v", ticker, err)
		return http.StatusInternalServerError, map[string]string{"error": "query failed"}
	}
	// Convert bson.M to JSON-serializable map (bson.M is map[string]any).
	b, err := json.Marshal(doc)
	if err != nil {
		return http.StatusInternalServerError, map[string]string{"error": "encode failed"}
	}
	var result map[string]any
	_ = json.Unmarshal(b, &result)
	return http.StatusOK, result
}

