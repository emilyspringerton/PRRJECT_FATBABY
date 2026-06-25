package earningscal

import (
	"regexp"
	"strings"
)

// CompanyResolver maps normalized company names to tickers using a watchlist.
// Matching is fuzzy: strips common legal suffixes and punctuation before comparing.
type CompanyResolver struct {
	// normalized company name → ticker
	byNorm map[string]string
	// all tickers for direct lookup
	tickerSet map[string]bool
}

var reLegalSuffix = regexp.MustCompile(
	`(?i)\s+(?:inc\.?|corp\.?|corporation|ltd\.?|limited|llc\.?|llp\.?|plc\.?|sa|ag|nv|bv|se|co\.?)\s*$`)

var reNonAlpha = regexp.MustCompile(`[^a-z0-9 ]+`)

// normalizeName lowercases, strips legal suffixes, and removes non-alphanumeric chars.
func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = reLegalSuffix.ReplaceAllString(s, "")
	s = reNonAlpha.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// NewCompanyResolver builds a resolver from a slice of (ticker, companyName) pairs.
func NewCompanyResolver(entries [][2]string) *CompanyResolver {
	r := &CompanyResolver{
		byNorm:    make(map[string]string, len(entries)),
		tickerSet: make(map[string]bool, len(entries)),
	}
	for _, e := range entries {
		ticker := strings.ToUpper(e[0])
		r.tickerSet[ticker] = true
		norm := normalizeName(e[1])
		if norm != "" {
			r.byNorm[norm] = ticker
		}
	}
	return r
}

// Resolve returns the ticker for a company name, or "" if no match is found.
// Tries: direct normalized match, then prefix/substring on long names (≥20 chars).
func (r *CompanyResolver) Resolve(companyName string) string {
	// Strip timestamp prefixes like "16:17 ET\n\t\t\tCompany Name" from raw PR bodies.
	companyName = CleanIssuer(companyName)
	if companyName == "" {
		return ""
	}

	// Direct ticker lookup (company IS a ticker already).
	up := strings.ToUpper(strings.TrimSpace(companyName))
	if r.tickerSet[up] {
		return up
	}

	// Exact normalized match.
	norm := normalizeName(companyName)
	if t := r.byNorm[norm]; t != "" {
		return t
	}

	// Prefix match: some PR issuers include extra words after the company name.
	for key, ticker := range r.byNorm {
		if len(key) >= 4 && strings.HasPrefix(norm, key) {
			return ticker
		}
	}

	// Substring match for very long company names containing the normalized key.
	if len(norm) > 20 {
		for key, ticker := range r.byNorm {
			if len(key) >= 6 && strings.Contains(norm, key) {
				return ticker
			}
		}
	}

	return ""
}

// CleanIssuer strips the timestamp/whitespace prefix that appears in raw PR
// body issuer fields: "16:17 ET\n\t\t\t\n\t\t\t\nActual Company Name"
func CleanIssuer(s string) string {
	// Find last non-whitespace segment after newlines (the real company name is last).
	parts := strings.Split(s, "\n")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		// Skip bare timestamps like "16:17 ET" — they're PR header noise.
		if reTimestamp.MatchString(p) {
			continue
		}
		return p
	}
	return strings.TrimSpace(s)
}

var reTimestamp = regexp.MustCompile(`^\d{1,2}:\d{2}\s+[A-Z]{2,3}$`)
