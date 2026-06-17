package store

import (
	"regexp"
	"strings"
)

// mysqlToSQLite translates a MySQL DDL/DML statement to SQLite-compatible SQL.
//
// Handles exactly the patterns that appear in migrations/mysql/. Not a general
// MySQL→SQLite converter. Add rules here when new migrations introduce new patterns.
//
// Rules applied (in order):
//  1. Backtick identifiers removed
//  2. INSERT IGNORE → INSERT OR IGNORE
//  3. ENGINE=... table options stripped
//  4. BIGINT [UNSIGNED] ... AUTO_INCREMENT PRIMARY KEY → INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT
//  5. BIGINT [UNSIGNED] NOT NULL AUTO_INCREMENT (non-inline PK) → INTEGER NOT NULL
//  6. Remaining AUTO_INCREMENT → AUTOINCREMENT
//  7. TIMESTAMP(6) → TEXT
//  8. ON UPDATE CURRENT_TIMESTAMP(6) removed
//  9. DEFAULT CURRENT_TIMESTAMP(6) → DEFAULT CURRENT_TIMESTAMP
// 10. CURRENT_TIMESTAMP(6) anywhere → CURRENT_TIMESTAMP
// 11. ENUM(...) → TEXT
// 12. TINYINT(1) → INTEGER
// 13. MEDIUMTEXT → TEXT
// 14. MySQL COMMENT 'text' on columns and COMMENT='text' table options removed
// 15. CONSTRAINT ... FOREIGN KEY lines removed
// 16. Inline KEY / INDEX lines removed
// 17. ADD COLUMN IF NOT EXISTS → ADD COLUMN (SQLite doesn't support IF NOT EXISTS on ALTER)
// 18. ADD INDEX IF NOT EXISTS → (dropped; CREATE INDEX IF NOT EXISTS is handled separately)
// 19. Trailing commas before closing paren cleaned up
// 20. COLLATE utf8mb4_unicode_ci stripped
// 21. AFTER <col> in ADD COLUMN stripped (MySQL positional; SQLite always appends)
func mysqlToSQLite(sql string) string {
	stmts := splitStatements(sql)
	out := make([]string, 0, len(stmts))
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		translated := translateStatement(s)
		if translated != "" {
			out = append(out, translated)
		}
	}
	return strings.Join(out, ";\n")
}

func translateStatement(s string) string {
	// Detect ALTER TABLE ... ADD INDEX IF NOT EXISTS — SQLite doesn't support ADD INDEX.
	// Convert to a standalone CREATE INDEX IF NOT EXISTS statement instead.
	if m := reAlterAddIndex.FindStringSubmatch(s); m != nil {
		table := strings.TrimSpace(m[1])
		idxName := strings.TrimSpace(m[2])
		cols := strings.TrimSpace(m[3])
		// Strip DESC from column list — SQLite ignores DESC on index columns anyway.
		cols = reDescInCols.ReplaceAllString(cols, "")
		return "CREATE INDEX IF NOT EXISTS " + idxName + " ON " + table + " (" + cols + ")"
	}

	// ALTER TABLE ... ADD COLUMN IF NOT EXISTS → ADD COLUMN (SQLite 3.37+ supports IF NOT EXISTS
	// on ADD COLUMN but older versions don't; drop the guard and rely on idempotent migration tracking).
	s = reAlterAddColumnIfNotExists.ReplaceAllString(s, "ALTER TABLE $1 ADD COLUMN")

	// Backtick identifiers → plain.
	s = reBacktick.ReplaceAllString(s, "")

	// INSERT IGNORE → INSERT OR IGNORE.
	s = reInsertIgnore.ReplaceAllString(s, "INSERT OR IGNORE")

	// Remove ENGINE=InnoDB ... and charset / collation options.
	s = reEngine.ReplaceAllString(s, "")
	s = reCollate.ReplaceAllString(s, "")

	// BIGINT [UNSIGNED] ... PRIMARY KEY AUTO_INCREMENT → INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT.
	s = reBigintAutoIncrementPK.ReplaceAllString(s, "INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT")

	// BIGINT [UNSIGNED] NOT NULL AUTO_INCREMENT (separate PK clause) → INTEGER NOT NULL.
	s = reBigintAutoIncrementOnly.ReplaceAllString(s, "INTEGER NOT NULL")

	// Remaining AUTO_INCREMENT → AUTOINCREMENT.
	s = reAutoIncrement.ReplaceAllString(s, "AUTOINCREMENT")

	// MEDIUMTEXT → TEXT.
	s = reMediumText.ReplaceAllString(s, "TEXT")

	// TIMESTAMP(6) → TEXT.
	s = reTimestamp6.ReplaceAllString(s, "TEXT")

	// ON UPDATE CURRENT_TIMESTAMP(6) removed before bare CURRENT_TIMESTAMP(6) rule.
	s = reOnUpdateTS.ReplaceAllString(s, "")

	// DEFAULT CURRENT_TIMESTAMP(6) → DEFAULT CURRENT_TIMESTAMP.
	s = reDefaultCurrentTS6.ReplaceAllString(s, "DEFAULT CURRENT_TIMESTAMP")

	// Remaining CURRENT_TIMESTAMP(6) → CURRENT_TIMESTAMP.
	s = reTimestamp6Bare.ReplaceAllString(s, "CURRENT_TIMESTAMP")

	// ENUM('A','B') → TEXT.
	s = reEnum.ReplaceAllString(s, "TEXT")

	// TINYINT(1) → INTEGER.
	s = reTinyint1.ReplaceAllString(s, "INTEGER")

	// MySQL column COMMENT 'text' and table-level COMMENT='text' removed.
	s = reColumnComment.ReplaceAllString(s, "")
	s = reTableComment.ReplaceAllString(s, "")

	// Remove CONSTRAINT ... FOREIGN KEY lines and inline KEY/INDEX lines.
	s = removeForeignKeyLines(s)

	// AFTER <col> in ALTER TABLE ADD COLUMN — MySQL positional syntax; SQLite
	// always appends to the end. Strip so the statement is valid SQLite.
	s = reAlterAfterCol.ReplaceAllString(s, "")

	// Remove trailing commas before closing paren.
	s = cleanTrailingCommas(s)

	return s
}

