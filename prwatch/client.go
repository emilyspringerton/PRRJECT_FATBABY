package prwatch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://www.prnewswire.com"
	defaultListPath  = "/news-releases/news-releases-list/"
	defaultUserAgent = "prrject-fatbaby-prwatch/0.1 (contact: secops@example.com)"
)

var (
	cardRe      = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*newsreleaseconsolidatelink[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	h3Re        = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`)
	companyRe   = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*color-inherit[^"]*"[^>]*>(.*?)</a>`)
	paragraphRe = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	tagRe       = regexp.MustCompile(`(?is)<[^>]+>`)
)

type PRDiscovery struct {
	ID, Headline, URL, Company string
	Timestamp                  time.Time
}

type Client struct {
	hc          *http.Client
	baseURL, ua string
}

type ClientConfig struct {
	HTTPClient         *http.Client
	BaseURL, UserAgent string
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return &Client{hc: cfg.HTTPClient, baseURL: strings.TrimRight(cfg.BaseURL, "/"), ua: cfg.UserAgent}
}

func (c *Client) Discover(ctx context.Context) ([]PRDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+defaultListPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("prnewswire status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if err != nil {
		return nil, err
	}
	m := cardRe.FindAllStringSubmatch(string(body), -1)
	out := make([]PRDiscovery, 0, len(m))
	for _, sm := range m {
		href := sm[1]
		block := sm[2]
		h := textFromFirst(h3Re, block)
		if h == "" {
			continue
		}
		fullURL := resolveURL(c.baseURL, href)
		if fullURL == "" {
			continue
		}
		id := extractID(fullURL)
		if id == "" {
			id = fullURL
		}
		company := textFromFirst(companyRe, block)
		ts := parsePRNewswireTime(textFromFirst(paragraphRe, block))
		out = append(out, PRDiscovery{ID: id, Headline: h, URL: fullURL, Company: company, Timestamp: ts})
	}
	return out, nil
}

func textFromFirst(re *regexp.Regexp, in string) string {
	m := re.FindStringSubmatch(in)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(tagRe.ReplaceAllString(m[1], ""))
}
func resolveURL(base, href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	bu, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return bu.ResolveReference(u).String()
}
func extractID(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "-")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSuffix(parts[len(parts)-1], ".html")
	if len(last) >= 8 {
		return last
	}
	return ""
}
func parsePRNewswireTime(v string) time.Time {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\u00a0", " "))
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{"Jan 2, 2006, 08:04 ET", "Jan 2, 2006, 3:04 PM ET", "January 2, 2006, 3:04 PM ET"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
