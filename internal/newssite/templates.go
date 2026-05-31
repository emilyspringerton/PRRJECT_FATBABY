package newssite

import "html/template"

// tmpl is the single template set. All page templates are defined here so
// they share the "masthead" and "sectionsrail" named fragments.
var tmpl = template.Must(template.New("").Parse(sharedFragments + allPageTemplates))

// Convenience lookups — panic at init if a name is missing (programming error).
var (
	frontTmpl   = mustLookup("front")
	detailTmpl  = mustLookup("detail")
	breakTmpl   = mustLookup("breaking")
	sectionTmpl = mustLookup("section")
	tickerTmpl  = mustLookup("ticker")
	ticker404Tmpl = mustLookup("ticker404")
	tickersTmpl = mustLookup("tickers")
	searchTmpl  = mustLookup("search")
	archiveTmpl = mustLookup("archive")
	aboutTmpl   = mustLookup("about")
	personTmpl   = mustLookup("person")
	liveTmpl     = mustLookup("live")
	earningsTmpl = mustLookup("earnings")
)

func mustLookup(name string) *template.Template {
	t := tmpl.Lookup(name)
	if t == nil {
		panic("newssite template not found: " + name)
	}
	return t
}

// ── Shared CSS ────────────────────────────────────────────────────────────────

