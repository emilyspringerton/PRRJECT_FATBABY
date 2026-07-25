package prwatch

import (
	"os"
	"path/filepath"
	"testing"
)

type testLogger struct{ t *testing.T }

func (l *testLogger) Printf(format string, args ...any) { l.t.Logf(format, args...) }

func TestDiscoveryCursor_MissingFileDefaultsToOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", ".cursor")
	got := loadDiscoveryCursor(path, &testLogger{t})
	if got != 1 {
		t.Fatalf("expected 1 for a missing cursor file, got %d", got)
	}
}

func TestDiscoveryCursor_SaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prwatch-body", ".cursor")
	saveDiscoveryCursor(path, 4284, &testLogger{t})

	got := loadDiscoveryCursor(path, &testLogger{t})
	if got != 4284 {
		t.Fatalf("expected the saved value 4284 back, got %d", got)
	}
}

func TestDiscoveryCursor_CorruptFileDefaultsToOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cursor")
	if err := os.WriteFile(path, []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	got := loadDiscoveryCursor(path, &testLogger{t})
	if got != 1 {
		t.Fatalf("expected 1 for a corrupt cursor file, not a crash or garbage value, got %d", got)
	}
}
