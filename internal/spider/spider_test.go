package spider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/streamlog"
)

func testServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestFetchReturnsPage(t *testing.T) {
	srv := testServer(`<html><head><title>Test Page</title></head><body><p>Hello world</p><a href="http://example.com">link</a></body></html>`, 200)
	defer srv.Close()

	s := &Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}
	page, err := s.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if page.StatusCode != 200 {
		t.Errorf("expected 200, got %d", page.StatusCode)
	}
	if page.Title != "Test Page" {
		t.Errorf("expected title, got %q", page.Title)
	}
	if !strings.Contains(page.BodyText, "Hello world") {
		t.Errorf("body text missing: %q", page.BodyText)
	}
	if len(page.Links) == 0 {
		t.Errorf("expected at least one link")
	}
}

func TestFetchNon200(t *testing.T) {
	srv := testServer("not found", 404)
	defer srv.Close()

	s := &Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}
	page, err := s.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.StatusCode != 404 {
		t.Errorf("expected 404, got %d", page.StatusCode)
	}
}

func TestFetchMultiStreams(t *testing.T) {
	srv := testServer("<html><body>ok</body></html>", 200)
	defer srv.Close()

	s := &Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}
	urls := []string{srv.URL, srv.URL}
	ch := s.FetchMulti(context.Background(), urls)

	var results []Result
	for r := range ch {
		results = append(results, r)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestFetchContextCancel(t *testing.T) {
	srv := testServer("<html><body>slow</body></html>", 200)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &Spider{Log: streamlog.Discard(), RateLimit: 5 * time.Second}
	_, err := s.Fetch(ctx, srv.URL)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestStripTags(t *testing.T) {
	result := stripTags("<p>Hello <b>world</b></p>")
	if strings.Contains(result, "<") || strings.Contains(result, ">") {
		t.Errorf("tags not stripped: %q", result)
	}
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "world") {
		t.Errorf("text missing: %q", result)
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct{ html, want string }{
		{`<html><head><title>My Title</title></head></html>`, "My Title"},
		{`<title>  Spaced  </title>`, "Spaced"},
		{`<html><body>no title</body></html>`, ""},
	}
	for _, c := range cases {
		got := extractTitle(c.html)
		if got != c.want {
			t.Errorf("extractTitle(%q) = %q, want %q", c.html, got, c.want)
		}
	}
}

func TestExtractLinks(t *testing.T) {
	html := `<a href="http://example.com">ext</a><a href="/path">rel</a><a href="#anchor">frag</a>`
	links := extractLinks(html, "http://base.com")
	found := map[string]bool{}
	for _, l := range links {
		found[l] = true
	}
	if !found["http://example.com"] {
		t.Error("expected absolute link")
	}
	if !found["http://base.com/path"] {
		t.Errorf("expected resolved relative link, got: %v", links)
	}
	for _, l := range links {
		if l == "#anchor" {
			t.Error("fragment should be skipped")
		}
	}
}
