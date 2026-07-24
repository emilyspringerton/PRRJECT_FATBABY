package companybios

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBiosFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRefresh_MissingFile(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh on missing file: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("count = %d, want 0", s.Count())
	}
	if got := s.Bio("AAPL"); got != "" {
		t.Errorf("Bio(AAPL) = %q, want empty", got)
	}
}

func TestRefresh_LoadsBios(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company_bios.json")
	writeBiosFile(t, path, `{"bios": {"AAPL": "Apple bio text.", "TRI": "Thomson Reuters bio text."}}`)

	s := NewStore(path)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if s.Count() != 2 {
		t.Errorf("count = %d, want 2", s.Count())
	}
	if got := s.Bio("AAPL"); got != "Apple bio text." {
		t.Errorf("Bio(AAPL) = %q", got)
	}
	// Case-insensitive lookup.
	if got := s.Bio("tri"); got != "Thomson Reuters bio text." {
		t.Errorf("Bio(tri) = %q, want case-insensitive match", got)
	}
	if got := s.Bio("MSFT"); got != "" {
		t.Errorf("Bio(MSFT) = %q, want empty for ticker with no bio", got)
	}
}
