// Package research is the Emily Research Engine — a transparent, auditable
// multi-source research library. Every step (fetch, extract, synthesize)
// emits a structured event to the streamlog so results are fully traceable.
//
// Usage:
//
//	log := streamlog.Stdout()
//	e := research.NewEngine(log)
//	result, err := e.Research(ctx, "supply chain disruption semiconductor Q3 2026", []string{"financial","supply_chain"})
//	// result.Content contains fetched page text keyed by source URL
//	// result.RedditPosts contains matched Reddit posts
//	// result.ExternalLinks contains URLs spidered out from Reddit
package research

import (
	"context"
	"time"

	"github.com/example/prrject-fatbaby/internal/spider"
	"github.com/example/prrject-fatbaby/internal/spider/reddit"
	"github.com/example/prrject-fatbaby/internal/streamlog"
)

// Domain tags for filtering research sources.
const (
	DomainFinancial    = "financial"
	DomainSupplyChain  = "supply_chain"
	DomainAI           = "ai"
)

// Source is a registered static research source (webpage/API, not Reddit).
type Source struct {
	Name   string
	URL    string
	Domain string
}

// DefaultSources is the built-in source registry.
var DefaultSources = []Source{
	{Name: "SEC EDGAR Full-Text Search", URL: "https://efts.sec.gov/LATEST/search-index?q=%s&dateRange=custom&startdt=%s&enddt=%s", Domain: DomainFinancial},
	{Name: "Reuters Business", URL: "https://www.reuters.com/business/", Domain: DomainFinancial},
	{Name: "Bloomberg Markets", URL: "https://www.bloomberg.com/markets", Domain: DomainFinancial},
	{Name: "Supply Chain Dive", URL: "https://www.supplychaindive.com/", Domain: DomainSupplyChain},
	{Name: "Logistics MgmtMag", URL: "https://www.logisticsmgmt.com/", Domain: DomainSupplyChain},
	{Name: "MIT Tech Review AI", URL: "https://www.technologyreview.com/topic/artificial-intelligence/", Domain: DomainAI},
	{Name: "Ars Technica", URL: "https://arstechnica.com/", Domain: DomainAI},
}

// PageResult is the fetched content from one static source.
type PageResult struct {
	Source    Source
	FetchedAt time.Time
	Title     string
	BodyText  string // truncated to 4000 chars for synthesis
	Error     string
}

// Result is the complete output of one Research call.
type Result struct {
	Query         string
	StartedAt     time.Time
	FinishedAt    time.Time
	Domains       []string
	Pages         []PageResult
	RedditPosts   []reddit.Post
	ExternalLinks []string
}

// Engine orchestrates multi-source research with streaming log output.
type Engine struct {
	log     *streamlog.Logger
	spider  *spider.Spider
	reddit  *reddit.Client
	sources []Source
}

// NewEngine returns an Engine with the default source registry.
func NewEngine(log *streamlog.Logger) *Engine {
	s := &spider.Spider{
		Log:       log,
		RateLimit: 500 * time.Millisecond,
	}
	r := reddit.New(log)
	return &Engine{
		log:     log,
		spider:  s,
		reddit:  r,
		sources: DefaultSources,
	}
}

// WithSources replaces the source registry. Useful for tests.
func (e *Engine) WithSources(srcs []Source) *Engine {
	e.sources = srcs
	return e
}

// Research performs multi-source research for a query across the requested domains.
// It fetches static sources, pulls recent Reddit posts from domain-appropriate
// subreddits, and then follows external links found in those Reddit posts.
// All activity is streamed to the Engine's Logger.
func (e *Engine) Research(ctx context.Context, query string, domains []string) (*Result, error) {
	e.log.Info("research", "starting research", map[string]any{
		"query":   query,
		"domains": domains,
	})

	result := &Result{
		Query:     query,
		StartedAt: time.Now().UTC(),
		Domains:   domains,
	}

	domainSet := make(map[string]bool, len(domains))
	for _, d := range domains {
		domainSet[d] = true
	}

	// Phase 1: Fetch static sources matching the requested domains.
	for _, src := range e.sources {
		if !domainSet[src.Domain] {
			continue
		}
		e.log.Info("research", "fetching static source: "+src.Name, map[string]any{"domain": src.Domain})
		pr := PageResult{Source: src, FetchedAt: time.Now().UTC()}
		page, err := e.spider.Fetch(ctx, src.URL)
		if err != nil {
			pr.Error = err.Error()
			e.log.Warn("research", "static source failed: "+src.Name, map[string]any{"error": err.Error()})
		} else {
			pr.Title = page.Title
			if len(page.BodyText) > 4000 {
				pr.BodyText = page.BodyText[:4000]
			} else {
				pr.BodyText = page.BodyText
			}
		}
		result.Pages = append(result.Pages, pr)
	}

	// Phase 2: Pull Reddit posts from domain-appropriate subreddits.
	subs := redditSubsForDomains(domainSet)
	if len(subs) > 0 {
		since := time.Now().UTC().Add(-72 * time.Hour) // last 3 days
		e.log.Info("research", "streaming Reddit", map[string]any{
			"subs":  subs,
			"since": since.Format(time.RFC3339),
		})
		for p := range e.reddit.Stream(ctx, subs, since) {
			result.RedditPosts = append(result.RedditPosts, p)
		}
		e.log.Info("research", "Reddit fetch complete", map[string]any{
			"post_count": len(result.RedditPosts),
		})
	}

	// Phase 3: Spider out from external links found in Reddit posts.
	extLinks := reddit.ExternalLinks(result.RedditPosts)
	if len(extLinks) > 10 {
		extLinks = extLinks[:10] // cap to 10 to avoid runaway fetches
	}
	result.ExternalLinks = extLinks
	if len(extLinks) > 0 {
		e.log.Info("research", "spidering external links from Reddit", map[string]any{
			"count": len(extLinks),
		})
		for res := range e.spider.FetchMulti(ctx, extLinks) {
			if res.Err != nil {
				continue
			}
			body := res.Page.BodyText
			if len(body) > 2000 {
				body = body[:2000]
			}
			result.Pages = append(result.Pages, PageResult{
				Source:    Source{Name: "reddit-link", URL: res.Page.URL, Domain: "reddit-spider"},
				FetchedAt: res.Page.FetchedAt,
				Title:     res.Page.Title,
				BodyText:  body,
			})
		}
	}

	result.FinishedAt = time.Now().UTC()
	elapsed := result.FinishedAt.Sub(result.StartedAt)
	e.log.Done("research", "research complete", map[string]any{
		"query":          query,
		"pages_fetched":  len(result.Pages),
		"reddit_posts":   len(result.RedditPosts),
		"external_links": len(result.ExternalLinks),
		"elapsed_ms":     elapsed.Milliseconds(),
	})
	return result, nil
}

// ResearchSupplyChain is a convenience wrapper for supply chain + financial research.
func (e *Engine) ResearchSupplyChain(ctx context.Context, product string) (*Result, error) {
	return e.Research(ctx, "supply chain disruption "+product, []string{DomainSupplyChain, DomainFinancial})
}

func redditSubsForDomains(domains map[string]bool) []string {
	var subs []string
	seen := make(map[string]bool)
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			subs = append(subs, s)
		}
	}
	if domains[DomainFinancial] {
		for _, s := range reddit.FinancialSubs {
			add(s)
		}
	}
	if domains[DomainSupplyChain] {
		for _, s := range reddit.SupplyChainSubs {
			add(s)
		}
	}
	if domains[DomainAI] {
		for _, s := range reddit.AITechSubs {
			add(s)
		}
	}
	return subs
}