const siteCSS = `
*, *::before, *::after { box-sizing: border-box; }
html { font-size: 16px; }
body {
	font-family: Georgia, 'Times New Roman', serif;
	font-size: 1rem; line-height: 1.72;
	color: #111; background: #f9f5f0; margin: 0; padding: 0;
}
a { color: #1a1a8c; text-decoration: underline; }
a:visited { color: #551a8b; }
a:hover { color: #0d0d4a; }

/* Layout */
.wrap { max-width: 1120px; margin: 0 auto; padding: 0 1.25rem; }

/* Masthead */
.masthead {
	padding: 0.9rem 0 0;
	border-bottom: 4px double #111;
	text-align: center;
}
.masthead-name {
	font-family: 'Times New Roman', Times, serif;
	font-size: clamp(2.2rem, 6vw, 4rem);
	font-weight: 900; letter-spacing: -1.5px; line-height: 1; color: #111;
}
.masthead-tagline {
	font-family: system-ui, -apple-system, sans-serif;
	font-size: 0.68rem; letter-spacing: 3px; text-transform: uppercase; color: #666;
	margin-top: 0.25rem; border-top: 1px solid #ccc; border-bottom: 1px solid #ccc;
	padding: 0.2rem 2rem; display: inline-block;
}
/* Search form in masthead */
.search-row {
	display: flex; justify-content: center; padding: 0.45rem 0;
}
.search-form { display: flex; gap: 0; }
.search-input {
	font-family: system-ui, sans-serif; font-size: 0.82rem;
	border: 1px solid #999; border-right: none;
	padding: 0.28rem 0.65rem; background: #fff; outline: none; width: 220px;
}
.search-input:focus { border-color: #111; }
.search-btn {
	font-family: system-ui, sans-serif; font-size: 0.82rem;
	border: 1px solid #999; background: #111; color: #fff;
	padding: 0.28rem 0.75rem; cursor: pointer;
}
.search-btn:hover { background: #333; }

/* Edition bar */
.edition-bar {
	display: flex; justify-content: space-between; align-items: center;
	font-family: system-ui, sans-serif; font-size: 0.7rem; color: #555;
	border-bottom: 1px solid #111; padding: 0.3rem 0;
}

/* Ticker tape */
.ticker-tape {
	background: #1a1a2e; color: #e8e8ff;
	font-family: system-ui, sans-serif; font-size: 0.7rem;
	padding: 0.38rem 0; overflow: hidden; white-space: nowrap; user-select: none;
}
.ticker-inner {
	display: inline-block;
	animation: scrollticker 50s linear infinite;
	padding-left: 100%;
}
@keyframes scrollticker { 0%{transform:translateX(0)} 100%{transform:translateX(-100%)} }
.ticker-seg { margin: 0 2.5rem 0 0; }
.ticker-seg b { color: #fff; margin-right: 0.5rem; }
.ticker-seg span { opacity: 0.7; }

/* Sections rail */
.sections-rail {
	display: flex; flex-wrap: wrap;
	border-top: 2px solid #111; border-bottom: 1px solid #ccc; margin-bottom: 1.5rem;
}
.sections-rail a {
	font-family: system-ui, sans-serif; font-size: 0.68rem; font-weight: 700;
	letter-spacing: 1.5px; text-transform: uppercase; color: #111;
	text-decoration: none; padding: 0.45rem 0.9rem; border-right: 1px solid #ccc;
}
.sections-rail a:first-child { padding-left: 0; }
.sections-rail a:hover { background: #f0ebe3; }

/* Kickers */
.kicker {
	font-family: system-ui, sans-serif; font-size: 0.62rem; font-weight: 800;
	letter-spacing: 2.5px; text-transform: uppercase; display: block; margin-bottom: 0.2rem;
}
.kicker-filing   { color: #1d4ed8; }
.kicker-wire     { color: #065f46; }
.kicker-critical { color: #dc2626; }
.kicker-high     { color: #b45309; }
.kicker-medium   { color: #475569; }
.kicker-low      { color: #9ca3af; }
.kicker-archive   { color: #9ca3af; font-style: italic; }
.kicker-earnings  { color: #065f46; font-weight: 800; }
.sr-only { position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0; }

/* Headlines */
h1,h2,h3 { font-family: 'Times New Roman', Times, serif; margin: 0 0 0.35rem; line-height: 1.12; }
.hl-lead      { font-size: clamp(2rem, 3.5vw, 3rem); font-weight: 700; }
.hl-secondary { font-size: 1.5rem; font-weight: 700; }
.hl-item      { font-size: 1.1rem; font-weight: 700; }
h1 a,h2 a,h3 a { color: #111; text-decoration: none; }
h1 a:hover,h2 a:hover,h3 a:hover { text-decoration: underline; }

/* Dateline / byline */
.dateline {
	font-family: system-ui, sans-serif; font-size: 0.72rem; font-variant: small-caps;
	letter-spacing: 0.5px; color: #444; margin-bottom: 0.3rem;
}
.byline { font-family: system-ui, sans-serif; font-size: 0.78rem; font-style: italic; color: #666; margin-bottom: 0.65rem; }
.deck { font-size: 1rem; color: #222; line-height: 1.55; margin-bottom: 0.5rem; }

/* Front page two-column grid */
.front-body { display: grid; grid-template-columns: 1fr 280px; gap: 2rem; align-items: start; }
@media (max-width: 740px) { .front-body { grid-template-columns: 1fr; } }

/* Stories */
.lead-story   { padding-bottom: 1.2rem; border-bottom: 2px solid #111; margin-bottom: 1.2rem; }
article.story { padding: 1.1rem 0; border-bottom: 1px solid #ddd; }
article.story:last-child { border-bottom: none; }
hr.rule       { border: none; border-top: 1px solid #ddd; margin: 1.2rem 0; }
hr.rule-heavy { border: none; border-top: 2px solid #111; margin: 1.4rem 0; }

/* Sidebar */
.sidebar-box {
	border: 1px solid #ddd; background: #fff; padding: 0.9rem 1rem; margin-bottom: 1.25rem;
	font-family: system-ui, sans-serif;
}
.sidebar-box h4 {
	font-family: system-ui, sans-serif; font-size: 0.62rem; font-weight: 800;
	letter-spacing: 2.5px; text-transform: uppercase; color: #555;
	margin: 0 0 0.65rem; padding-bottom: 0.4rem; border-bottom: 1px solid #e5e5e5;
}
.sidebar-row {
	display: flex; justify-content: space-between; align-items: baseline;
	padding: 0.3rem 0; border-bottom: 1px dashed #eee; font-size: 0.82rem;
}
.sidebar-row:last-child { border-bottom: none; }
.sidebar-row .sym { font-weight: 700; color: #111; text-decoration: none; }
.sidebar-row .cnt { color: #888; font-size: 0.72rem; }
.wire-item { padding: 0.4rem 0; border-bottom: 1px dashed #eee; font-size: 0.85rem; }
.wire-item:last-child { border-bottom: none; }
.section-hdr {
	font-family: system-ui, sans-serif; font-size: 0.62rem; font-weight: 800;
	letter-spacing: 2.5px; text-transform: uppercase; color: #888;
	padding: 0.5rem 0 0.3rem; border-top: 1px solid #111; border-bottom: 1px solid #ddd;
	margin: 0 0 0;
}

/* Ticker page two-column grid */
.ticker-body { display: grid; grid-template-columns: 1fr 240px; gap: 2rem; align-items: start; }
@media (max-width: 700px) { .ticker-body { grid-template-columns: 1fr; } }

/* Severity dot */
.sev-dot {
	display: inline-block; width: 8px; height: 8px; border-radius: 50%;
	margin-right: 4px; vertical-align: middle;
}
.sev-dot-critical { background: #dc2626; }
.sev-dot-high     { background: #d97706; }
.sev-dot-medium   { background: #64748b; }
.sev-dot-low      { background: #d1d5db; }

/* Directory table */
.dir-table { width: 100%; border-collapse: collapse; font-family: system-ui, sans-serif; font-size: 0.88rem; }
.dir-table th {
	text-align: left; font-size: 0.62rem; font-weight: 800; letter-spacing: 2px;
	text-transform: uppercase; color: #888; border-bottom: 2px solid #111;
	padding: 0.4rem 0.5rem;
}
.dir-table td { padding: 0.45rem 0.5rem; border-bottom: 1px solid #eee; }
.dir-table tr:last-child td { border-bottom: none; }
.dir-table tr:hover td { background: #f5f0ea; }
.dir-table td:first-child { font-weight: 700; }

/* Search results */
.search-header { font-family: system-ui, sans-serif; font-size: 0.78rem; color: #555; margin-bottom: 1rem; }
.search-result { padding: 0.75rem 0; border-bottom: 1px solid #eee; font-family: system-ui, sans-serif; }
.search-result:last-child { border-bottom: none; }
.search-sym { font-size: 1.1rem; font-weight: 700; color: #111; text-decoration: none; }
.search-sym:hover { text-decoration: underline; }
.search-meta { font-size: 0.75rem; color: #888; margin-top: 0.15rem; }

/* Badges */
.badge {
	display: inline-block; font-family: system-ui, sans-serif; font-size: 0.6rem;
	font-weight: 800; letter-spacing: 1px; text-transform: uppercase;
	padding: 0.1rem 0.45rem; border-radius: 2px; vertical-align: middle; margin-right: 0.3rem;
}
.badge-wire { background: #065f46; color: #fff; }
.badge-sec  { background: #1d4ed8; color: #fff; }
.badge-sev-critical { background: #dc2626; color: #fff; }
.badge-sev-high     { background: #d97706; color: #fff; }

/* Detail page */
.reading-col { max-width: 700px; }
.fact-box {
	background: #f0ebe3; border-left: 3px solid #111;
	padding: 0.9rem 1.1rem; margin-bottom: 1.4rem; font-family: system-ui, sans-serif;
}
.fact-box h4 {
	font-size: 0.62rem; font-weight: 800; letter-spacing: 2.5px; text-transform: uppercase;
	color: #555; margin: 0 0 0.5rem;
}
.fact-box dl { display: grid; grid-template-columns: max-content 1fr; gap: 0.25rem 1rem; margin: 0; }
.fact-box dt { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: #666; white-space: nowrap; }
.fact-box dd { font-size: 0.85rem; margin: 0; color: #111; word-break: break-all; }
.doc-body pre { white-space: pre-wrap; word-break: break-word; font-family: Georgia, serif; font-size: 0.95rem; line-height: 1.75; margin: 0; }
nav.back-nav { font-family: system-ui, sans-serif; font-size: 0.85rem; margin-bottom: 1.5rem; padding-top: 0.75rem; }

/* Director table in ticker page */
.director-row { padding: 0.4rem 0; border-bottom: 1px dashed #eee; font-family: system-ui, sans-serif; font-size: 0.82rem; }
.director-row:last-child { border-bottom: none; }
.director-row .dr-name { font-weight: 600; }
.director-row .dr-pct  { font-size: 0.75rem; color: #555; }
.friction-flag { color: #dc2626; font-weight: 700; }

/* Footer */
.site-footer {
	margin-top: 3rem; border-top: 4px double #111; padding: 1rem 0 2rem;
	font-family: system-ui, sans-serif; font-size: 0.72rem; color: #888; text-align: center;
}
`

