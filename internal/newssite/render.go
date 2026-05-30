package newssite

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/internal/newssite/edition"
)

// ── View models ──────────────────────────────────────────────────────────────

// FrontPageView is the template data for the front page.
type FrontPageView struct {
	Date        string
	Count       int
	TickerItems []TickerTapeItem
	Lead        *ArticleView
	Secondary   []ArticleView
	MostActive  []ActiveTickerView
	WireItems   []WireItem
	Rest        []ArticleView
}

// ArticleView is the view model for one article card (signal or document).
// Link is the canonical href for the headline; Identity is kept for detail
// pages that need the raw /doc/ URL in the fact box.
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

// TickerTapeItem is one scrolling segment in the ticker tape.
type TickerTapeItem struct {
	Ticker string
	Label  string
}

// ActiveTickerView is one row in the "most active" sidebar.
type ActiveTickerView struct {
	Ticker   string
	DocCount int
}

// WireItem is one press release in the Wire sidebar.
type WireItem struct {
	Identity string
	Ticker   string
	Form     string
	DateStr  string
}

// DetailPageView is the template data for the filing detail page.
type DetailPageView struct {
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

// ── Render entry points ───────────────────────────────────────────────────────

// RenderListPage builds the front-page view and executes the template.
// ranked may be nil when the entity-graph store is not configured.
func RenderListPage(w io.Writer, entries []DocEntry, ranked []edition.Ranked) {
	view := buildFrontPage(entries, ranked)
	if err := frontTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// RenderDetailPage builds the detail view and executes the template.
func RenderDetailPage(w io.Writer, entry DocEntry) {
	view := buildDetailPage(entry)
	if err := detailTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// ── View builders ─────────────────────────────────────────────────────────────

func buildFrontPage(entries []DocEntry, ranked []edition.Ranked) FrontPageView {
	// Count documents per ticker; collect press releases for the sidebar.
	tickerCount := map[string]int{}
	var wireEntries []DocEntry
	for _, e := range entries {
		t := normTicker(e.Ticker)
		tickerCount[t]++
		if e.SourceType == "press_release" {
			wireEntries = append(wireEntries, e)
		}
	}

	// Most-active tickers (up to 6, by doc count).
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

	// Ticker tape: critical/high signals first, then unique tickers from docs.
	var tickerItems []TickerTapeItem
	tapeSeen := map[string]bool{}
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

	// Wire sidebar (top 3 press releases).
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

	// Build articles. Signals take priority for the lead + secondary slots.
	// After that, source documents fill the remaining slots.
	var lead *ArticleView
	var secondary, rest []ArticleView

	// Convert top 4 signals into article views.
	signalViews := make([]ArticleView, 0, 4)
	for i, r := range ranked {
		if i >= 4 {
			break
		}
		av := signalToArticleView(r, 400)
		if i == 0 {
			signalViews = append(signalViews, av)
		} else {
			av.Deck = truncateRunes(av.Deck, 160)
			signalViews = append(signalViews, av)
		}
	}

	// Assign lead + secondary.
	switch len(signalViews) {
	case 0:
		// No signals — fall back to documents.
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
	default:
		lead = &signalViews[0]
		secondary = signalViews[1:]
		// Fill remaining secondary slots from top docs (skip wire for secondary).
		docSecondary := 0
		for _, e := range entries {
			if len(secondary) >= 3 {
				break
			}
			if e.SourceType == "press_release" {
				continue
			}
			secondary = append(secondary, docToArticleView(e, 160))
			docSecondary++
		}
		// Rest: documents.
		docOffset := 0
		for _, e := range entries {
			if e.SourceType == "press_release" && docOffset < docSecondary {
				docOffset++
				continue
			}
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
		Link:        "/company/" + normTicker(r.Signal.Ticker),
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
	byline := docByline(e)
	srcLabel := sourceTypeLabel(e.SourceType)
	formOrType := srcLabel
	if e.Form != "" {
		formOrType = e.Form
	}
	headline := fmt.Sprintf("%s — %s", normTicker(e.Ticker), formOrType)
	dateline := fmt.Sprintf("%s — %s.", normTicker(e.Ticker), formatDateFull(e.PersistedAt))

	return ArticleView{
		Identity:    e.Identity,
		Link:        "/doc/" + e.Identity,
		Kicker:      kicker,
		KickerClass: kickerClass,
		Headline:    headline,
		Dateline:    dateline,
		Byline:      byline,
		Deck:        truncateRunes(e.BodyPreview, deckMax),
	}
}

// ── Supplemental page renderers ───────────────────────────────────────────────

// RenderBreakingPage renders /breaking: critical + high signals.
func RenderBreakingPage(w io.Writer, ranked []edition.Ranked) {
	items := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, signalToArticleView(r, 300))
	}
	if err := breakTmpl.Execute(w, BreakingView{Items: items}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// RenderSectionPage renders /section/{slug}.
func RenderSectionPage(w io.Writer, slug string, ranked []edition.Ranked) {
	items := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, signalToArticleView(r, 300))
	}
	view := SectionView{
		Slug:  slug,
		Title: sectionTitle(slug),
		Blurb: sectionBlurb(slug),
		Items: items,
	}
	if err := sectionTmpl.Execute(w, view); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// RenderCompanyPage renders /company/{ticker}.
func RenderCompanyPage(w io.Writer, ticker string, ranked []edition.Ranked, docs []DocEntry) {
	sigs := make([]ArticleView, 0, len(ranked))
	for _, r := range ranked {
		sigs = append(sigs, signalToArticleView(r, 300))
	}
	docViews := make([]ArticleView, 0, len(docs))
	for _, d := range docs {
		docViews = append(docViews, docToArticleView(d, 140))
	}
	if err := companyTmpl.Execute(w, CompanyView{Ticker: ticker, Signals: sigs, Docs: docViews}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// RenderArchivePage renders /archive.
func RenderArchivePage(w io.Writer, entries []DocEntry) {
	avs := make([]ArticleView, 0, len(entries))
	for _, e := range entries {
		avs = append(avs, docToArticleView(e, 0))
	}
	if err := archiveTmpl.Execute(w, ArchiveView{Entries: avs}); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
}

// RenderAboutPage renders /about.
func RenderAboutPage(w io.Writer) {
	if err := aboutTmpl.Execute(w, nil); err != nil {
		fmt.Fprintf(w, "render error: %v", err)
	}
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func docKicker(e DocEntry) (kicker, class string) {
	switch e.SourceType {
	case "press_release":
		return "The Wire", "kicker-wire"
	default:
		if e.Form != "" {
			return "SEC Filing · " + e.Form, "kicker-filing"
		}
		return "SEC Filing", "kicker-filing"
	}
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

func normTicker(t string) string {
	return strings.ToUpper(strings.TrimSpace(t))
}

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
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	cut := string(runes[:maxRunes])
	if i := strings.LastIndex(cut, " "); i > 0 {
		return strings.TrimSpace(cut[:i]) + "…"
	}
	return strings.TrimSpace(cut) + "…"
}
