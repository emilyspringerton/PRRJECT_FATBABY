package newssite

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/edition"
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

type FrontPageView struct {
	Base
	Date        string
	Count       int
	TickerItems []TickerTapeItem
	Lead        *ArticleView
	Secondary   []ArticleView
	MostActive  []ActiveTickerView
	WireItems   []WireItem
	Rest        []ArticleView
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
			DateStr:  formatDateShort(e.PersistedAt),
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

	return FrontPageView{
		Date:        time.Now().UTC().Format("Monday, 2 January 2006"),
		Count:       len(entries),
		TickerItems: tickerItems,
		Lead:        lead,
		Secondary:   secondary,
		MostActive:  mostActive,
		WireItems:   wireItems,
		Rest:        rest,
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
	dateline := fmt.Sprintf("%s — %s.", normTicker(entry.Ticker), formatDateFull(entry.PersistedAt))
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
	return ArticleView{
		Link:        "/ticker/" + normTicker(r.Signal.Ticker),
		Kicker:      r.Kicker,
		KickerClass: r.KickerClass,
		Headline:    r.Headline,
		Dateline:    r.Dateline,
		Byline:      r.Byline,
		Deck:        truncateRunes(r.Deck, deckMax),
	}
}

func docToArticleView(e DocEntry, deckMax int) ArticleView {
	kicker, kickerClass := docKicker(e)
	srcLabel := sourceTypeLabel(e.SourceType)
	formOrType := srcLabel
	if e.Form != "" {
		formOrType = e.Form
	}
	headline := fmt.Sprintf("%s — %s", normTicker(e.Ticker), formOrType)
	dateline := fmt.Sprintf("%s — %s.", normTicker(e.Ticker), formatDateFull(e.PersistedAt))
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
		Byline:      docByline(e),
		Deck:        deck,
	}
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
