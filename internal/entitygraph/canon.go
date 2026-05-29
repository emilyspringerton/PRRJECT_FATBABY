package entitygraph

import (
	"strings"
	"unicode"
)

// Canonicalize converts a person name to a stable kebab-case slug for use as
// a node identity key. The result is deterministic for equivalent name forms.
// e.g. "Frank C. Herringer" → "frank-c-herringer"
//
//	"Carolyn Schwab-Pomerantz" → "carolyn-schwab-pomerantz"
func Canonicalize(name string) string {
	name = strings.TrimSpace(name)
	// Remove honorifics and trailing punctuation.
	name = strings.NewReplacer(
		"Mr. ", "", "Ms. ", "", "Mrs. ", "", "Dr. ", "", "Prof. ", "",
	).Replace(name)

	var b strings.Builder
	prev := '-'
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prev = r
		} else if r == '-' || r == '\'' {
			// Preserve hyphens in compound names; drop apostrophes.
			if r == '-' && prev != '-' {
				b.WriteRune('-')
				prev = '-'
			}
		} else {
			// Space, dot, comma → separator.
			if prev != '-' && b.Len() > 0 {
				b.WriteRune('-')
				prev = '-'
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	// Collapse repeated hyphens.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// NamesMatch returns true when two names canonicalize to the same slug,
// or when one is a prefix of the other (handles "J. Smith" ≈ "John Smith").
func NamesMatch(a, b string) bool {
	ca, cb := Canonicalize(a), Canonicalize(b)
	if ca == cb {
		return true
	}
	// Allow initial-only first name: "j-smith" matches "john-smith"
	pa, pb := namePrefix(ca), namePrefix(cb)
	return pa == pb && pa != ""
}

// namePrefix returns <first-initial>-<last-name> from a canonical slug.
func namePrefix(slug string) string {
	parts := strings.SplitN(slug, "-", 2)
	if len(parts) < 2 || len(parts[0]) == 0 {
		return ""
	}
	return string(parts[0][0]) + "-" + parts[len(parts)-1]
}
