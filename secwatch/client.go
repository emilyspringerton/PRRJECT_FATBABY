package secwatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

const (
	defaultUA             = "prrject-fatbaby-secwatch/0.1 (contact: secops@example.com)"
	defaultTimeout        = 25 * time.Second
	defaultRateLimitRPS   = 2.0
	defaultRateLimitBurst = 3
	defaultMaxRetries     = 4
	defaultBackoffBase    = 500 * time.Millisecond
	defaultBackoffCap     = 8 * time.Second
)

type ClientConfig struct {
	BaseURL      string
	UserAgent    string
	Timeout      time.Duration
	RateLimitRPS float64
	RateBurst    int
	MaxRetries   int
	BackoffBase  time.Duration
	BackoffCap   time.Duration
	HTTPClient   *http.Client
	Random       *rand.Rand
}

type Client struct {
	baseURL string
	ua      string
	http    *http.Client
	limiter *tokenBucket
	cfg     ClientConfig
	rand    *rand.Rand

	mu         sync.Mutex
	extraDelay time.Duration
	consec5xx  int
}

type HTTPStatusError struct {
	StatusCode int
	URL        string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("sec response status=%d url=%s", e.StatusCode, e.URL)
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://data.sec.gov"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUA
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = defaultRateLimitRPS
	}
	if cfg.RateBurst <= 0 {
		cfg.RateBurst = defaultRateLimitBurst
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = defaultBackoffBase
	}
	if cfg.BackoffCap <= 0 {
		cfg.BackoffCap = defaultBackoffCap
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}
	if cfg.Random == nil {
		cfg.Random = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	return &Client{
		baseURL: cfg.BaseURL,
		ua:      cfg.UserAgent,
		http:    cfg.HTTPClient,
		limiter: newTokenBucket(cfg.RateLimitRPS, cfg.RateBurst),
		cfg:     cfg,
		rand:    cfg.Random,
	}
}

func (c *Client) FetchSubmissions(ctx context.Context, cik string) ([]byte, error) {
	cik = NormalizeCIK(cik)
	if cik == "" {
		return nil, errors.New("cik required")
	}
	reqURL := c.baseURL + "/submissions/CIK" + cik + ".json"

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if err := c.waitForTurn(ctx); err != nil {
			return nil, err
		}
		body, status, err := c.doOnce(ctx, reqURL)
		if err == nil {
			c.noteSuccess(status)
			return body, nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == c.cfg.MaxRetries {
			break
		}
		c.noteFailure(err)
		if err := sleepWithBackoff(ctx, c.backoffFor(attempt)); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("fetch submissions cik=%s: %w", cik, lastErr)
}

func (c *Client) doOnce(ctx context.Context, reqURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, &HTTPStatusError{StatusCode: resp.StatusCode, URL: reqURL, Body: string(b)}
	}
	return b, resp.StatusCode, nil
}

func (c *Client) waitForTurn(ctx context.Context) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	delay := c.extraDelay
	c.mu.Unlock()
	if delay <= 0 {
		return nil
	}
	return sleepWithBackoff(ctx, delay)
}

func (c *Client) noteSuccess(status int) {
	if status >= 500 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consec5xx = 0
	if c.extraDelay > 100*time.Millisecond {
		c.extraDelay /= 2
	}
}

func (c *Client) noteFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		switch {
		case statusErr.StatusCode == http.StatusForbidden || statusErr.StatusCode == http.StatusTooManyRequests:
			c.bumpDelay(2 * time.Second)
			c.consec5xx = 0
		case statusErr.StatusCode >= 500 && statusErr.StatusCode <= 599:
			c.consec5xx++
			if c.consec5xx >= 2 {
				c.bumpDelay(1500 * time.Millisecond)
			}
		default:
			c.consec5xx = 0
		}
	}
}

func (c *Client) bumpDelay(add time.Duration) {
	c.extraDelay += add
	if c.extraDelay > 12*time.Second {
		c.extraDelay = 12 * time.Second
	}
}

func (c *Client) backoffFor(attempt int) time.Duration {
	base := c.cfg.BackoffBase << attempt
	if base > c.cfg.BackoffCap {
		base = c.cfg.BackoffCap
	}
	j := time.Duration(c.rand.Int63n(int64(base/2 + 1)))
	return base/2 + j
}

func isRetryable(err error) bool {
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusTooManyRequests ||
			statusErr.StatusCode == http.StatusForbidden ||
			(statusErr.StatusCode >= 500 && statusErr.StatusCode <= 599)
	}
	return true
}

func sleepWithBackoff(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type tokenBucket struct {
	mu       sync.Mutex
	interval time.Duration
	burst    int
	tokens   float64
	last     time.Time
}

func newTokenBucket(rps float64, burst int) *tokenBucket {
	return &tokenBucket{
		interval: time.Duration(float64(time.Second) / rps),
		burst:    burst,
		tokens:   float64(burst),
		last:     time.Now(),
	}
}

func (tb *tokenBucket) Wait(ctx context.Context) error {
	for {
		tb.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(tb.last)
		if elapsed > 0 {
			tb.tokens += float64(elapsed) / float64(tb.interval)
			if tb.tokens > float64(tb.burst) {
				tb.tokens = float64(tb.burst)
			}
			tb.last = now
		}
		if tb.tokens >= 1 {
			tb.tokens -= 1
			tb.mu.Unlock()
			return nil
		}
		need := (1 - tb.tokens) * float64(tb.interval)
		wait := time.Duration(need)
		tb.mu.Unlock()

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
