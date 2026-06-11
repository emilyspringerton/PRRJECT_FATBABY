package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// githubClient creates GitHub issues from Emily observations.
// All fields are required; a zero-value client is a no-op.
type githubClient struct {
	token string
	owner string
	repo  string
	http  *http.Client
}

func newGithubClient(token, owner, repo string) *githubClient {
	if token == "" || owner == "" || repo == "" {
		return nil
	}
	return &githubClient{
		token: token,
		owner: owner,
		repo:  repo,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

type issuePayload struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

type issueResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// createIssue opens a GitHub issue and returns its URL.
func (g *githubClient) createIssue(title, body string) (string, error) {
	if g == nil {
		return "", nil
	}
	payload := issuePayload{
		Title:  truncate(title, 256),
		Body:   body,
		Labels: []string{"emily-observation"},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", g.owner, g.repo)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var result issueResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.HTMLURL, nil
}

// issueBodyForObservation formats a Markdown issue body from a single observation.
func issueBodyForObservation(obs observation, latestPath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Timestamp:** %s\n", obs.Timestamp)
	if obs.Source != "" {
		fmt.Fprintf(&sb, "**Source:** %s\n", obs.Source)
	}
	if obs.Severity != "" {
		fmt.Fprintf(&sb, "**Severity:** %s\n", obs.Severity)
	}
	if obs.Status != "" {
		fmt.Fprintf(&sb, "**Status:** %s\n", obs.Status)
	}
	if obs.Summary != "" {
		fmt.Fprintf(&sb, "\n## Summary\n%s\n", obs.Summary)
	}
	if obs.Findings != "" {
		fmt.Fprintf(&sb, "\n## Findings\n%s\n", obs.Findings)
	}
	if obs.SuggestedFix != "" {
		fmt.Fprintf(&sb, "\n## Suggested fix\n%s\n", obs.SuggestedFix)
	}
	if obs.RequestForClaude != "" {
		fmt.Fprintf(&sb, "\n## Request\n%s\n", obs.RequestForClaude)
	}
	if len(obs.Gaps) > 0 {
		sb.WriteString("\n## Gaps\n")
		for _, g := range obs.Gaps {
			fmt.Fprintf(&sb, "- %s\n", g)
		}
	}
	if obs.FilingsProcessed > 0 || obs.DirectorsFound > 0 || obs.SignalsGenerated > 0 {
		fmt.Fprintf(&sb, "\n**Pipeline:** %d filings processed, %d directors, %d signals\n",
			obs.FilingsProcessed, obs.DirectorsFound, obs.SignalsGenerated)
	}
	fmt.Fprintf(&sb, "\n---\n*Filed automatically by observation-watcher from `%s`*\n", latestPath)
	return sb.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
