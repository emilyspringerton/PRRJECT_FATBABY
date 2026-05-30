package newssite

import "html/template"

var (
	frontTmpl   = template.Must(template.New("front").Parse(frontTemplate))
	detailTmpl  = template.Must(template.New("detail").Parse(detailTemplate))
	breakTmpl   = template.Must(template.New("break").Parse(breakingTemplate))
	sectionTmpl = template.Must(template.New("section").Parse(sectionTemplate))
	companyTmpl = template.Must(template.New("company").Parse(companyTemplate))
	archiveTmpl = template.Must(template.New("archive").Parse(archiveTemplate))
	aboutTmpl   = template.Must(template.New("about").Parse(aboutTemplate))
)

const siteCSS = `
*, *::before, *::after { box-sizing: border-box; }
html { font-size: 16px; }
body {
	font-family: Georgia, 'Times New Roman', serif;
	font-size: 1rem;
	line-height: 1.72;
	color: #111;
	background: #f9f5f0;
	margin: 0; padding: 0;
}
a { color: #1a1a8c; text-decoration: underline; }
a:visited { color: #551a8b; }
a:hover { color: #0d0d4a; }

/* Layout */
.wrap { max-width: 1120px; margin: 0 auto; padding: 0 1.25rem; }

/* Masthead */
.masthead {
	padding: 1.1rem 0 0.6rem;
	border-bottom: 4px double #111;
	text-align: center;
}
.masthead-name {
	font-family: 'Times New Roman', Times, serif;
	font-size: clamp(2.2rem, 6vw, 4rem);
	font-weight: 900;
	letter-spacing: -1.5px;
	line-height: 1;
	color: #111;
	text-decoration: none;
}
.masthead-tagline {
	font-family: system-ui, -apple-system, sans-serif;
	font-size: 0.68rem;
	letter-spacing: 3px;
	text-transform: uppercase;
	color: #666;
	margin-top: 0.3rem;
	border-top: 1px solid #ccc;
	border-bottom: 1px solid #ccc;
	padding: 0.2rem 2rem;
	display: inline-block;
}
.edition-bar {
	display: flex;
	justify-content: space-between;
	align-items: center;
	font-family: system-ui, sans-serif;
	font-size: 0.7rem;
	color: #555;
	border-bottom: 1px solid #111;
	padding: 0.35rem 0;
}

/* Ticker tape */
.ticker-tape {
	background: #1a1a2e;
	color: #e8e8ff;
	font-family: system-ui, sans-serif;
	font-size: 0.7rem;
	letter-spacing: 0.4px;
	padding: 0.38rem 0;
	overflow: hidden;
	white-space: nowrap;
	user-select: none;
}
.ticker-inner {
	display: inline-block;
	animation: scrollticker 50s linear infinite;
	padding-left: 100%;
}
@keyframes scrollticker {
	0%   { transform: translateX(0); }
	100% { transform: translateX(-100%); }
}
.ticker-seg { margin: 0 2.5rem 0 0; }
.ticker-seg b { color: #fff; margin-right: 0.5rem; }
.ticker-seg span { opacity: 0.7; }

/* Sections rail */
.sections-rail {
	display: flex;
	flex-wrap: wrap;
	border-top: 2px solid #111;
	border-bottom: 1px solid #ccc;
	margin-bottom: 1.5rem;
}
.sections-rail a {
	font-family: system-ui, sans-serif;
	font-size: 0.68rem;
	font-weight: 700;
	letter-spacing: 1.5px;
	text-transform: uppercase;
	color: #111;
	text-decoration: none;
	padding: 0.45rem 0.9rem;
	border-right: 1px solid #ccc;
}
.sections-rail a:first-child { padding-left: 0; }
.sections-rail a:hover { background: #f0ebe3; }

/* Kickers */
.kicker {
	font-family: system-ui, sans-serif;
	font-size: 0.62rem;
	font-weight: 800;
	letter-spacing: 2.5px;
	text-transform: uppercase;
	display: block;
	margin-bottom: 0.2rem;
}
.kicker-filing   { color: #1d4ed8; }
.kicker-wire     { color: #065f46; }
.kicker-critical { color: #dc2626; }
.kicker-high     { color: #b45309; }
.kicker-medium   { color: #475569; }
.kicker-low      { color: #9ca3af; }

/* Headlines */
h1, h2, h3 { font-family: 'Times New Roman', Times, serif; margin: 0 0 0.35rem; line-height: 1.12; }
.hl-lead      { font-size: clamp(2rem, 3.5vw, 3rem); font-weight: 700; }
.hl-secondary { font-size: 1.5rem; font-weight: 700; }
.hl-item      { font-size: 1.1rem; font-weight: 700; }
h1 a, h2 a, h3 a { color: #111; text-decoration: none; }
h1 a:hover, h2 a:hover, h3 a:hover { text-decoration: underline; }

/* Dateline / byline */
.dateline {
	font-family: system-ui, sans-serif;
	font-size: 0.72rem;
	font-variant: small-caps;
	letter-spacing: 0.5px;
	color: #444;
	margin-bottom: 0.3rem;
}
.byline {
	font-family: system-ui, sans-serif;
	font-size: 0.78rem;
	font-style: italic;
	color: #666;
	margin-bottom: 0.65rem;
}
.deck { font-size: 1rem; color: #222; line-height: 1.55; margin-bottom: 0.5rem; }

/* Front page two-column grid */
.front-body {
	display: grid;
	grid-template-columns: 1fr 280px;
	gap: 2rem;
	align-items: start;
}
@media (max-width: 740px) {
	.front-body { grid-template-columns: 1fr; }
}

/* Stories */
.lead-story    { padding-bottom: 1.2rem; border-bottom: 2px solid #111; margin-bottom: 1.2rem; }
article.story  { padding: 1.1rem 0; border-bottom: 1px solid #ddd; }
article.story:last-child { border-bottom: none; }
hr.rule        { border: none; border-top: 1px solid #ddd; margin: 1.2rem 0; }
hr.rule-heavy  { border: none; border-top: 2px solid #111; margin: 1.4rem 0; }

/* Sidebar */
.sidebar-box {
	border: 1px solid #ddd;
	background: #fff;
	padding: 0.9rem 1rem;
	margin-bottom: 1.25rem;
	font-family: system-ui, sans-serif;
}
.sidebar-box h4 {
	font-family: system-ui, sans-serif;
	font-size: 0.62rem;
	font-weight: 800;
	letter-spacing: 2.5px;
	text-transform: uppercase;
	color: #555;
	margin: 0 0 0.65rem;
	padding-bottom: 0.4rem;
	border-bottom: 1px solid #e5e5e5;
}
.sidebar-row {
	display: flex;
	justify-content: space-between;
	align-items: baseline;
	padding: 0.3rem 0;
	border-bottom: 1px dashed #eee;
	font-size: 0.82rem;
}
.sidebar-row:last-child { border-bottom: none; }
.sidebar-row .sym { font-weight: 700; color: #111; text-decoration: none; }
.sidebar-row .cnt { color: #888; font-size: 0.72rem; }
.wire-item { padding: 0.4rem 0; border-bottom: 1px dashed #eee; font-size: 0.85rem; }
.wire-item:last-child { border-bottom: none; }

/* Section header (above full list) */
.section-hdr {
	font-family: system-ui, sans-serif;
	font-size: 0.62rem;
	font-weight: 800;
	letter-spacing: 2.5px;
	text-transform: uppercase;
	color: #888;
	padding: 0.5rem 0 0.3rem;
	border-top: 1px solid #111;
	border-bottom: 1px solid #ddd;
	margin: 0 0 0;
}

/* Badges */
.badge {
	display: inline-block;
	font-family: system-ui, sans-serif;
	font-size: 0.6rem;
	font-weight: 800;
	letter-spacing: 1px;
	text-transform: uppercase;
	padding: 0.1rem 0.45rem;
	border-radius: 2px;
	vertical-align: middle;
	margin-right: 0.3rem;
}
.badge-wire { background: #065f46; color: #fff; }
.badge-sec  { background: #1d4ed8; color: #fff; }

/* Detail page */
.reading-col { max-width: 700px; }
.fact-box {
	background: #f0ebe3;
	border-left: 3px solid #111;
	padding: 0.9rem 1.1rem;
	margin-bottom: 1.4rem;
	font-family: system-ui, sans-serif;
}
.fact-box h4 {
	font-size: 0.62rem;
	font-weight: 800;
	letter-spacing: 2.5px;
	text-transform: uppercase;
	color: #555;
	margin: 0 0 0.5rem;
}
.fact-box dl {
	display: grid;
	grid-template-columns: max-content 1fr;
	gap: 0.25rem 1rem;
	margin: 0;
}
.fact-box dt { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: #666; white-space: nowrap; }
.fact-box dd { font-size: 0.85rem; margin: 0; color: #111; word-break: break-all; }
.doc-body pre {
	white-space: pre-wrap;
	word-break: break-word;
	font-family: Georgia, serif;
	font-size: 0.95rem;
	line-height: 1.75;
	margin: 0;
}
nav.back-nav { font-family: system-ui, sans-serif; font-size: 0.85rem; margin-bottom: 1.5rem; padding-top: 0.75rem; }

/* Footer */
.site-footer {
	margin-top: 3rem;
	border-top: 4px double #111;
	padding: 1rem 0 2rem;
	font-family: system-ui, sans-serif;
	font-size: 0.72rem;
	color: #888;
	text-align: center;
}
`

const frontTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>FATBABY Financial Intelligence</title>
<style>` + siteCSS + `</style>
</head>
<body>
<div class="wrap">

  <header class="masthead">
    <a href="/" class="masthead-name" style="color:#111;text-decoration:none;">FATBABY</a>
    <div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div>
  </header>

  <div class="edition-bar">
    <span>{{.Date}}</span>
    <span>{{.Count}} documents indexed</span>
  </div>

  {{if .TickerItems}}
  <div class="ticker-tape">
    <div class="ticker-inner">
      {{range .TickerItems}}<span class="ticker-seg"><b>{{.Ticker}}</b><span>{{.Label}}</span></span>{{end}}
      {{range .TickerItems}}<span class="ticker-seg"><b>{{.Ticker}}</b><span>{{.Label}}</span></span>{{end}}
    </div>
  </div>
  {{end}}

  <nav class="sections-rail">
    <a href="/section/governance">Governance</a>
    <a href="/section/activism">Activism Watch</a>
    <a href="/section/boardroom">Boardroom</a>
    <a href="/section/auditor">Auditor Watch</a>
    <a href="/section/pay">Pay &amp; Proxy</a>
    <a href="/wire">The Wire</a>
    <a href="/breaking">Breaking</a>
    <a href="/archive">Archive</a>
    <a href="/about">About</a>
  </nav>

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
        {{range .Secondary}}
        <article>
          <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
          <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
          <div class="dateline">{{.Dateline}}</div>
          <div class="byline">{{.Byline}}</div>
          <p class="deck" style="font-size:0.9rem;">{{.Deck}}</p>
        </article>
        {{end}}
      </div>
      {{end}}

      {{if .Rest}}
      <p class="section-hdr">All Documents</p>
      {{range .Rest}}
      <article class="story">
        <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
        <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
        <div class="dateline">{{.Dateline}}</div>
        <div class="byline">{{.Byline}}</div>
        <p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>
      </article>
      {{end}}
      {{end}}
    </main>

    <aside class="sidebar">
      {{if .MostActive}}
      <div class="sidebar-box">
        <h4>Most Active Tickers</h4>
        {{range .MostActive}}
        <div class="sidebar-row">
          <a class="sym" href="/company/{{.Ticker}}">{{.Ticker}}</a>
          <span class="cnt">{{.DocCount}} docs</span>
        </div>
        {{end}}
      </div>
      {{end}}

      {{if .WireItems}}
      <div class="sidebar-box">
        <h4>The Wire</h4>
        {{range .WireItems}}
        <div class="wire-item">
          <span class="badge badge-wire">Wire</span>
          <a href="/doc/{{.Identity}}" style="font-family:system-ui;font-weight:600;font-size:0.82rem;color:#111;">{{.Ticker}}{{if .Form}} · {{.Form}}{{end}}</a>
          <div style="font-family:system-ui;font-size:0.7rem;color:#888;margin-top:0.1rem;">{{.DateStr}}</div>
        </div>
        {{end}}
      </div>
      {{end}}

      <div class="sidebar-box">
        <h4>Signals</h4>
        <p style="font-size:0.8rem;color:#888;margin:0;">Governance signals surface here once the processor has run. <a href="/breaking">Breaking signals →</a></p>
      </div>
    </aside>
  </div>

  {{else}}
  <main style="max-width:700px;padding:2rem 0;">
    <p>No source documents have been persisted yet. The processor has not run or no filings have been discovered.</p>
  </main>
  {{end}}

