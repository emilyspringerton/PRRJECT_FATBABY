package newssite

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/edition"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
)

// frictionThreshold is the approval-pct below which a director is flagged.
const frictionThreshold = 0.90

// ── Base ──────────────────────────────────────────────────────────────────────

// Base contains fields present on every page view model.
// All page view models embed this struct so the shared "masthead" template
// can access .Symbols and the "footer" template gets its fields.
type Base struct {
	Symbols []string // known ticker symbols for the <datalist>
}

// ── Shared article view ───────────────────────────────────────────────────────

// ArticleView is the view model for one article card (signal or document).
type ArticleView struct {
	Identity    string // non-empty for source documents
	Link        string // href for the headline (always set)
	Kicker      string
	KickerClass string
	Headline    string
	Dateline    string
	Byline      string
	Deck        string
}

// ── Front page ────────────────────────────────────────────────────────────────

// SuccessionWatchItem is one entry in the "succession watch" rail on the front page.
// It surfaces director_decay signals framed as a forward-looking succession story.
type SuccessionWatchItem struct {
	Name        string
	CanonicalID string
	Ticker      string
	ApprovalStr string
	Deck        string
	Link        string
}

type FrontPageView struct {
	Base
	Date            string
	Count           int
	TickerItems     []TickerTapeItem
	Lead            *ArticleView
	Secondary       []ArticleView
	MostActive      []ActiveTickerView
	WireItems       []WireItem
	Rest            []ArticleView
	SuccessionWatch []SuccessionWatchItem
}

type TickerTapeItem struct {
	Ticker string
	Label  string
}

type ActiveTickerView struct {
	Ticker   string
	DocCount int
}

type WireItem struct {
	Identity string
	Ticker   string
	Form     string
	DateStr  string
}

// ── Detail page ───────────────────────────────────────────────────────────────

type DetailPageView struct {
	Base
	Title          string
	Headline       string
	Kicker         string
	KickerClass    string
	Dateline       string
	Byline         string
	Form           string
	SourceLabel    string
	DateStr        string
	CharCount      int
	DocumentURL    string
	IsExternalLink bool
	FullText       string
	Ticker         string
}

// ── Breaking page ─────────────────────────────────────────────────────────────

type BreakingView struct {
	Base
	Items []ArticleView
}

// ── Section page ─────────────────────────────────────────────────────────────

type SectionView struct {
	Base
	Slug  string
	Title string
	Blurb string
	Items []ArticleView
}

// ── Ticker page ───────────────────────────────────────────────────────────────

type DirectorView struct {
	Name        string
	CanonicalID string
	ApprovalStr string // "85%" or "" if unknown
	HasFriction bool
}

type TickerFactBox struct {
	TotalSignals  int
	CritHighCount int
	DirectorCount int
	DocCount      int
}

type TickerPageView struct {
	Base
	Symbol      string
	Auditor     string
	LastActivity string // formatted date
	Forms       string // comma-joined
	Lead        *ArticleView
	Signals     []ArticleView
	Directors   []DirectorView
	Docs        []ArticleView
	Wire        []ArticleView
	Facts       TickerFactBox
}

// ── Ticker 404 ────────────────────────────────────────────────────────────────

type Ticker404View struct {
	Base
	Symbol  string
	Nearest []string
}

// ── Tickers directory ─────────────────────────────────────────────────────────

type TickerDirRowView struct {
	Symbol        string
	MaxSeverity   string
	TotalSignals  int
	DocCount      int
	DirectorCount int
	LatestStr     string
}

type TickerDirView struct {
	Base
	Rows    []TickerDirRowView
	Total   int
	ByAlpha bool
}

// ── Search page ───────────────────────────────────────────────────────────────

type SearchResultView struct {
	Symbol      string
	MaxSeverity string
	SignalCount  int
	LatestStr   string
}

type SearchView struct {
	Base
	Query   string
	Matches []SearchResultView
}

// ── Archive page ──────────────────────────────────────────────────────────────

type ArchiveView struct {
	Base
	Entries []ArticleView
}

// ── About page ────────────────────────────────────────────────────────────────

type AboutView struct {
	Base
}

// ── Person (director dossier) page ────────────────────────────────────────────

// AppearanceView is one filing entry in the person page vote-history table.
type AppearanceView struct {
	Ticker         string
	FilingDate     string // YYYY-MM-DD, for display
	DateStr        string // "21 May 2026"
	Form           string
	ApprovalStr    string // "97.9%"
	ApprovalPct    float64
	ForVotes       string
	AgainstVotes   string
	AbstainVotes   string
	BrokerNonVotes string
	IsFriction     bool
}

