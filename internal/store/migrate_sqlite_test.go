package store

import (
	"strings"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single statement no semicolon",
			input: "SELECT 1",
			want:  []string{"SELECT 1"},
		},
		{
			name:  "two statements",
			input: "SELECT 1; SELECT 2",
			want:  []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:  "semicolon inside string literal preserved",
			input: `INSERT INTO t VALUES ('a;b')`,
			want:  []string{`INSERT INTO t VALUES ('a;b')`},
		},
		{
			name:  "line comment with semicolon not split",
			input: "SELECT 1 -- comment; still comment\nSELECT 2",
			want:  []string{"SELECT 1 -- comment; still comment\nSELECT 2"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "trailing semicolon only",
			input: "SELECT 1;",
			want:  []string{"SELECT 1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements, want %d: %v", len(got), len(tc.want), got)
			}
			for i, s := range got {
				if s != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, s, tc.want[i])
				}
			}
		})
	}
}

func TestMySQLToSQLiteRules(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string // non-empty: output must contain
		excludes string // non-empty: output must NOT contain
	}{
		{
			name:     "backtick removal",
			input:    "CREATE TABLE `users` (`id` INTEGER)",
			excludes: "`",
		},
		{
			name:     "INSERT IGNORE",
			input:    "INSERT IGNORE INTO t (c) VALUES (1)",
			contains: "INSERT OR IGNORE",
			excludes: "INSERT IGNORE",
		},
		{
			name:     "ENGINE stripped",
			input:    "CREATE TABLE t (id INTEGER) ENGINE=InnoDB",
			excludes: "ENGINE",
		},
		{
			name:     "BIGINT AUTO_INCREMENT PRIMARY KEY",
			input:    "CREATE TABLE t (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY)",
			contains: "INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT",
			excludes: "BIGINT",
		},
		{
			name:     "TIMESTAMP(6) to TEXT",
			input:    "CREATE TABLE t (created_at TIMESTAMP(6) NOT NULL)",
			contains: "TEXT NOT NULL",
			excludes: "TIMESTAMP(6)",
		},
		{
			name:     "ON UPDATE stripped",
			input:    "CREATE TABLE t (u TEXT ON UPDATE CURRENT_TIMESTAMP(6))",
			excludes: "ON UPDATE",
		},
		{
			name:     "ENUM to TEXT",
			input:    "CREATE TABLE t (status ENUM('active','inactive'))",
			contains: "TEXT",
			excludes: "ENUM",
		},
		{
			name:     "TINYINT(1) to INTEGER",
			input:    "CREATE TABLE t (flag TINYINT(1))",
			contains: "INTEGER",
			excludes: "TINYINT(1)",
		},
		{
			name:     "MEDIUMTEXT to TEXT",
			input:    "CREATE TABLE t (body MEDIUMTEXT)",
			contains: "TEXT",
			excludes: "MEDIUMTEXT",
		},
		{
			name:     "column COMMENT stripped",
			input:    "CREATE TABLE t (name TEXT COMMENT 'the name')",
			excludes: "COMMENT 'the name'",
		},
		{
			name:     "ADD COLUMN IF NOT EXISTS",
			input:    "ALTER TABLE events ADD COLUMN IF NOT EXISTS extra TEXT",
			contains: "ADD COLUMN",
			excludes: "IF NOT EXISTS",
		},
		{
			name:     "AFTER col stripped",
			input:    "ALTER TABLE t ADD COLUMN x TEXT AFTER y",
			excludes: "AFTER",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mysqlToSQLite(tc.input)
			if tc.contains != "" && !strings.Contains(got, tc.contains) {
				t.Errorf("output does not contain %q:\n%s", tc.contains, got)
			}
			if tc.excludes != "" && strings.Contains(got, tc.excludes) {
				t.Errorf("output should not contain %q:\n%s", tc.excludes, got)
			}
		})
	}
}

func TestMySQLToSQLiteAlterAddIndex(t *testing.T) {
	input := "ALTER TABLE events ADD INDEX IF NOT EXISTS idx_ticker (ticker, filed_at DESC)"
	got := mysqlToSQLite(input)
	if !strings.HasPrefix(got, "CREATE INDEX IF NOT EXISTS") {
		t.Errorf("expected CREATE INDEX, got: %s", got)
	}
	if strings.Contains(got, "DESC") {
		t.Errorf("DESC should be stripped from index columns: %s", got)
	}
}

func TestCleanTrailingCommas(t *testing.T) {
	input := "CREATE TABLE t (a TEXT, b TEXT, )"
	got := cleanTrailingCommas(input)
	if strings.Contains(got, ",)") || strings.Contains(got, ", )") {
		t.Errorf("trailing comma before paren not cleaned: %q", got)
	}
}