</div>
<footer class="site-footer">
  FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.
  &nbsp;&middot;&nbsp; <a href="/about">About &amp; methodology</a>
  &nbsp;&middot;&nbsp; <a href="/archive">Archive</a>
</footer>
</body>
</html>`

const detailTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — FATBABY</title>
<style>` + siteCSS + `</style>
</head>
<body>
<div class="wrap">

  <header class="masthead">
    <a href="/" class="masthead-name" style="color:#111;text-decoration:none;">FATBABY</a>
    <div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div>
  </header>

  <div class="edition-bar">
    <span>{{.DateStr}}</span>
    <span><a href="/">← Front page</a></span>
  </div>

  <nav class="sections-rail">
    <a href="/section/governance">Governance</a>
    <a href="/section/activism">Activism Watch</a>
    <a href="/section/boardroom">Boardroom</a>
    <a href="/section/auditor">Auditor Watch</a>
    <a href="/section/pay">Pay &amp; Proxy</a>
    <a href="/wire">The Wire</a>
    <a href="/breaking">Breaking</a>
    <a href="/archive">Archive</a>
    <a href="/about">About</a>
  </nav>

  <div class="reading-col" style="padding:1.5rem 0;">
    <nav class="back-nav"><a href="/">← All documents</a>{{if .Ticker}} &nbsp;&middot;&nbsp; <a href="/company/{{.Ticker}}">{{.Ticker}} desk →</a>{{end}}</nav>

    <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
    <h1 class="hl-lead" style="margin-bottom:0.5rem;">{{.Headline}}</h1>
    <div class="dateline">{{.Dateline}}</div>
    <div class="byline">{{.Byline}}</div>

    <div class="fact-box">
      <h4>Filing Details</h4>
      <dl>
        <dt>Source</dt>
        <dd>{{.SourceLabel}}</dd>
        {{if .Form}}<dt>Form</dt><dd>{{.Form}}</dd>{{end}}
        <dt>Persisted</dt>
        <dd>{{.DateStr}}</dd>
        {{if .CharCount}}<dt>Length</dt><dd>{{.CharCount}} characters</dd>{{end}}
        {{if .DocumentURL}}<dt>Original</dt>
        <dd>{{if .IsExternalLink}}<a href="{{.DocumentURL}}" rel="noopener">{{.DocumentURL}}</a>{{else}}{{.DocumentURL}}{{end}}</dd>{{end}}
      </dl>
    </div>

    <hr class="rule">
    <div class="doc-body">
      <pre>{{.FullText}}</pre>
    </div>
  </div>

</div>
<footer class="site-footer">
  FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.
  &nbsp;&middot;&nbsp; <a href="/about">About &amp; methodology</a>
  &nbsp;&middot;&nbsp; <a href="/archive">Archive</a>
</footer>
</body>
</html>`