// ── Shared fragments (named templates used by all pages) ─────────────────────

const sharedFragments = `
{{define "masthead"}}
<header class="masthead" role="banner">
  <p class="masthead-name"><a href="/" style="color:#111;text-decoration:none;" aria-label="FATBABY home">FATBABY</a></p>
  <div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div>
  <div class="search-row">
    <form class="search-form" method="get" action="/search" role="search">
      <label for="q" class="sr-only">Search tickers</label>
      <input id="q" type="search" name="q" placeholder="Search tickers…" list="ticker-list" autocomplete="off" class="search-input" aria-label="Search tickers">
      <button type="submit" class="search-btn" aria-label="Submit search">→</button>
    </form>
    <datalist id="ticker-list">{{range .Symbols}}<option value="{{.}}">{{end}}</datalist>
  </div>
</header>
<script>(function(){
  var input = document.getElementById('q');
  if (!input) return;
  // When a datalist suggestion is selected, the browser fires 'change' with the
  // chosen value already in the input. Navigate immediately rather than making
  // the user press the Go button a second time.
  input.addEventListener('change', function() {
    var val = this.value.trim().toUpperCase();
    if (!val) return;
    var opts = document.querySelectorAll('#ticker-list option');
    for (var i = 0; i < opts.length; i++) {
      if (opts[i].value.toUpperCase() === val) {
        window.location.href = '/ticker/' + encodeURIComponent(val);
        return;
      }
    }
  });
}());</script>
{{end}}

{{define "sectionsrail"}}
<nav class="sections-rail" aria-label="Site sections">
  <a href="/">Front Page</a>
  <a href="/tickers">Tickers</a>
  <a href="/section/governance">Governance</a>
  <a href="/section/activism">Activism Watch</a>
  <a href="/section/boardroom">Boardroom</a>
  <a href="/section/auditor">Auditor Watch</a>
  <a href="/section/pay">Pay &amp; Proxy</a>
  <a href="/section/earnings">Earnings</a>
  <a href="/wire">The Wire</a>
  <a href="/breaking">Breaking</a>
  <a href="/live">Live</a>
  <a href="/archive">Archive</a>
  <a href="/about">About</a>
</nav>
{{end}}

{{define "footer"}}
<footer class="site-footer">
  FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.
  &nbsp;&middot;&nbsp; <a href="/about">About &amp; methodology</a>
  &nbsp;&middot;&nbsp; <a href="/tickers">Tickers</a>
  &nbsp;&middot;&nbsp; <a href="/archive">Archive</a>
</footer>
{{end}}

{{define "sevdot"}}{{if eq . "critical"}}<span class="sev-dot sev-dot-critical"></span>{{else if eq . "high"}}<span class="sev-dot sev-dot-high"></span>{{else if eq . "medium"}}<span class="sev-dot sev-dot-medium"></span>{{else if eq . "low"}}<span class="sev-dot sev-dot-low"></span>{{end}}{{end}}
`

// ── Page templates ────────────────────────────────────────────────────────────

const allPageTemplates = frontTemplate + detailTemplate + breakingTemplate +
	sectionTemplate + tickerTemplate + ticker404Template +
	tickersTemplate + searchTemplate + archiveTemplate + aboutTemplate +
	personTemplate + liveTemplate + earningsTemplate

