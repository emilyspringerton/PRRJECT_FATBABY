// movers-watcher fetches Yahoo's free market-wide day_gainers/day_losers
// screener, records a raw snapshot into the event store, and publishes a
// daily "Stocks on the Move" article via newssite's commentary API.
//
// Meant to run once per day (~9:30-9:45am ET) from a systemd timer, same
// idiom as cmd/earnings-alert's weekly timer -- not a long-running poll
// loop. Gates on marketcal.IsMarketDay itself so a timer misfire or a
// manual run on a holiday/weekend still correctly no-ops instead of
// publishing a garbage article.
//
// See PRRJECT_FATBABY/docs/northstar/auto-generated-articles.md (Phase 1).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/marketcal"
	"github.com/example/prrject-fatbaby/internal/movers"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "eventstore root")
	watchlistPath := flag.String("watchlist", "config/watchlist.json", "path to watchlist.json")
	count := flag.Int("count", 15, "number of gainers/losers to fetch and publish")
	commentaryURL := flag.String("commentary-url", "http://localhost:8082/api/commentary", "newssite POST /api/commentary endpoint")
	force := flag.Bool("force", false, "publish even if today is not a recognized market day (testing only)")
	dryRun := flag.Bool("dry-run", false, "fetch and build the article but do not post it or write to the event store")
	flag.Parse()

	logger := log.New(os.Stdout, "movers-watcher ", log.LstdFlags|log.LUTC)
	now := time.Now().UTC()

	if !*force && !marketcal.IsMarketDay(now) {
		if name := marketcal.HolidayName(now); name != "" {
			logger.Printf("not a market day (%s) -- skipping, no article published", name)
		} else {
			logger.Printf("not a market day (weekend) -- skipping, no article published")
		}
		return
	}

	ctx := context.Background()

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	wl, err := secwatch.LoadWatchlist(*watchlistPath)
	if err != nil {
		logger.Fatalf("load watchlist: %v", err)
	}
	tracked := make(map[string]bool, len(wl.Entries))
	for _, e := range wl.Entries {
		if e.Enabled {
			tracked[e.Ticker] = true
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	snap, err := movers.FetchSnapshot(ctx, client, *count)
	if err != nil {
		logger.Fatalf("fetch movers snapshot: %v", err)
	}
	logger.Printf("fetched snapshot gainers=%d losers=%d", len(snap.Gainers), len(snap.Losers))

	if *dryRun {
		fmt.Println(buildArticleBody(snap, tracked, now))
		return
	}

	if err := emitSnapshotEvent(ctx, store, snap); err != nil {
		logger.Printf("WARNING: failed to record snapshot event (continuing anyway): %v", err)
	}

	art := buildArticle(snap, tracked, now)
	if err := postCommentary(ctx, client, *commentaryURL, art); err != nil {
		logger.Fatalf("publish article: %v", err)
	}
	logger.Printf("published %s", art["id"])
}

// emitSnapshotEvent records the raw fetched quotes into the event store as
// an audit trail, same append-only pattern as every other watcher in this
// pipeline -- independent of whether article generation/publishing below
// succeeds.
func emitSnapshotEvent(ctx context.Context, store eventstore.EventStore, snap movers.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = store.Append(ctx, eventstore.Event{
		ID:         "market_movers_snapshot:" + snap.FetchedAt.Format("2006-01-02T15-04-05"),
		Type:       "market_movers_snapshot",
		Source:     "movers-watcher",
		OccurredAt: snap.FetchedAt,
		Data:       data,
	})
	return err
}

// commentaryArticle mirrors internal/newssite/commentary.Article's JSON
// shape without importing the newssite module tree from a cmd/ package --
// movers-watcher talks to newssite over HTTP (POST /api/commentary), the
// same boundary every other watcher uses, not a direct Go import.
type commentaryArticle map[string]any

func buildArticle(snap movers.Snapshot, tracked map[string]bool, now time.Time) commentaryArticle {
	dateStr := now.Format("January 2, 2006")
	id := "movers-" + now.Format("2006-01-02")
	headline := "Stocks on the Move — " + dateStr
	body := buildArticleBody(snap, tracked, now)
	preview := "Today's biggest market-wide gainers and losers, tracked live."

	return commentaryArticle{
		"id":           id,
		"headline":     headline,
		"body":         body,
		"preview":      preview,
		"byline":       "The Markets Desk",
		"kind":         "market_movers",
		"published_at": now.Format(time.RFC3339),
	}
}

func buildArticleBody(snap movers.Snapshot, tracked map[string]bool, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stocks on the Move — %s\n\n", now.Format("January 2, 2006"))
	fmt.Fprintf(&b, "A look at today's biggest market-wide gainers and losers, sourced live "+
		"from the market. Names we track closely for filings and signals are marked below; "+
		"everything else here is price action alone.\n\n")

	writeMoverSection(&b, "TOP GAINERS", snap.Gainers, tracked)
	writeMoverSection(&b, "TOP LOSERS", snap.Losers, tracked)

	return b.String()
}

func writeMoverSection(b *strings.Builder, title string, quotes []movers.Quote, tracked map[string]bool) {
	fmt.Fprintf(b, "%s\n\n", title)
	if len(quotes) == 0 {
		fmt.Fprintf(b, "No qualifying names today.\n\n")
		return
	}
	sorted := make([]movers.Quote, len(quotes))
	copy(sorted, quotes)
	sort.Slice(sorted, func(i, j int) bool {
		return abs(sorted[i].ChangePercent) > abs(sorted[j].ChangePercent)
	})
	for _, q := range sorted {
		sign := ""
		if q.ChangePercent > 0 {
			sign = "+"
		}
		coverage := ""
		if tracked[q.Symbol] {
			coverage = " (tracked — see filings and signal history on its ticker page)"
		}
		fmt.Fprintf(b, "%s (%s) — %s%.2f%%, $%.2f, volume %s%s\n",
			q.Name, q.Symbol, sign, q.ChangePercent, q.Price, formatVolume(q.Volume), coverage)
	}
	fmt.Fprintf(b, "\n")
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func formatVolume(v int64) string {
	return strconv.FormatInt(v, 10)
}

func postCommentary(ctx context.Context, client *http.Client, url string, art commentaryArticle) error {
	payload, err := json.Marshal(art)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post commentary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("post commentary: status=%d", resp.StatusCode)
	}
	return nil
}