// BoardEntryView groups all appearances of a person at one ticker.
type BoardEntryView struct {
	Ticker        string
	Appearances   []AppearanceView
	LatestApprStr string
	HasFriction   bool
}

// InterlockView is one co-director relationship surfaced on the person page.
type InterlockView struct {
	Name        string
	CanonicalID string
	SharedBoards []string // tickers where they co-appear
}

// PersonPageView is the view model for /person/{canonical_id}.
type PersonPageView struct {
	Base
	Name        string
	CanonicalID string
	Role        string
	Centrality  int    // number of distinct boards
	FirstSeen   string // formatted
	LastSeen    string // formatted
	FilingCount int
	Boards      []BoardEntryView
	Signals     []ArticleView
	Interlocks  []InterlockView
	Sparkline   SparklineView
}

// ── Render entry points ───────────────────────────────────────────────────────

func RenderListPage(w io.Writer, entries []DocEntry, ranked []edition.Ranked, symbols []string) {
	view := buildFrontPage(entries, ranked)
	view.Symbols = symbols
	if err := frontTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderDetailPage(w io.Writer, entry DocEntry, symbols []string) {
	view := buildDetailPage(entry)
	view.Symbols = symbols
	if err := detailTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderBreakingPage(w io.Writer, ranked []edition.Ranked, symbols []string) {
	items := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, signalToArticleView(r, 300))
	}
	if err := breakTmpl.Execute(w, BreakingView{Base: Base{Symbols: symbols}, Items: items}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// LiveView is the view model for /live.
type LiveView struct {
	Base
	Items []ArticleView
}

func RenderLivePage(w io.Writer, ranked []edition.Ranked, symbols []string) {
	items := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, signalToArticleView(r, 300))
	}
	view := LiveView{Base: Base{Symbols: symbols}, Items: items}
	if err := liveTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderSectionPage(w io.Writer, slug string, ranked []edition.Ranked, symbols []string) {
	items := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, signalToArticleView(r, 300))
	}
	view := SectionView{
		Base:  Base{Symbols: symbols},
		Slug:  slug,
		Title: sectionTitle(slug),
		Blurb: sectionBlurb(slug),
		Items: items,
	}
	if err := sectionTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderTickerPage(w io.Writer, symbol string, row *catalog.TickerRow,
	ranked []edition.Ranked, directors []*entitygraph.PersonNode,
	secDocs []DocEntry, wireDocs []DocEntry, symbols []string) {

	var lead *ArticleView
	var signals []ArticleView

	if len(ranked) > 0 {
		av := signalToArticleView(ranked[0], 400)
		lead = &av
		for _, r := range ranked[1:] {
			signals = append(signals, signalToArticleView(r, 200))
		}
	} else if len(secDocs) > 0 {
		av := docToArticleView(secDocs[0], 400)
		lead = &av
	}

	// Directors sorted by approval pct asc (friction first).
	type dirWithPct struct {
		node *entitygraph.PersonNode
		pct  float64
	}
	dps := make([]dirWithPct, 0, len(directors))
	for _, n := range directors {
		dps = append(dps, dirWithPct{n, latestApprovalAt(n, symbol)})
	}
	sort.Slice(dps, func(i, j int) bool {
		pi, pj := dps[i].pct, dps[j].pct
		if pi < 0 {
			pi = 2 // unknown goes last
		}
		if pj < 0 {
			pj = 2
		}
		return pi < pj
	})
	dirViews := make([]DirectorView, 0, len(dps))
	for _, dp := range dps {
		dv := DirectorView{
			Name:        dp.node.Name,
			CanonicalID: dp.node.CanonicalID,
			HasFriction: dp.pct >= 0 && dp.pct < frictionThreshold,
		}
		if dp.pct >= 0 {
			dv.ApprovalStr = fmt.Sprintf("%.0f%%", dp.pct*100)
		}
		dirViews = append(dirViews, dv)
	}

	// Docs as article views.
	secViews := make([]ArticleView, 0, len(secDocs))
	for _, d := range secDocs {
		secViews = append(secViews, docToArticleView(d, 140))
	}
	wireViews := make([]ArticleView, 0, len(wireDocs))
	for _, d := range wireDocs {
		wireViews = append(wireViews, docToArticleView(d, 0))
	}

	// Fact box.
	critHigh := 0
	for _, r := range ranked {
		if r.Signal.Severity == "critical" || r.Signal.Severity == "high" {
			critHigh++
		}
	}

	var lastActivityStr, auditor, forms string
	if row != nil {
		if !row.LatestActivity.IsZero() {
			lastActivityStr = row.LatestActivity.UTC().Format("2 Jan 2006")
		}
		auditor = row.Auditor
		forms = strings.Join(row.Forms, ", ")
	}

	facts := TickerFactBox{
		TotalSignals:  len(ranked) + len(signals),
		CritHighCount: critHigh,
		DirectorCount: len(directors),
		DocCount:      len(secDocs) + len(wireDocs),
	}
	if row != nil {
		facts.TotalSignals = row.SignalCount + row.GovSignals
		facts.DocCount = row.DocCount
		facts.DirectorCount = row.DirectorCount
	}

	view := TickerPageView{
		Base:         Base{Symbols: symbols},
		Symbol:       symbol,
		Auditor:      auditor,
		LastActivity: lastActivityStr,
		Forms:        forms,
		Lead:         lead,
		Signals:      signals,
		Directors:    dirViews,
		Docs:         secViews,
		Wire:         wireViews,
		Facts:        facts,
	}
	if err := tickerTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderTicker404Page(w io.Writer, symbol string, nearest []string, symbols []string) {
	view := Ticker404View{Base: Base{Symbols: symbols}, Symbol: symbol, Nearest: nearest}
	if err := ticker404Tmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderTickersPage(w io.Writer, rows []*catalog.TickerRow, byAlpha bool, symbols []string) {
	rowViews := make([]TickerDirRowView, 0, len(rows))
	for _, r := range rows {
		latestStr := ""
		if !r.LatestActivity.IsZero() {
			latestStr = r.LatestActivity.UTC().Format("2 Jan 2006")
		}
		rowViews = append(rowViews, TickerDirRowView{
			Symbol:        r.Symbol,
			MaxSeverity:   r.MaxSeverity,
			TotalSignals:  r.SignalCount + r.GovSignals,
			DocCount:      r.DocCount,
			DirectorCount: r.DirectorCount,
			LatestStr:     latestStr,
		})
	}
	view := TickerDirView{
		Base:    Base{Symbols: symbols},
		Rows:    rowViews,
		Total:   len(rows),
		ByAlpha: byAlpha,
	}
	if err := tickersTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderSearchPage(w io.Writer, query string, results []catalog.SearchResult, symbols []string) {
	views := make([]SearchResultView, 0, len(results))
	for _, r := range results {
		latestStr := ""
		if !r.LatestActivity.IsZero() {
			latestStr = r.LatestActivity.UTC().Format("2 Jan 2006")
		}
		views = append(views, SearchResultView{
			Symbol:      r.Symbol,
			MaxSeverity: r.MaxSeverity,
			SignalCount: r.SignalCount,
			LatestStr:   latestStr,
		})
	}
	view := SearchView{Base: Base{Symbols: symbols}, Query: query, Matches: views}
	if err := searchTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderArchivePage(w io.Writer, entries []DocEntry, symbols []string) {
	avs := make([]ArticleView, 0, len(entries))
	for _, e := range entries {
		avs = append(avs, docToArticleView(e, 0))
	}
	if err := archiveTmpl.Execute(w, ArchiveView{Base: Base{Symbols: symbols}, Entries: avs}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

func RenderAboutPage(w io.Writer, symbols []string) {
	if err := aboutTmpl.Execute(w, AboutView{Base: Base{Symbols: symbols}}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// ── View builders ─────────────────────────────────────────────────────────────

func buildFrontPage(entries []DocEntry, ranked []edition.Ranked) FrontPageView {
	// Suppress historical articles from the front page. Backfilling a new ticker
	// can add years of archived filings; those should appear on the ticker page
	// only, not flood the front-page feed with fake "breaking" dates.
	freshEntries := entries[:0:0]
	for _, e := range entries {
		fd := e.FilingDate
		if fd == "" {
			fd = e.PersistedAt.Format("2006-01-02")
		}
		if !isHistoricalDateStr(fd) {
			freshEntries = append(freshEntries, e)
		}
	}
	entries = freshEntries

	freshRanked := ranked[:0:0]
	for _, r := range ranked {
		fd := r.Signal.FilingDate
		if fd == "" {
			fd = r.Signal.DetectedAt
		}
		if !isHistoricalDateStr(fd) {
			freshRanked = append(freshRanked, r)
		}
	}
	ranked = freshRanked

	tickerCount := map[string]int{}
	var wireEntries []DocEntry
	for _, e := range entries {
		t := normTicker(e.Ticker)
		tickerCount[t]++
		if e.SourceType == "press_release" {
			wireEntries = append(wireEntries, e)
		}
	}

	type kv struct {
		k string
		v int
	}
	kvs := make([]kv, 0, len(tickerCount))
	for k, v := range tickerCount {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].v != kvs[j].v {
			return kvs[i].v > kvs[j].v
		}
		return kvs[i].k < kvs[j].k
	})
	mostActive := make([]ActiveTickerView, 0, 6)
	for i, item := range kvs {
		if i >= 6 {
			break
		}
		mostActive = append(mostActive, ActiveTickerView{Ticker: item.k, DocCount: item.v})
	}

	tapeSeen := map[string]bool{}
	var tickerItems []TickerTapeItem
	for _, r := range ranked {
		if r.Signal.Severity != "critical" && r.Signal.Severity != "high" {
			continue
		}
		t := normTicker(r.Signal.Ticker)
		if tapeSeen[t] {
			continue
		}
		tapeSeen[t] = true
		tickerItems = append(tickerItems, TickerTapeItem{Ticker: t, Label: r.Headline})
		if len(tickerItems) >= 8 {
			break
		}
	}
	for _, e := range entries {
		if len(tickerItems) >= 12 {
			break
		}
		t := normTicker(e.Ticker)
		if tapeSeen[t] {
			continue
		}
		tapeSeen[t] = true
		label := sourceTypeLabel(e.SourceType)
		if e.Form != "" {
			label = e.Form
		}
		tickerItems = append(tickerItems, TickerTapeItem{Ticker: t, Label: label})
	}

	var wireItems []WireItem
	for i, e := range wireEntries {
		if i >= 3 {
			break
		}
		wireItems = append(wireItems, WireItem{
			Identity: e.Identity,
			Ticker:   normTicker(e.Ticker),
			Form:     e.Form,
			DateStr:  displayDateShort(e),
		})
	}

	var lead *ArticleView
	var secondary, rest []ArticleView

	signalViews := make([]ArticleView, 0, 4)
	for i, r := range ranked {
		if i >= 4 {
			break
		}
		av := signalToArticleView(r, 400)
		if i > 0 {
			av.Deck = truncateRunes(av.Deck, 160)
		}
		signalViews = append(signalViews, av)
	}

	if len(signalViews) == 0 {
		for i, e := range entries {
			av := docToArticleView(e, 400)
			if i == 0 {
				lead = &av
			} else if i < 4 {
				secondary = append(secondary, docToArticleView(e, 160))
			} else {
				rest = append(rest, docToArticleView(e, 120))
			}
		}
	} else {
		lead = &signalViews[0]
		secondary = signalViews[1:]
		for _, e := range entries {
			if len(secondary) >= 3 {
				break
			}
			if e.SourceType != "press_release" {
				secondary = append(secondary, docToArticleView(e, 160))
			}
		}
		for _, e := range entries {
			rest = append(rest, docToArticleView(e, 120))
		}
	}

	// Succession watch: up to 5 director_decay signals, framed as forward succession.
	var successionWatch []SuccessionWatchItem
	for _, r := range ranked {
		if r.Signal.Type != entitygraph.SignalDirectorDecay {
			continue
		}
		item := SuccessionWatchItem{
			Name:   r.Signal.Entity,
			Ticker: normTicker(r.Signal.Ticker),
			Deck:   truncateRunes(r.Signal.Interpretation, 120),
			Link:   "/ticker/" + normTicker(r.Signal.Ticker),
		}
		if r.Signal.Entity != "" {
			item.CanonicalID = entitygraph.Canonicalize(r.Signal.Entity)
			item.Link = "/person/" + item.CanonicalID
		}
		if r.Signal.Score > 0 {
			item.ApprovalStr = fmt.Sprintf("%.1f%%", r.Signal.Score*100)
		}
		successionWatch = append(successionWatch, item)
		if len(successionWatch) >= 5 {
			break
		}
	}

	return FrontPageView{
		Date:            time.Now().UTC().Format("Monday, 2 January 2006"),
		Count:           len(entries),
		TickerItems:     tickerItems,
		Lead:            lead,
		Secondary:       secondary,
		MostActive:      mostActive,
		WireItems:       wireItems,
		Rest:            rest,
		SuccessionWatch: successionWatch,
	}
}

func buildDetailPage(entry DocEntry) DetailPageView {
	kicker, kickerClass := docKicker(entry)
	byline := docByline(entry)
	srcLabel := sourceTypeLabel(entry.SourceType)
	formOrType := srcLabel
	if entry.Form != "" {
		formOrType = entry.Form
	}
	headline := fmt.Sprintf("%s — %s", normTicker(entry.Ticker), formOrType)
	dateline := fmt.Sprintf("%s — %s.", normTicker(entry.Ticker), displayDateFull(entry))
	return DetailPageView{
		Title:          headline,
		Headline:       headline,
		Kicker:         kicker,
		KickerClass:    kickerClass,
		Dateline:       dateline,
		Byline:         byline,
		Form:           entry.Form,
		SourceLabel:    srcLabel,
		DateStr:        formatTimeUTC(entry.PersistedAt),
		CharCount:      entry.CharCount,
		DocumentURL:    entry.DocumentURL,
		IsExternalLink: isSafeLink(entry.DocumentURL),
		FullText:       entry.FullText,
		Ticker:         normTicker(entry.Ticker),
	}
}

func signalToArticleView(r edition.Ranked, deckMax int) ArticleView {
	kicker := r.Kicker
	kickerClass := r.KickerClass
	fd := r.Signal.FilingDate
	if fd == "" {
		fd = r.Signal.DetectedAt
	}
	if isHistoricalDateStr(fd) {
		kicker = "ARCHIVE · " + kicker
		kickerClass = "kicker-archive"
	}
	return ArticleView{
		Link:        "/ticker/" + normTicker(r.Signal.Ticker),
		Kicker:      kicker,
		KickerClass: kickerClass,
		Headline:    r.Headline,
		Dateline:    r.Dateline,
		Byline:      r.Byline,
		Deck:        truncateRunes(r.Deck, deckMax),
	}
}

func docToArticleView(e DocEntry, deckMax int) ArticleView {
	kicker, kickerClass := docKicker(e)
	fd := e.FilingDate
	if fd == "" && !e.PersistedAt.IsZero() {
		fd = e.PersistedAt.Format("2006-01-02")
	}
	if isHistoricalDateStr(fd) {
		kicker = "ARCHIVE · " + kicker
		kickerClass = "kicker-archive"
	}
	srcLabel := sourceTypeLabel(e.SourceType)
	formOrType := srcLabel
	if e.Form != "" {
		formOrType = e.Form
	}
	headline := fmt.Sprintf("%s — %s", normTicker(e.Ticker), formOrType)
	dateline := fmt.Sprintf("%s — %s.", normTicker(e.Ticker), displayDateFull(e))
	byline := docByline(e)
	if e.FilingDate != "" && !e.PersistedAt.IsZero() {
		indexedStr := e.PersistedAt.UTC().Format("2 Jan 2006")
		filedStr := displayDateShort(e)
		if indexedStr != filedStr {
			byline += " · Indexed " + indexedStr
		}
	}
	deck := ""
	if deckMax > 0 {
		deck = truncateRunes(e.BodyPreview, deckMax)
	}
	return ArticleView{
		Identity:    e.Identity,
		Link:        "/doc/" + e.Identity,
		Kicker:      kicker,
		KickerClass: kickerClass,
		Headline:    headline,
		Dateline:    dateline,
		Byline:      byline,
		Deck:        deck,
	}
}

// ── RSS 2.0 feed ─────────────────────────────────────────────────────────────

type rssChannel struct {
	XMLName       xml.Name  `xml:"channel"`
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []rssItem `xml:"item"`
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Category    string `xml:"category,omitempty"`
}

// RenderRSS writes an RSS 2.0 feed for a slice of ranked signals to w.
// section is the slug for per-section feeds; empty for the front-page feed.
// baseURL is the scheme+host used to form absolute item links.
func RenderRSS(w io.Writer, ranked []edition.Ranked, section, baseURL string) error {
	title := "FATBABY Financial Intelligence"
	link := baseURL + "/"
	desc := "Governance and risk signals from SEC filings"
	if section != "" {
		title = sectionTitle(section) + " — FATBABY"
		link = baseURL + "/section/" + section
		desc = sectionBlurb(section)
		if desc == "" {
			desc = "FATBABY " + sectionTitle(section) + " signals"
		}
	}

	items := make([]rssItem, 0, len(ranked))
	for _, r := range ranked {
		itemLink := baseURL + "/ticker/" + normTicker(r.Signal.Ticker)
		pubDate := ""
		dateStr := r.Signal.FilingDate
		if dateStr == "" {
			dateStr = r.Signal.DetectedAt
		}
		if dateStr != "" {
			if t, err := time.Parse("2006-01-02", dateStr); err == nil {
				pubDate = t.UTC().Format(time.RFC1123Z)
			}
		}
		items = append(items, rssItem{
			Title:       r.Headline,
			Link:        itemLink,
			Description: truncateRunes(r.Deck, 300),
			PubDate:     pubDate,
			GUID:        r.Signal.SignalID,
			Category:    r.Section,
		})
	}

	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:         title,
			Link:          link,
			Description:   desc,
			LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
			Items:         items,
		},
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(feed)
}

// ── Front-page historical filtering ──────────────────────────────────────────

// historicalThresholdDays is the age (in days) beyond which a filing is
// considered historical and suppressed from the front-page feed. Historical
// articles are still visible on per-ticker pages.
const historicalThresholdDays = 90

// isHistoricalDateStr reports whether a YYYY-MM-DD date string represents a
// filing older than historicalThresholdDays. Returns false when the date is
// empty or unparseable so that undated documents are never silently suppressed.
func isHistoricalDateStr(dateStr string) bool {
	if dateStr == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return false
	}
	return time.Since(t) > historicalThresholdDays*24*time.Hour
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func docKicker(e DocEntry) (kicker, class string) {
	if e.SourceType == "press_release" {
		return "The Wire", "kicker-wire"
	}
	if e.Form != "" {
		return "SEC Filing · " + e.Form, "kicker-filing"
	}
	return "SEC Filing", "kicker-filing"
}

func docByline(e DocEntry) string {
	if e.SourceType == "press_release" {
		return "By the Wire Desk"
	}
	return "By SEC Watch"
}

func sourceTypeLabel(sourceType string) string {
	switch sourceType {
	case "sec_8k":
		return "SEC Filing"
	case "press_release":
		return "Press Release"
	default:
		r := strings.ReplaceAll(sourceType, "_", " ")
		if r == "" {
			return "Unknown"
		}
		return strings.Title(r) //nolint:staticcheck
	}
}

func normTicker(t string) string { return strings.ToUpper(strings.TrimSpace(t)) }

func formatTimeUTC(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.UTC().Format("2 Jan 2006 15:04 UTC")
}

func formatDateShort(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2 Jan 2006")
}

func formatDateFull(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.UTC().Format("2 January 2006")
}

// displayDateFull returns the human-readable filing date for an article dateline.
// Uses FilingDate (the SEC filing date) when present; falls back to PersistedAt
// (pipeline-index time) for legacy documents that pre-date the FilingDate field.
func displayDateFull(e DocEntry) string {
	if e.FilingDate != "" {
		if t, err := time.Parse("2006-01-02", e.FilingDate); err == nil {
			return t.UTC().Format("2 January 2006")
		}
	}
	return formatDateFull(e.PersistedAt)
}

// displayDateShort is the short form of displayDateFull (e.g. "26 Apr 2019").
func displayDateShort(e DocEntry) string {
	if e.FilingDate != "" {
		if t, err := time.Parse("2006-01-02", e.FilingDate); err == nil {
			return t.UTC().Format("2 Jan 2006")
		}
	}
	return formatDateShort(e.PersistedAt)
}

func isSafeLink(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	cut := string(runes[:maxRunes])
	if i := strings.LastIndex(cut, " "); i > 0 {
		return strings.TrimSpace(cut[:i]) + "…"
	}
	return strings.TrimSpace(cut) + "…"
}

func sectionTitle(slug string) string {
	switch slug {
	case "governance":
		return "Governance"
	case "activism":
		return "Activism Watch"
	case "boardroom":
		return "Boardroom"
	case "auditor":
		return "Auditor Watch"
	case "pay":
		return "Pay & Proxy"
	case "wire":
		return "The Wire"
	default:
		return strings.Title(slug) //nolint:staticcheck
	}
}

func sectionBlurb(slug string) string {
	switch slug {
	case "activism":
		return "Entrenchment and friction co-occurring. Base rate: ~60% for a 13D activist filing within six months."
	case "governance":
		return "Composite governance health, board entrenchment, and family-control signals."
	case "boardroom":
		return "Director approval trends, trust levels, and vote anomalies."
	case "auditor":
		return "Auditor changes are a known pre-transaction and dispute risk indicator."
	case "pay":
		return "Compensation advisory votes and broker non-vote anomalies."
	default:
		return ""
	}
}

func latestApprovalAt(node *entitygraph.PersonNode, ticker string) float64 {
	ticker = normTicker(ticker)
	var latestDate string
	pct := -1.0
	for _, f := range node.Filings {
		if normTicker(f.Ticker) != ticker {
			continue
		}
		if f.FilingDate > latestDate {
			latestDate = f.FilingDate
			pct = f.ApprovalPct
		}
	}
	return pct
}

// ── Person page render ────────────────────────────────────────────────────────

// SparklineView holds the pre-computed SVG polyline attributes for the approval
// trend chart. Width=260 Height=50; Y scaled so 100%=0 and 60%=50 (typical range).
type SparklineView struct {
	HasData      bool
	Points       string // SVG polyline points attribute, e.g. "0,12.5 130,25 260,37.5"
	ThresholdY   string // Y coordinate of the 90% friction-threshold line
	Width        int
	Height       int
}

func buildSparkline(appearances []AppearanceView) SparklineView {
	const svgW, svgH = 260, 50
	const yMin, yMax = 0.60, 1.00 // display range: 60–100%
	sv := SparklineView{Width: svgW, Height: svgH}

	// Filter to appearances that have a numeric approval pct.
	var pts []AppearanceView
	for _, a := range appearances {
		if a.ApprovalPct > 0 {
			pts = append(pts, a)
		}
	}
	if len(pts) == 0 {
		return sv
	}
	sv.HasData = true

	yOf := func(pct float64) float64 {
		clamped := pct
		if clamped > yMax {
			clamped = yMax
		}
		if clamped < yMin {
			clamped = yMin
		}
		return (1.0 - (clamped-yMin)/(yMax-yMin)) * float64(svgH)
	}

	var b strings.Builder
	for i, pt := range pts {
		var x float64
		if len(pts) == 1 {
			x = float64(svgW) / 2
		} else {
			x = float64(i) / float64(len(pts)-1) * float64(svgW)
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, yOf(pt.ApprovalPct))
	}
	sv.Points = b.String()
	sv.ThresholdY = fmt.Sprintf("%.1f", yOf(frictionThreshold))
	return sv
}

func RenderPersonPage(w io.Writer, view PersonPageView) {
	if err := personTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// buildPersonPage assembles the PersonPageView from a PersonNode, its edges,
// signals about the person, and the graph store for co-director name lookups.
func buildPersonPage(
	node *entitygraph.PersonNode,
	edges []*entitygraph.Edge,
	personSigs []entitygraph.Signal,
	today string,
	gs *graphread.Store,
	symbols []string,
) PersonPageView {
	view := PersonPageView{
		Base:        Base{Symbols: symbols},
		Name:        node.Name,
		CanonicalID: node.CanonicalID,
		Role:        string(node.Type),
		Centrality:  node.Centrality,
		FilingCount: node.FilingCount,
	}
	if node.FirstAppearance != "" {
		if t, err := time.Parse("2006-01-02", node.FirstAppearance); err == nil {
			view.FirstSeen = t.Format("2 Jan 2006")
		}
	}
	if node.LastAppearance != "" {
		if t, err := time.Parse("2006-01-02", node.LastAppearance); err == nil {
			view.LastSeen = t.Format("2 Jan 2006")
		}
	}

	// Group filings by ticker, sorted oldest-to-newest within each ticker.
	byTicker := map[string][]entitygraph.FilingAppearance{}
	for _, f := range node.Filings {
		t := normTicker(f.Ticker)
		byTicker[t] = append(byTicker[t], f)
	}
	tickerOrder := make([]string, 0, len(byTicker))
	for t := range byTicker {
		tickerOrder = append(tickerOrder, t)
	}
	sort.Strings(tickerOrder)

	var allApprearances []AppearanceView // all appearances sorted by date, for sparkline
	for _, t := range tickerOrder {
		filings := byTicker[t]
		sort.Slice(filings, func(i, j int) bool { return filings[i].FilingDate < filings[j].FilingDate })

		var latestAppr float64 = -1
		var apps []AppearanceView
		for _, f := range filings {
			av := AppearanceView{
				Ticker:     t,
				FilingDate: f.FilingDate,
				Form:       f.Form,
			}
			if t2, err := time.Parse("2006-01-02", f.FilingDate); err == nil {
				av.DateStr = t2.Format("2 Jan 2006")
			}
			if f.ApprovalPct > 0 {
				av.ApprovalPct = f.ApprovalPct
				av.ApprovalStr = fmt.Sprintf("%.1f%%", f.ApprovalPct*100)
				av.IsFriction = f.ApprovalPct < frictionThreshold
				latestAppr = f.ApprovalPct
			}
			if f.ForVotes > 0 {
				av.ForVotes = formatVotes(f.ForVotes)
				av.AgainstVotes = formatVotes(f.AgainstVotes)
				av.AbstainVotes = formatVotes(f.AbstainVotes)
				av.BrokerNonVotes = formatVotes(f.BrokerNonVotes)
			}
			apps = append(apps, av)
			allApprearances = append(allApprearances, av)
		}

		be := BoardEntryView{
			Ticker:      t,
			Appearances: apps,
			HasFriction: latestAppr >= 0 && latestAppr < frictionThreshold,
		}
		if latestAppr >= 0 {
			be.LatestApprStr = fmt.Sprintf("%.1f%%", latestAppr*100)
		}
		view.Boards = append(view.Boards, be)
	}

	// Sort allAppearances by date for sparkline.
	sort.Slice(allApprearances, func(i, j int) bool {
		return allApprearances[i].FilingDate < allApprearances[j].FilingDate
	})
	view.Sparkline = buildSparkline(allApprearances)

	// Signals about this person, rendered as article views.
	for _, sig := range personSigs {
		r := edition.Ranked{
			Signal:      sig,
			Headline:    edition.GenerateHeadline(sig),
			Kicker:      fmt.Sprintf("%s · %s", strings.ToUpper(string(sig.Type)), strings.ToUpper(string(sig.Severity))),
			KickerClass: severityKickerClass(sig.Severity),
			Dateline:    fmt.Sprintf("%s — %s.", normTicker(sig.Ticker), displayDateFull(DocEntry{FilingDate: sig.FilingDate, PersistedAt: time.Time{}})),
			Byline:      "By the Entity-Graph Desk",
			Deck:        sig.Interpretation,
		}
		view.Signals = append(view.Signals, signalToArticleView(r, 200))
	}

	// Board interlocks: find co-directors from edges.
	seen := map[string]bool{}
	for _, e := range edges {
		otherID := e.Target
		if otherID == node.CanonicalID {
			otherID = e.Source
		}
		if seen[otherID] {
			continue
		}
		seen[otherID] = true
		otherNode, ok := gs.Node(otherID)
		if !ok {
			continue
		}
		iv := InterlockView{
			Name:         otherNode.Name,
			CanonicalID:  otherID,
			SharedBoards: e.Companies,
		}
		view.Interlocks = append(view.Interlocks, iv)
	}
	sort.Slice(view.Interlocks, func(i, j int) bool {
		return view.Interlocks[i].Name < view.Interlocks[j].Name
	})

	return view
}

func severityKickerClass(s entitygraph.Severity) string {
	switch s {
	case entitygraph.SeverityCritical:
		return "kicker-critical"
	case entitygraph.SeverityHigh:
		return "kicker-high"
	case entitygraph.SeverityMedium:
		return "kicker-medium"
	default:
		return "kicker-low"
	}
}

// formatVotes formats a raw vote count as a human-readable string (e.g. "1.4B", "221M").
func formatVotes(n int64) string {
	if n == 0 {
		return ""
	}
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// ── JSON API ──────────────────────────────────────────────────────────────────

// TickerAPIResult is the JSON shape for /api/tickers.
type TickerAPIResult struct {
	Query   string           `json:"query"`
	Count   int              `json:"count"`
	Tickers []TickerAPIEntry `json:"tickers"`
}

// TickerAPIEntry is one item in the /api/tickers response.
type TickerAPIEntry struct {
	Symbol         string    `json:"symbol"`
	SignalCount     int       `json:"signal_count"`
	MaxSeverity     string    `json:"max_severity,omitempty"`
	LatestActivity  time.Time `json:"latest_activity,omitempty"`
}

// WriteTickerAPIJSON serialises search results as the /api/tickers JSON response.
func WriteTickerAPIJSON(w io.Writer, query string, results []catalog.SearchResult) error {
	limit := 8
	if len(results) > limit {
		results = results[:limit]
	}
	entries := make([]TickerAPIEntry, 0, len(results))
	for _, r := range results {
		entries = append(entries, TickerAPIEntry{
			Symbol:        r.Symbol,
			SignalCount:   r.SignalCount,
			MaxSeverity:   r.MaxSeverity,
			LatestActivity: r.LatestActivity,
		})
	}
	return json.NewEncoder(w).Encode(TickerAPIResult{
		Query:   strings.ToUpper(strings.TrimSpace(query)),
		Count:   len(entries),
		Tickers: entries,
	})
}