const frontTemplate = `{{define "front"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FATBABY Financial Intelligence</title>
<link rel="alternate" type="application/rss+xml" title="FATBABY Financial Intelligence" href="/feed.xml">
<style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span>{{.Date}}</span><span>{{.Count}} documents indexed</span></div>
{{if .TickerItems}}
<div class="ticker-tape"><div class="ticker-inner">
{{range .TickerItems}}<span class="ticker-seg"><b>{{.Ticker}}</b><span>{{.Label}}</span></span>{{end}}
{{range .TickerItems}}<span class="ticker-seg"><b>{{.Ticker}}</b><span>{{.Label}}</span></span>{{end}}
</div></div>{{end}}
{{template "sectionsrail" .}}
{{if .Lead}}
<div class="front-body">
  <main>
    <article class="lead-story">
      <span class="kicker {{.Lead.KickerClass}}">{{.Lead.Kicker}}</span>
      <h1 class="hl-lead"><a href="{{.Lead.Link}}">{{.Lead.Headline}}</a></h1>
      <div class="dateline">{{.Lead.Dateline}}</div>
      <div class="byline">{{.Lead.Byline}}</div>
      <p class="deck">{{.Lead.Deck}}</p>
      <p style="font-family:system-ui;font-size:0.85rem;"><a href="{{.Lead.Link}}">Read full document →</a></p>
    </article>
    {{if .Secondary}}
    <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:1rem 1.5rem;margin-bottom:1.4rem;">
      {{range .Secondary}}<article>
        <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
        <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
        <div class="dateline">{{.Dateline}}</div>
        <div class="byline">{{.Byline}}</div>
        <p class="deck" style="font-size:0.9rem;">{{.Deck}}</p>
      </article>{{end}}
    </div>{{end}}
    {{if .Rest}}
    <p class="section-hdr">All Documents</p>
    {{range .Rest}}<article class="story">
      <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
      <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
      <div class="dateline">{{.Dateline}}</div>
      <div class="byline">{{.Byline}}</div>
      <p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>
    </article>{{end}}{{end}}
  </main>
  <aside>
    {{if .MostActive}}<div class="sidebar-box"><h4>Most Active Tickers</h4>
      {{range .MostActive}}<div class="sidebar-row">
        <a class="sym" href="/ticker/{{.Ticker}}">{{.Ticker}}</a>
        <span class="cnt">{{.DocCount}} docs</span>
      </div>{{end}}
    </div>{{end}}
    {{if .WireItems}}<div class="sidebar-box"><h4>The Wire</h4>
      {{range .WireItems}}<div class="wire-item">
        <span class="badge badge-wire">Wire</span>
        <a href="/doc/{{.Identity}}" style="font-family:system-ui;font-weight:600;font-size:0.82rem;color:#111;">{{.Ticker}}{{if .Form}} · {{.Form}}{{end}}</a>
        <div style="font-family:system-ui;font-size:0.7rem;color:#888;margin-top:0.1rem;">{{.DateStr}}</div>
      </div>{{end}}
    </div>{{end}}
    {{if .Earnings}}<div class="sidebar-box"><h4>Earnings</h4>
      {{range .Earnings}}<div style="padding:0.4rem 0;border-bottom:1px dashed #eee;font-family:system-ui;font-size:0.8rem;">
        <div><a href="{{.Link}}" style="font-weight:600;color:#111;">{{.Headline}}</a></div>
        <div style="color:#666;font-size:0.72rem;margin-top:0.1rem;">
          <span class="kicker-earnings" style="font-weight:800;font-size:0.65rem;letter-spacing:1px;text-transform:uppercase;margin-right:0.4rem;">{{.Ticker}}</span>
          {{if .EPSStr}}<strong>{{.EPSStr}}</strong>{{if .IsGAAP}} GAAP{{end}} ·{{end}} {{.PeriodStr}}
        </div>
      </div>{{end}}
      <div style="font-size:0.72rem;color:#888;margin-top:0.5rem;font-family:system-ui;"><a href="/section/earnings">All earnings →</a></div>
    </div>{{end}}
    {{if .SuccessionWatch}}<div class="sidebar-box"><h4>Succession Watch</h4>
      {{range .SuccessionWatch}}<div style="padding:0.4rem 0;border-bottom:1px dashed #eee;font-family:system-ui;font-size:0.8rem;">
        <div><a href="{{.Link}}" style="font-weight:600;color:#111;">{{if .Name}}{{.Name}}{{else}}Director{{end}}</a>
          {{if .Ticker}}<span style="color:#888;margin-left:0.35rem;font-size:0.72rem;">{{.Ticker}}</span>{{end}}
          {{if .ApprovalStr}}<span style="color:#dc2626;font-size:0.72rem;margin-left:0.35rem;">{{.ApprovalStr}}</span>{{end}}
        </div>
        {{if .Deck}}<div style="color:#666;font-size:0.72rem;line-height:1.4;margin-top:0.15rem;">{{.Deck}}</div>{{end}}
      </div>{{end}}
      <div style="font-size:0.72rem;color:#888;margin-top:0.5rem;font-family:system-ui;"><a href="/section/boardroom">More boardroom →</a></div>
    </div>{{end}}
    <div class="sidebar-box"><h4>Signals</h4>
      <p style="font-size:0.8rem;color:#888;margin:0;">Governance signals surface here once the processor has run. <a href="/breaking">Breaking →</a></p>
    </div>
  </aside>
</div>
{{else}}
<main style="max-width:700px;padding:2rem 0;">
  <p>No source documents have been persisted yet. The processor has not run or no filings have been discovered.</p>
</main>
{{end}}
</div>{{template "footer" .}}</body></html>{{end}}`

const detailTemplate = `{{define "detail"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span>{{.DateStr}}</span><span><a href="/">← Front page</a></span></div>
{{template "sectionsrail" .}}
<main class="reading-col" style="padding:1.5rem 0;">
  <nav class="back-nav" aria-label="Document navigation"><a href="/">← All documents</a>{{if .Ticker}} &nbsp;&middot;&nbsp; <a href="/ticker/{{.Ticker}}">{{.Ticker}} desk →</a>{{end}}</nav>
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h1 class="hl-lead" style="margin-bottom:0.5rem;">{{.Headline}}</h1>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
  <div class="fact-box"><h4>Filing Details</h4><dl>
    <dt>Source</dt><dd>{{.SourceLabel}}</dd>
    {{if .Form}}<dt>Form</dt><dd>{{.Form}}</dd>{{end}}
    <dt>Persisted</dt><dd>{{.DateStr}}</dd>
    {{if .CharCount}}<dt>Length</dt><dd>{{.CharCount}} characters</dd>{{end}}
    {{if .DocumentURL}}<dt>Original</dt><dd>{{if .IsExternalLink}}<a href="{{.DocumentURL}}" rel="noopener">{{.DocumentURL}}</a>{{else}}{{.DocumentURL}}{{end}}</dd>{{end}}
  </dl></div>
  <hr class="rule">
  <div class="doc-body"><pre>{{.FullText}}</pre></div>
</main>
</div>{{template "footer" .}}</body></html>{{end}}`

