package main

import (
	"database/sql"
	"fmt"
	"strings"
)

func registerDBTools(d *ToolDispatcher, db *sql.DB, fatbabyRoot string) {
	registerMigrationTools(d, db, fatbabyRoot)
	registerSchemaTools(d, db)
	registerProjectorTools(d, db)
}

func registerSchemaTools(d *ToolDispatcher, db *sql.DB) {
	d.Register(ToolDef{
		Name:        "db_status",
		Description: "Ping the database, confirm schema_migrations table presence, and return current database name. Always call this first in a health sweep.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		if err := db.Ping(); err != nil {
			return "", fmt.Errorf("db ping failed: %w", err)
		}
		var dbName string
		_ = db.QueryRow("SELECT DATABASE()").Scan(&dbName)
		var migTableExists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='schema_migrations'`).Scan(&migTableExists)
		var migCount int
		if migTableExists > 0 {
			_ = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migCount)
		}
		return fmt.Sprintf("db_name=%s  schema_migrations_table=%v  migrations_applied=%d", dbName, migTableExists > 0, migCount), nil
	})

	d.Register(ToolDef{
		Name:        "schema_tables",
		Description: "List all tables in the current database with engine, estimated row count, and creation time.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		rows, err := db.Query(`SELECT table_name, engine, COALESCE(table_rows,0), COALESCE(data_length+index_length,0), COALESCE(create_time,'?')
			FROM information_schema.tables WHERE table_schema=DATABASE() ORDER BY table_name`)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		var sb strings.Builder
		count := 0
		for rows.Next() {
			var name, engine, created string
			var rowEst, sizeBytes int64
			if err := rows.Scan(&name, &engine, &rowEst, &sizeBytes, &created); err != nil {
				continue
			}
			fmt.Fprintf(&sb, "%-40s  engine=%-8s  ~rows=%-8d  size=%-10d  created=%s\n", name, engine, rowEst, sizeBytes, created)
			count++
		}
		if count == 0 {
			return "no tables found", nil
		}
		return fmt.Sprintf("Tables (%d):\n%s", count, sb.String()), nil
	})

	d.Register(ToolDef{
		Name:        "schema_describe",
		Description: "Describe a specific table: column types/nullability/defaults and all indexes.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"table": {Type: "string", Description: "Table name"},
			},
			Required: []string{"table"},
		},
	}, func(args map[string]any) (string, error) {
		table, _ := args["table"].(string)
		if table == "" {
			return "", fmt.Errorf("table name is required")
		}
		for _, ch := range table {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return "", fmt.Errorf("invalid table name: %q", table)
			}
		}
		colRows, err := db.Query(`SELECT column_name, column_type, is_nullable, column_default, extra
			FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? ORDER BY ordinal_position`, table)
		if err != nil {
			return "", err
		}
		defer colRows.Close()
		var sb strings.Builder
		fmt.Fprintf(&sb, "Table: %s\n\nCOLUMNS:\n", table)
		colCount := 0
		for colRows.Next() {
			var name, colType, nullable string
			var def, extra sql.NullString
			if err := colRows.Scan(&name, &colType, &nullable, &def, &extra); err != nil {
				continue
			}
			defStr := "NULL"
			if def.Valid {
				defStr = def.String
			}
			extraStr := ""
			if extra.Valid && extra.String != "" {
				extraStr = "  [" + extra.String + "]"
			}
			fmt.Fprintf(&sb, "  %-32s  %-40s  nullable=%-3s  default=%s%s\n", name, colType, nullable, defStr, extraStr)
			colCount++
		}
		if colCount == 0 {
			return fmt.Sprintf("table %q not found or has no columns", table), nil
		}
		idxRows, err := db.Query(`SELECT index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index), non_unique
			FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=?
			GROUP BY index_name, non_unique ORDER BY index_name`, table)
		if err == nil {
			defer idxRows.Close()
			fmt.Fprintf(&sb, "\nINDEXES:\n")
			for idxRows.Next() {
				var name, cols string
				var nonUnique int
				if err := idxRows.Scan(&name, &cols, &nonUnique); err != nil {
					continue
				}
				uniq := ""
				if nonUnique == 0 {
					uniq = " UNIQUE"
				}
				fmt.Fprintf(&sb, "  %-30s  columns=(%s)%s\n", name, cols, uniq)
			}
		}
		return sb.String(), nil
	})

	d.Register(ToolDef{
		Name:        "db_row_counts",
		Description: "Return row counts for FatBaby's key MySQL read-model tables.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		tables := []string{
			"governance_signals", "eps_results", "entity_timeline",
			"projector_cursors", "schema_migrations",
		}
		var sb strings.Builder
		for _, t := range tables {
			var count int
			if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", t)).Scan(&count); err != nil {
				fmt.Fprintf(&sb, "  %-30s  ERROR: %v\n", t, err)
			} else {
				fmt.Fprintf(&sb, "  %-30s  %d rows\n", t, count)
			}
		}
		return sb.String(), nil
	})

	d.Register(ToolDef{
		Name:        "db_query",
		Description: "Run a read-only SELECT/SHOW/DESCRIBE/EXPLAIN query against the FatBaby MySQL database.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"sql": {Type: "string", Description: "A SELECT query to execute"},
			},
			Required: []string{"sql"},
		},
	}, func(args map[string]any) (string, error) {
		query, _ := args["sql"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("sql is required")
		}
		upper := strings.ToUpper(query)
		for _, banned := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "TRUNCATE", "GRANT", "REVOKE", "REPLACE"} {
			if strings.HasPrefix(upper, banned) {
				return "", fmt.Errorf("only SELECT/SHOW/DESCRIBE/EXPLAIN allowed; got: %s", banned)
			}
		}
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") {
			return "", fmt.Errorf("only SELECT/SHOW/DESCRIBE/EXPLAIN statements are allowed")
		}
		rows, err := db.Query(query)
		if err != nil {
			return "", fmt.Errorf("query error: %w", err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		fmt.Fprintln(&sb, strings.Join(cols, "\t"))
		fmt.Fprintln(&sb, strings.Repeat("-", len(strings.Join(cols, "\t"))))
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rowCount := 0
		for rows.Next() {
			if rowCount >= 200 {
				fmt.Fprintln(&sb, "[truncated at 200 rows]")
				break
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			parts := make([]string, len(cols))
			for i, v := range vals {
				if v == nil {
					parts[i] = "NULL"
				} else {
					parts[i] = fmt.Sprintf("%v", v)
				}
			}
			fmt.Fprintln(&sb, strings.Join(parts, "\t"))
			rowCount++
		}
		if rowCount == 0 {
			return "(no rows returned)", nil
		}
		return sb.String(), nil
	})
}

func registerProjectorTools(d *ToolDispatcher, db *sql.DB) {
	d.Register(ToolDef{
		Name:        "projector_status",
		Description: "Show the current projector cursor position and how many signals are in governance_signals vs. what the eventstore has processed. Use this to diagnose whether the projector is keeping up.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		rows, err := db.Query(`SELECT projector_name, last_seq, updated_at FROM projector_cursors ORDER BY projector_name`)
		if err != nil {
			return "", fmt.Errorf("query projector_cursors: %w", err)
		}
		defer rows.Close()
		var sb strings.Builder
		fmt.Fprintln(&sb, "PROJECTOR CURSORS:")
		count := 0
		for rows.Next() {
			var name string
			var seq int64
			var updatedAt string
			if err := rows.Scan(&name, &seq, &updatedAt); err != nil {
				continue
			}
			fmt.Fprintf(&sb, "  %-30s  last_seq=%-10d  updated=%s\n", name, seq, updatedAt)
			count++
		}
		if count == 0 {
			fmt.Fprintln(&sb, "  (no cursors — projector has never run)")
		}

		var totalSignals, nullPubDate int
		_ = db.QueryRow("SELECT COUNT(*) FROM governance_signals").Scan(&totalSignals)
		_ = db.QueryRow("SELECT COUNT(*) FROM governance_signals WHERE source_published_at IS NULL").Scan(&nullPubDate)
		fmt.Fprintf(&sb, "\ngovernance_signals: total=%d  missing_source_published_at=%d", totalSignals, nullPubDate)
		if nullPubDate > 0 {
			fmt.Fprintln(&sb, "\n  → Run projector with reset cursor to backfill source_published_at")
		}
		return sb.String(), nil
	})

	d.Register(ToolDef{
		Name:        "signal_sample",
		Description: "Show the 10 most recent signals for a ticker, ordered by source_published_at DESC.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Equity ticker symbol (e.g. AAPL)"},
			},
			Required: []string{"ticker"},
		},
	}, func(args map[string]any) (string, error) {
		ticker, _ := args["ticker"].(string)
		if ticker == "" {
			return "", fmt.Errorf("ticker is required")
		}
		rows, err := db.Query(
			`SELECT id, event_type, headline, filing_date, COALESCE(source_published_at,''), signal_score, eventstore_seq
			 FROM governance_signals WHERE ticker=?
			 ORDER BY COALESCE(source_published_at, filing_date) DESC LIMIT 10`,
			strings.ToUpper(strings.TrimSpace(ticker)),
		)
		if err != nil {
			return "", fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		var sb strings.Builder
		fmt.Fprintf(&sb, "Last 10 signals for %s (by source_published_at):\n", strings.ToUpper(ticker))
		count := 0
		for rows.Next() {
			var id, seq int64
			var eventType, headline, filingDate, pubDate string
			var score float64
			if err := rows.Scan(&id, &eventType, &headline, &filingDate, &pubDate, &score, &seq); err != nil {
				continue
			}
			pubStr := pubDate
			if pubStr == "" {
				pubStr = "(null)"
			}
			fmt.Fprintf(&sb, "  [%d] %s  pub=%s  filing=%s  score=%.2f  seq=%d\n      %s\n",
				id, eventType, pubStr, filingDate, score, seq, truncate(headline, 100))
			count++
		}
		if count == 0 {
			return fmt.Sprintf("no signals found for %s", strings.ToUpper(ticker)), nil
		}
		return sb.String(), nil
	})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