// BreakingView is the template data for /breaking.
type BreakingView struct {
	Items []ArticleView
}

const breakingTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Breaking — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
<header class="masthead"><a href="/" style="color:#111;text-decoration:none;" class="masthead-name">FATBABY</a>
<div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div></header>
<nav class="sections-rail">
<a href="/">Front Page</a><a href="/section/governance">Governance</a><a href="/section/activism">Activism Watch</a>
<a href="/section/boardroom">Boardroom</a><a href="/section/auditor">Auditor Watch</a>
<a href="/section/pay">Pay &amp; Proxy</a><a href="/wire">The Wire</a><a href="/archive">Archive</a></nav>
<main style="max-width:760px;padding:1rem 0;">
<h2 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#dc2626;margin:0 0 1.2rem;">Breaking &amp; High-Priority Signals</h2>
{{if .Items}}
{{range .Items}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
  <p class="deck">{{.Deck}}</p>
</article>
{{end}}
{{else}}
<p>No critical or high-priority signals in the last 48 hours.</p>
{{end}}
</main></div>
<footer class="site-footer">FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.</footer>
</body></html>`

// SectionView is the template data for /section/{slug}.
type SectionView struct {
	Slug  string
	Title string
	Blurb string
	Items []ArticleView
}

const sectionTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
<header class="masthead"><a href="/" style="color:#111;text-decoration:none;" class="masthead-name">FATBABY</a>
<div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div></header>
<nav class="sections-rail">
<a href="/">Front Page</a><a href="/section/governance">Governance</a><a href="/section/activism">Activism Watch</a>
<a href="/section/boardroom">Boardroom</a><a href="/section/auditor">Auditor Watch</a>
<a href="/section/pay">Pay &amp; Proxy</a><a href="/wire">The Wire</a><a href="/archive">Archive</a></nav>
<main style="max-width:760px;padding:1rem 0;">
<h2 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#888;margin:0 0 0.3rem;">{{.Title}}</h2>
{{if .Blurb}}<p style="font-family:system-ui;font-size:0.88rem;color:#555;margin:0 0 1.2rem;border-bottom:1px solid #ddd;padding-bottom:0.75rem;">{{.Blurb}}</p>{{end}}
{{if .Items}}
{{range .Items}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
  <p class="deck">{{.Deck}}</p>
</article>
{{end}}
{{else}}
<p>No signals in this section yet.</p>
{{end}}
</main></div>
<footer class="site-footer">FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.</footer>
</body></html>`

// CompanyView is the template data for /company/{ticker}.
type CompanyView struct {
	Ticker  string
	Signals []ArticleView
	Docs    []ArticleView
}

const companyTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Ticker}} — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
<header class="masthead"><a href="/" style="color:#111;text-decoration:none;" class="masthead-name">FATBABY</a>
<div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div></header>
<nav class="sections-rail">
<a href="/">Front Page</a><a href="/section/governance">Governance</a><a href="/section/activism">Activism Watch</a>
<a href="/section/boardroom">Boardroom</a><a href="/section/auditor">Auditor Watch</a>
<a href="/section/pay">Pay &amp; Proxy</a><a href="/wire">The Wire</a><a href="/archive">Archive</a></nav>
<main style="max-width:760px;padding:1rem 0;">
<h1 class="hl-lead" style="border-bottom:2px solid #111;padding-bottom:0.5rem;margin-bottom:1.2rem;">{{.Ticker}} Desk</h1>
{{if .Signals}}
<p class="section-hdr">Governance Signals</p>
{{range .Signals}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h2 class="hl-secondary"><a href="{{.Link}}">{{.Headline}}</a></h2>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
  <p class="deck">{{.Deck}}</p>
</article>
{{end}}
{{end}}
{{if .Docs}}
<p class="section-hdr" style="margin-top:1.5rem;">Filings &amp; Documents</p>
{{range .Docs}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
  <p class="deck" style="font-size:0.88rem;">{{.Deck}}</p>
</article>
{{end}}
{{end}}
{{if and (not .Signals) (not .Docs)}}
<p>No data for {{.Ticker}} yet.</p>
{{end}}
</main></div>
<footer class="site-footer">FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.</footer>
</body></html>`

// ArchiveView is the template data for /archive.
type ArchiveView struct {
	Entries []ArticleView
}

const archiveTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Archive — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
<header class="masthead"><a href="/" style="color:#111;text-decoration:none;" class="masthead-name">FATBABY</a>
<div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div></header>
<nav class="sections-rail">
<a href="/">Front Page</a><a href="/section/governance">Governance</a><a href="/section/activism">Activism Watch</a>
<a href="/wire">The Wire</a><a href="/breaking">Breaking</a><a href="/about">About</a></nav>
<main style="max-width:760px;padding:1rem 0;">
<h2 style="font-family:system-ui;font-size:0.65rem;font-weight:800;letter-spacing:2.5px;text-transform:uppercase;color:#888;margin:0 0 1rem;">The Morgue — Full Archive</h2>
{{range .Entries}}
<article class="story">
  <span class="kicker {{.KickerClass}}">{{.Kicker}}</span>
  <h3 class="hl-item"><a href="{{.Link}}">{{.Headline}}</a></h3>
  <div class="dateline">{{.Dateline}}</div>
  <div class="byline">{{.Byline}}</div>
</article>
{{end}}
{{if not .Entries}}<p>No documents in the archive yet.</p>{{end}}
</main></div>
<footer class="site-footer">FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.</footer>
</body></html>`

const aboutTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>About — FATBABY</title><style>` + siteCSS + `</style></head>
<body><div class="wrap">
<header class="masthead"><a href="/" style="color:#111;text-decoration:none;" class="masthead-name">FATBABY</a>
<div class="masthead-tagline">Financial Intelligence · Governance &amp; Signal Reporting</div></header>
<nav class="sections-rail">
<a href="/">Front Page</a><a href="/wire">The Wire</a><a href="/archive">Archive</a></nav>
<main style="max-width:700px;padding:1.5rem 0;" class="reading-col">
<h1 class="hl-lead">Masthead &amp; Methodology</h1>
<hr class="rule-heavy">
<h2 style="font-family:system-ui;font-size:0.85rem;font-weight:700;margin:1.5rem 0 0.5rem;">What is FATBABY?</h2>
<p>FATBABY Financial Intelligence is a Go-based pipeline that watches SEC EDGAR filings and press releases,
extracts governance signals, and presents them as a structured publication. It is not a news organisation,
not a registered investment adviser, and not affiliated with any company it covers.</p>
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
<p>All signals are model-derived from public filings. They are not investment advice and should not be relied
upon for trading decisions. Confidence scores reflect model calibration, not certainty.</p>
</main></div>
<footer class="site-footer">FATBABY Financial Intelligence &mdash; signals are model-derived, not investment advice.</footer>
</body></html>`