const breakingTemplate = `{{define "breaking"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Breaking — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/">← Front page</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:760px;padding:1rem 0;">
<h1 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#dc2626;margin:0 0 1.2rem;">Breaking &amp; High-Priority Signals</h1>
{{if .Items}}{{range .Items}}<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
  <p class="deck">{{.Deck}}</p>
</article>{{end}}
{{else}}<p>No critical or high-priority signals right now.</p>{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const sectionTemplate = `{{define "section"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/">← Front page</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:760px;padding:1rem 0;">
<h1 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#888;margin:0 0 0.3rem;">{{.Title}}</h1>
{{if .Blurb}}<p style="font-family:system-ui;font-size:0.88rem;color:#555;margin:0 0 1.2rem;border-bottom:1px solid #ddd;padding-bottom:0.75rem;">{{.Blurb}}</p>{{end}}
{{if .Items}}{{range .Items}}<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
  <p class="deck">{{.Deck}}</p>
</article>{{end}}
{{else}}<p>No signals in this section yet.</p>{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const tickerTemplate = `{{define "ticker"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Symbol}} — FATBABY</title>
<link rel="alternate" type="application/rss+xml" title="{{.Symbol}} signals — FATBABY" href="/ticker/{{.Symbol}}/feed.xml">
<style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar">
  <span style="font-family:system-ui;font-weight:700;font-size:0.88rem;">
    {{.Symbol}}
    {{if .Auditor}} &middot; {{.Auditor}}{{end}}
    {{if .LastActivity}} &middot; {{.LastActivity}}{{end}}
    {{if .Forms}} &middot; {{.Forms}}{{end}}
  </span>
  <span><a href="/tickers">← All tickers</a></span>
</div>
{{template "sectionsrail" .}}
<div class="ticker-body">
  <main>
    {{if .Lead}}
    <article class="lead-story">
      <span class="kicker {{.Lead.KickerClass}}">{{.Lead.Kicker}}</span>
      <h1 class="hl-lead"><a href="{{.Lead.Link}}">{{.Lead.Headline}}</a></h1>
      <div class="dateline">{{.Lead.Dateline}}</div>
      <div class="byline">{{.Lead.Byline}}</div>
      <p class="deck">{{.Lead.Deck}}</p>
    </article>
    {{end}}
    {{if .Signals}}
    <p class="section-hdr">Governance Signals</p>
    {{range .Signals}}<article class="story">
      <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
      <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
      <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
      <p class="deck" style="font-size:0.9rem;">{{.Deck}}</p>
    </article>{{end}}
    {{end}}
    {{if .Docs}}
    <p class="section-hdr" style="margin-top:1.5rem;">SEC Filings</p>
    {{range .Docs}}<article class="story">
      <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
      <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
      <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
      <p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>
    </article>{{end}}
    {{end}}
    {{if .Wire}}
    <p class="section-hdr" style="margin-top:1.5rem;">Press Releases</p>
    {{range .Wire}}<article class="story">
      <span class="badge badge-wire">Wire</span>
      <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
      <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
    </article>{{end}}
    {{end}}
    {{if .Earnings}}
    <p class="section-hdr" style="margin-top:1.5rem;">Earnings</p>
    {{range .Earnings}}<article class="story">
      <span class="kicker kicker-earnings">EARNINGS{{if .PeriodStr}} · {{.PeriodStr}}{{end}}</span>
      <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
      <div class="dateline">{{.Ticker}}{{if .DateStr}} — {{.DateStr}}.{{end}}</div>
      <div class="byline">By the EPS Desk{{if .EPSStr}} &nbsp;·&nbsp; <strong>{{.EPSStr}}</strong>{{if .IsGAAP}} GAAP{{end}}{{end}}</div>
    </article>{{end}}
    {{end}}
    {{if and (not .Lead) (not .Signals) (not .Docs) (not .Wire) (not .Earnings)}}
    <p>No signals or documents for {{.Symbol}} yet.</p>
    {{end}}
  </main>
  <aside>
    <div class="fact-box" style="margin-bottom:1.25rem;">
      <h4>Fact Box</h4><dl>
        <dt>Total signals</dt><dd>{{.Facts.TotalSignals}}</dd>
        {{if .Facts.CritHighCount}}<dt>Critical / high</dt><dd>{{.Facts.CritHighCount}}</dd>{{end}}
        <dt>Directors tracked</dt><dd>{{.Facts.DirectorCount}}</dd>
        <dt>Documents</dt><dd>{{.Facts.DocCount}}</dd>
      </dl>
    </div>
    {{if .Directors}}
    <div class="sidebar-box"><h4>The Board</h4>
      {{range .Directors}}<div class="director-row">
        <span class="dr-name">{{if .HasFriction}}<span class="friction-flag">⚑ </span>{{end}}<a href="/person/{{.CanonicalID}}" style="color:#111;">{{.Name}}</a></span>
        {{if .ApprovalStr}}<span class="dr-pct"> — {{.ApprovalStr}}</span>{{end}}
      </div>{{end}}
    </div>
    {{end}}
    {{if .Auditor}}
    <div class="sidebar-box"><h4>Auditor</h4>
      <p style="font-size:0.85rem;margin:0;">{{.Auditor}}</p>
    </div>
    {{end}}
  </aside>
</div>
</div>{{template "footer" .}}</body></html>{{end}}`

const ticker404Template = `{{define "ticker404"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Symbol}} not found — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/tickers">← All tickers</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:700px;padding:2rem 0;">
  <h1 class="hl-lead">We don't cover <strong>{{.Symbol}}</strong></h1>
  <p style="font-family:system-ui;color:#555;">That ticker isn't in our data yet. Try searching:</p>
  {{if .Nearest}}
  <p style="font-family:system-ui;font-size:0.88rem;color:#555;">Nearest matches:</p>
  <ul style="font-family:system-ui;">
    {{range .Nearest}}<li><a href="/ticker/{{.}}">{{.}}</a></li>{{end}}
  </ul>
  {{end}}
  <p style="font-family:system-ui;margin-top:1.5rem;"><a href="/tickers">Browse all tickers →</a></p>
</main></div>{{template "footer" .}}</body></html>{{end}}`

const tickersTemplate = `{{define "tickers"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Tickers — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar">
  <span style="font-family:system-ui;font-size:0.7rem;color:#555;">{{.Total}} tickers covered</span>
  <span style="font-family:system-ui;font-size:0.7rem;">
    {{if .ByAlpha}}<a href="/tickers">By activity</a> &middot; <strong>A–Z</strong>{{else}}<strong>By activity</strong> &middot; <a href="/tickers?sort=alpha">A–Z</a>{{end}}
  </span>
</div>
{{template "sectionsrail" .}}
<main>
<table class="dir-table">
  <thead><tr>
    <th>Ticker</th><th>Severity</th><th>Signals</th><th>Docs</th><th>Directors</th><th>Last Activity</th>
  </tr></thead>
  <tbody>
  {{range .Rows}}<tr>
    <td><a href="/ticker/{{.Symbol}}" style="color:#111;text-decoration:none;font-weight:700;">{{.Symbol}}</a></td>
    <td>{{if .MaxSeverity}}{{template "sevdot" .MaxSeverity}}<span style="font-family:system-ui;font-size:0.75rem;">{{.MaxSeverity}}</span>{{else}}<span style="color:#ccc;font-family:system-ui;font-size:0.75rem;">—</span>{{end}}</td>
    <td style="font-family:system-ui;">{{.TotalSignals}}</td>
    <td style="font-family:system-ui;">{{.DocCount}}</td>
    <td style="font-family:system-ui;">{{.DirectorCount}}</td>
    <td style="font-family:system-ui;font-size:0.75rem;color:#666;">{{.LatestStr}}</td>
  </tr>{{end}}
  </tbody>
</table>
</main></div>{{template "footer" .}}</body></html>{{end}}`

const searchTemplate = `{{define "search"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{if .Query}}{{.Query}} — {{end}}Search — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/">← Front page</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:700px;padding:1rem 0;">
{{if .Query}}
  <p class="search-header">Search results for <strong>{{.Query}}</strong>{{if .Matches}} — {{len .Matches}} ticker{{if gt (len .Matches) 1}}s{{end}}{{end}}</p>
  {{if .Matches}}
  {{range .Matches}}<div class="search-result">
    <a class="search-sym" href="/ticker/{{.Symbol}}">{{template "sevdot" .MaxSeverity}}{{.Symbol}}</a>
    <div class="search-meta">{{if .SignalCount}}{{.SignalCount}} signal{{if gt .SignalCount 1}}s{{end}}{{if .MaxSeverity}} · {{.MaxSeverity}}{{end}}{{else}}no signals yet{{end}}{{if .LatestStr}} · {{.LatestStr}}{{end}}</div>
  </div>{{end}}
  {{else}}<p style="font-family:system-ui;color:#555;">No tickers match <strong>{{.Query}}</strong>. <a href="/tickers">Browse all →</a></p>{{end}}
{{else}}
  <p style="font-family:system-ui;color:#555;margin-bottom:1rem;">Enter a ticker symbol or name to search.</p>
  <p style="font-family:system-ui;"><a href="/tickers">Browse all tickers →</a></p>
{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const archiveTemplate = `{{define "archive"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Archive — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/">← Front page</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:760px;padding:1rem 0;">
<h2 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#888;margin:0 0 1rem;">The Morgue — Full Archive</h2>
{{if .Entries}}{{range .Entries}}<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
  <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
</article>{{end}}
{{else}}<p>No documents in the archive yet.</p>{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const aboutTemplate = `{{define "about"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>About — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
<div class="edition-bar"><span><a href="/">← Front page</a></span><span></span></div>
{{template "sectionsrail" .}}
<main style="max-width:700px;padding:1.5rem 0;" class="reading-col">
<h1 class="hl-lead">Masthead &amp; Methodology</h1>
<hr class="rule-heavy">
<h2 style="font-family:system-ui;font-size:0.85rem;font-weight:700;margin:1.5rem 0 0.5rem;">What is FATBABY?</h2>
<p>FATBABY Financial Intelligence is a Go-based pipeline that watches SEC EDGAR filings and press releases, extracts governance signals, and presents them as a structured publication. It is not a news organisation, not a registered investment adviser, and not affiliated with any company it covers.</p>
<h2 style="font-family:system-ui;font-size:0.85rem;font-weight:700;margin:1.5rem 0 0.5rem;">Signal types</h2>
<dl style="font-family:system-ui;font-size:0.88rem;">
<dt style="font-weight:700;margin-top:0.75rem;">director_friction</dt><dd>Director approval fell below the friction threshold (~90%). May indicate activist targeting.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">nomination_rejection</dt><dd>Director received sub-50% approval — critical under majority voting standards.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">activist_risk</dt><dd>Co-occurrence of entrenchment and friction. Historical base rate: activist 13D within 6 months in ~60% of similar cases.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">governance_entrenchment</dt><dd>Shareholder proposal cleared majority support but failed due to supermajority threshold.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">director_decay</dt><dd>Director approval trending downward over multiple years.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">auditor_change</dt><dd>Public accounting firm changed between filings.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">compensation_concern</dt><dd>Advisory say-on-pay vote received elevated opposition.</dd>
<dt style="font-weight:700;margin-top:0.75rem;">governance_health_index</dt><dd>Composite score [0.0–1.0] based on adverse and positive signals within the trailing window.</dd>
</dl>
<h2 style="font-family:system-ui;font-size:0.85rem;font-weight:700;margin:1.5rem 0 0.5rem;">Not investment advice</h2>
<p>All signals are model-derived from public filings. They are not investment advice and should not be relied upon for trading decisions.</p>
{{if .RulesUpdatedAt}}
<h2 style="font-family:system-ui;font-size:0.85rem;font-weight:700;margin:1.5rem 0 0.5rem;">
  {{if .RulesRecent}}<span style="color:#b45309;">⟳ Methodology recently updated</span>{{else}}Methodology{{end}}
</h2>
<p style="font-size:0.9rem;">Signal scoring rules (<code>config/entity-graph-rules.json</code>) were last updated on <strong>{{.RulesUpdatedAt}}</strong>.
When rules change, previously scored signals may be re-evaluated on the next entity-graph run.
The accuracy of prior signals published before the update date should be interpreted in light of the rule version in effect at that time.</p>
{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const liveTemplate = `{{define "live"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Live Desk — FATBABY</title>
<style>` + siteCSS + `
#live-status { font-family:system-ui;font-size:0.72rem;color:#888;margin-bottom:0.75rem; }
#live-status.connected { color:#065f46; }
.live-new { animation: fadeIn 0.4s ease; }
@keyframes fadeIn { from { opacity:0; transform:translateY(-6px); } to { opacity:1; transform:none; } }
</style></head>
<body><div class="wrap">
{{template "masthead" .}}
{{template "sectionsrail" .}}
<main style="max-width:700px;">
<h1 style="font-family:system-ui;font-size:1.1rem;font-weight:800;letter-spacing:1.5px;text-transform:uppercase;margin:1.2rem 0 0.3rem;">Live Desk</h1>
<p id="live-status">Static snapshot — connect with JavaScript for live updates.</p>
<div id="live-feed">
{{if .Items}}
  {{range .Items}}<article class="story">
    <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
    <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
    <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
    {{if .Deck}}<p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>{{end}}
  </article>{{end}}
{{else}}
  <p style="color:#888;font-family:system-ui;">No critical or high signals at the moment. Check back soon.</p>
{{end}}
</div>
</main>
<script>
(function() {
  var status = document.getElementById('live-status');
  var feed   = document.getElementById('live-feed');
  if (!window.EventSource) return;

  var es = new EventSource('/live/events');

  es.addEventListener('connected', function() {
    status.textContent = 'Connected — updates appear automatically.';
    status.className = 'connected';
  });

  es.addEventListener('refresh', function() {
    fetch('/breaking')
      .then(function(r) { return r.text(); })
      .then(function(html) {
        var parser = new DOMParser();
        var doc = parser.parseFromString(html, 'text/html');
        var articles = doc.querySelectorAll('article.story');
        if (articles.length === 0) return;
        var existing = feed.querySelectorAll('article.story');
        var existingLinks = new Set(Array.from(existing).map(function(a) {
          var l = a.querySelector('a'); return l ? l.href : '';
        }));
        var added = 0;
        articles.forEach(function(art) {
          var link = art.querySelector('a');
          if (!link || existingLinks.has(link.href)) return;
          art.classList.add('live-new');
          feed.insertBefore(art, feed.firstChild);
          added++;
        });
        if (added > 0) {
          status.textContent = 'Updated — ' + added + ' new item' + (added > 1 ? 's' : '') + ' just arrived.';
        }
      });
  });

  es.onerror = function() {
    status.textContent = 'Connection lost — reconnecting…';
    status.className = '';
  };
})();
</script>
</div>{{template "footer" .}}</body></html>{{end}}`

const earningsTemplate = `{{define "earnings"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Earnings — FATBABY</title>
<link rel="alternate" type="application/rss+xml" title="Earnings — FATBABY" href="/section/earnings/feed.xml">
<style>` + siteCSS + `</style></head>
<body><div class="wrap">
{{template "masthead" .}}
{{template "sectionsrail" .}}
<main style="max-width:760px;padding:1rem 0;">
<h1 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#065f46;margin:0 0 1.2rem;">Earnings Desk</h1>
{{if .Items}}
{{range .Items}}<article class="story">
  <span class="kicker kicker-earnings">EARNINGS{{if .PeriodStr}} · {{.PeriodStr}}{{end}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Ticker}}{{if .DateStr}} — {{.DateStr}}.{{end}}</div>
  <div class="byline">By the EPS Desk{{if .EPSStr}} &nbsp;·&nbsp; <strong>{{.EPSStr}}</strong>{{if .IsGAAP}} GAAP{{end}}{{end}}</div>
  {{if .Dek}}<p class="deck" style="font-size:0.88rem;">{{.Dek}}</p>{{end}}
</article>{{end}}
{{else}}
<p style="color:#888;font-family:system-ui;">No earnings articles yet. Run eps-processor against a press release feed to generate them.</p>
{{end}}
</main></div>{{template "footer" .}}</body></html>{{end}}`

const personTemplate = `{{define "person"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Name}} — FATBABY</title>
<style>` + siteCSS + `
.person-header { border-bottom: 3px solid #111; padding-bottom: 0.75rem; margin-bottom: 1.4rem; }
.person-name { font-family: 'Times New Roman', Times, serif; font-size: clamp(1.6rem,4vw,2.4rem); font-weight: 900; margin: 0 0 0.2rem; }
.person-meta { font-family: system-ui, sans-serif; font-size: 0.75rem; color: #666; }
.person-meta span + span::before { content: " · "; }
.board-section { margin-bottom: 2rem; }
.board-ticker-hdr {
  font-family: system-ui, sans-serif; font-size: 0.68rem; font-weight: 800;
  letter-spacing: 2.5px; text-transform: uppercase; color: #555;
  border-bottom: 2px solid #111; padding-bottom: 0.3rem; margin-bottom: 0.7rem;
}
.sparkline-wrap { margin-bottom: 1.2rem; }
.sparkline-wrap svg { display: block; }
.sparkline-label { font-family: system-ui, sans-serif; font-size: 0.65rem; color: #888; margin-top: 0.2rem; }
.interlock-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px,1fr)); gap: 0.5rem 1rem; }
.interlock-card { font-family: system-ui, sans-serif; font-size: 0.82rem; padding: 0.4rem 0; border-bottom: 1px dashed #eee; }
.interlock-name { font-weight: 600; }
.interlock-boards { font-size: 0.72rem; color: #666; }
</style></head>
<body><div class="wrap">
{{template "masthead" .}}
{{template "sectionsrail" .}}
<main class="reading-col" style="max-width:860px;">
<nav class="back-nav"><a href="/tickers">← Tickers</a></nav>

<div class="person-header">
  <h1 class="person-name">{{.Name}}</h1>
  <div class="person-meta">
    <span style="text-transform:capitalize;">{{.Role}}</span>
    {{if gt .Centrality 1}}<span>Sits on {{.Centrality}} boards</span>{{end}}
    {{if .FilingCount}}<span>{{.FilingCount}} filing appearance{{if gt .FilingCount 1}}s{{end}}</span>{{end}}
    {{if .FirstSeen}}<span>First seen {{.FirstSeen}}</span>{{end}}
    {{if .LastSeen}}<span>Last seen {{.LastSeen}}</span>{{end}}
  </div>
</div>

{{if .Sparkline.HasData}}
<div class="sparkline-wrap">
  <svg width="{{.Sparkline.Width}}" height="{{.Sparkline.Height}}" viewBox="0 0 {{.Sparkline.Width}} {{.Sparkline.Height}}" aria-hidden="true">
    <line x1="0" y1="{{.Sparkline.ThresholdY}}" x2="{{.Sparkline.Width}}" y2="{{.Sparkline.ThresholdY}}"
          stroke="#dc2626" stroke-width="1" stroke-dasharray="4,3" opacity="0.5"/>
    <polyline points="{{.Sparkline.Points}}" fill="none" stroke="#1a1a8c" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>
    {{range $i, $pt := .Sparkline.Points}}{{end}}
  </svg>
  <div class="sparkline-label">Approval % over time &nbsp;— &nbsp;<span style="color:#dc2626;">— — —</span>&nbsp; 90% friction threshold</div>
</div>
{{end}}

{{if .Boards}}
<h2 style="font-family:system-ui;font-size:0.78rem;font-weight:800;letter-spacing:2px;text-transform:uppercase;color:#555;margin:0 0 1rem;">Board appearances</h2>
{{range .Boards}}
<div class="board-section">
  <div class="board-ticker-hdr"><a href="/ticker/{{.Ticker}}">{{.Ticker}}</a>
    {{if .HasFriction}}&nbsp;<span style="color:#dc2626;font-size:0.75rem;">▼ friction</span>{{end}}
    {{if .LatestApprStr}}&nbsp;<span style="color:#555;font-weight:400;font-size:0.75rem;">{{.LatestApprStr}} latest</span>{{end}}
  </div>
  <table class="dir-table" style="width:100%;">
    <thead><tr>
      <th>Date</th><th>Form</th><th>Approval</th>
      <th>For</th><th>Against</th><th>Abstain</th><th>Broker NV</th>
    </tr></thead>
    <tbody>
    {{range .Appearances}}<tr{{if .IsFriction}} style="color:#dc2626;"{{end}}>
      <td>{{.DateStr}}</td>
      <td>{{.Form}}</td>
      <td><strong>{{.ApprovalStr}}</strong></td>
      <td style="font-size:0.8rem;color:#555;">{{.ForVotes}}</td>
      <td style="font-size:0.8rem;color:#555;">{{.AgainstVotes}}</td>
      <td style="font-size:0.8rem;color:#555;">{{.AbstainVotes}}</td>
      <td style="font-size:0.8rem;color:#555;">{{.BrokerNonVotes}}</td>
    </tr>{{end}}
    </tbody>
  </table>
</div>
{{end}}
{{end}}

{{if .Signals}}
<h2 style="font-family:system-ui;font-size:0.78rem;font-weight:800;letter-spacing:2px;text-transform:uppercase;color:#555;margin:1.5rem 0 0.8rem;">Signals</h2>
{{range .Signals}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
  <div class="dateline">{{.Dateline}}</div><div class="byline">{{.Byline}}</div>
  {{if .Deck}}<p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>{{end}}
</article>
{{end}}
{{end}}

{{if .Interlocks}}
<h2 style="font-family:system-ui;font-size:0.78rem;font-weight:800;letter-spacing:2px;text-transform:uppercase;color:#555;margin:1.5rem 0 0.8rem;">Board co-members</h2>
<div class="interlock-grid">
{{range .Interlocks}}
<div class="interlock-card">
  <div class="interlock-name"><a href="/person/{{.CanonicalID}}">{{.Name}}</a></div>
  <div class="interlock-boards">{{range $i,$t := .SharedBoards}}{{if $i}}, {{end}}<a href="/ticker/{{$t}}">{{$t}}</a>{{end}}</div>
</div>
{{end}}
</div>
{{end}}

</main></div>{{template "footer" .}}</body></html>{{end}}`
