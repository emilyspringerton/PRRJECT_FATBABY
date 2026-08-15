package fedwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sampleFeed is a trimmed real capture of the Fed's actual RSS feed,
// fetched live 2026-08-15 -- same "real fixture, not invented" discipline
// as internal/bonddata's own CSV test fixtures.
const sampleFeed = `<?xml version="1.0" encoding="utf-8" ?>
<rss version="2.0">
    <channel>
        <title>FRB: Press Release - Monetary Policy</title>
        <link><![CDATA[https://www.federalreserve.gov/feeds/feeds.htm]]></link>
        <description><![CDATA[Press releases about monetary policy from the Federal Reserve Board]]></description>
        <language>en</language>
        <item>
            <title>Federal Reserve issues FOMC statement</title>
            <link><![CDATA[https://www.federalreserve.gov/newsevents/pressreleases/monetary20260729a.htm]]></link>
            <guid><![CDATA[https://www.federalreserve.gov/newsevents/pressreleases/monetary20260729a.htm]]></guid>
            <description><![CDATA[Federal Reserve issues FOMC statement]]></description>
            <category>Monetary Policy</category>
            <pubDate><![CDATA[Wed, 29 Jul 2026 18:00:00 GMT]]></pubDate>
        </item>
        <item>
            <title>Minutes of the Federal Open Market Committee, June 16-17, 2026</title>
            <link><![CDATA[https://www.federalreserve.gov/newsevents/pressreleases/monetary20260708a.htm]]></link>
            <guid><![CDATA[https://www.federalreserve.gov/newsevents/pressreleases/monetary20260708a.htm]]></guid>
            <description><![CDATA[Minutes of the Federal Open Market Committee, June 16-17, 2026]]></description>
            <category>Monetary Policy</category>
            <pubDate><![CDATA[Wed, 8 Jul 2026 18:00:00 GMT]]></pubDate>
        </item>
    </channel>
</rss>`

func testServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscover_ParsesRealFeedShape(t *testing.T) {
	srv := testServer(t, sampleFeed, http.StatusOK)
	c := NewClient(ClientConfig{FeedURL: srv.URL})

	items, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first := items[0]
	if first.Title != "Federal Reserve issues FOMC statement" {
		t.Errorf("Title = %q", first.Title)
	}
	if first.URL != "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260729a.htm" {
		t.Errorf("URL = %q", first.URL)
	}
	if first.ID != first.URL {
		t.Errorf("ID = %q, want it to equal URL (guid==link in this feed)", first.ID)
	}
	if first.Category != "Monetary Policy" {
		t.Errorf("Category = %q", first.Category)
	}
	wantTime := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	if !first.PublishedAt.Equal(wantTime) {
		t.Errorf("PublishedAt = %v, want %v", first.PublishedAt, wantTime)
	}
}

func TestDiscover_EmptyFeedReturnsEmptySlice(t *testing.T) {
	srv := testServer(t, `<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`, http.StatusOK)
	c := NewClient(ClientConfig{FeedURL: srv.URL})

	items, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestDiscover_ServerErrorReturnsError(t *testing.T) {
	srv := testServer(t, "internal error", http.StatusInternalServerError)
	c := NewClient(ClientConfig{
		FeedURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	})

	// httpretry retries 3x on a 500; keep this test fast by not waiting
	// for real backoff -- just confirm the eventual error surfaces.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := c.Discover(ctx)
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
}

func TestDiscover_MalformedXMLReturnsError(t *testing.T) {
	srv := testServer(t, "not xml at all {{{", http.StatusOK)
	c := NewClient(ClientConfig{FeedURL: srv.URL})

	_, err := c.Discover(context.Background())
	if err == nil {
		t.Fatal("expected an error for malformed XML")
	}
}

func TestDiscover_ItemWithoutGUIDFallsBackToLink(t *testing.T) {
	feed := `<?xml version="1.0"?><rss version="2.0"><channel><item>
		<title>No GUID here</title>
		<link>https://www.federalreserve.gov/example.htm</link>
		<pubDate>Wed, 29 Jul 2026 18:00:00 GMT</pubDate>
	</item></channel></rss>`
	srv := testServer(t, feed, http.StatusOK)
	c := NewClient(ClientConfig{FeedURL: srv.URL})

	items, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ID != "https://www.federalreserve.gov/example.htm" {
		t.Errorf("ID = %q, want fallback to Link", items[0].ID)
	}
}

func TestDiscover_ItemWithNoIdentitySkipped(t *testing.T) {
	feed := `<?xml version="1.0"?><rss version="2.0"><channel><item>
		<title>No identity at all</title>
	</item></channel></rss>`
	srv := testServer(t, feed, http.StatusOK)
	c := NewClient(ClientConfig{FeedURL: srv.URL})

	items, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected item with no guid/link to be skipped, got %d items", len(items))
	}
}