var (
	reBacktick     = regexp.MustCompile("`")
	reInsertIgnore = regexp.MustCompile(`(?i)\bINSERT\s+IGNORE\b`)
	reEngine       = regexp.MustCompile(`(?im)\s*ENGINE\s*=\s*\S+[^\n]*$`)
	reCollate      = regexp.MustCompile(`(?i)\s+COLLATE\s+\S+`)

	reBigintAutoIncrementPK   = regexp.MustCompile(`(?i)\bBIGINT(?:\s+UNSIGNED)?\s+(?:NOT\s+NULL\s+)?(?:AUTO_INCREMENT\s+PRIMARY\s+KEY|PRIMARY\s+KEY\s+AUTO_INCREMENT)`)
	reBigintAutoIncrementOnly = regexp.MustCompile(`(?i)\bBIGINT(?:\s+UNSIGNED)?\s+NOT\s+NULL\s+AUTO_INCREMENT\b`)
	reAutoIncrement           = regexp.MustCompile(`(?i)\bAUTO_INCREMENT\b`)

	reMediumText        = regexp.MustCompile(`(?i)\bMEDIUMTEXT\b`)
	reTimestamp6        = regexp.MustCompile(`(?i)\bTIMESTAMP\(6\)`)
	reOnUpdateTS        = regexp.MustCompile(`(?i)\s*ON\s+UPDATE\s+CURRENT_TIMESTAMP\(6\)`)
	reDefaultCurrentTS6 = regexp.MustCompile(`(?i)\bDEFAULT\s+CURRENT_TIMESTAMP\(6\)`)
	reTimestamp6Bare    = regexp.MustCompile(`(?i)\bCURRENT_TIMESTAMP\(6\)`)
	reEnum              = regexp.MustCompile(`(?i)\bENUM\([^)]+\)`)
	reTinyint1          = regexp.MustCompile(`(?i)\bTINYINT\(1\)`)
	reColumnComment     = regexp.MustCompile(`(?i)\s+COMMENT\s+'[^']*'`)
	reTableComment      = regexp.MustCompile(`(?im)\s*COMMENT\s*=\s*'[^']*'\s*$`)

	reConstraintFK = regexp.MustCompile(`(?i)^\s*CONSTRAINT\s+\S+\s+FOREIGN\s+KEY\b.*$`)
	reInlineKey    = regexp.MustCompile(`(?i)^\s*(?:(?:UNIQUE\s+)?KEY|INDEX)\s+\S+\s*\(.*\)\s*,?\s*$`)

	reTrailingCommaBeforeParen = regexp.MustCompile(`,\s*\)`)

	// ALTER TABLE tbl ADD COLUMN IF NOT EXISTS col ...
	reAlterAddColumnIfNotExists = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+(\S+)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\b`)

	// ALTER TABLE tbl ADD INDEX IF NOT EXISTS idx_name (col1, col2 DESC)
	reAlterAddIndex = regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+(\S+)\s+ADD\s+(?:UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS\s+(\S+)\s*\(([^)]+)\)`)

	// Strip DESC/ASC from index column lists (SQLite ignores ordering anyway).
	reDescInCols = regexp.MustCompile(`(?i)\s+(?:DESC|ASC)\b`)

	// AFTER <identifier> in ADD COLUMN — MySQL column positioning; not supported by SQLite.
	reAlterAfterCol = regexp.MustCompile(`(?i)\s+AFTER\s+\S+`)
)

func removeForeignKeyLines(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if reConstraintFK.MatchString(line) || reInlineKey.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func cleanTrailingCommas(s string) string {
	prev := ""
	for prev != s {
		prev = s
		s = reTrailingCommaBeforeParen.ReplaceAllString(s, ")")
	}
	return s
}

// splitStatements splits SQL on semicolons, respecting string literals and line comments.
func splitStatements(sql string) []string {
	var stmts []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	inLineComment := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if inLineComment {
			buf.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if ch == '-' && !inSingle && !inDouble && i+1 < len(sql) && sql[i+1] == '-' {
			inLineComment = true
			buf.WriteByte(ch)
			continue
		}
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == ';' && !inSingle && !inDouble:
			if stmt := strings.TrimSpace(buf.String()); stmt != "" {
				stmts = append(stmts, stmt)
			}
			buf.Reset()
			continue
		}
		buf.WriteByte(ch)
	}
	if stmt := strings.TrimSpace(buf.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
