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
// or when they share the same last name and first initial (handles middle-initial
// variants like "Susan L. Wagner" ≈ "Sue Wagner" and "John L. Smith" ≈ "John Smith").
func NamesMatch(a, b string) bool {
	ca, cb := Canonicalize(a), Canonicalize(b)
	if ca == cb {
		return true
	}
	la, fa := lastAndFirstInitial(ca)
	lb, fb := lastAndFirstInitial(cb)
	return la != "" && fa != "" && la == lb && fa == fb
}

// nameSuffixes are name suffixes to ignore when extracting the last name.
var nameSuffixes = map[string]bool{"jr": true, "sr": true, "ii": true, "iii": true, "iv": true}

// lastAndFirstInitial extracts (last-name, first-initial) from a canonical slug,
// skipping middle initials (single-letter parts) and known suffixes.
func lastAndFirstInitial(slug string) (last, firstInitial string) {
	parts := strings.Split(slug, "-")
	if len(parts) == 0 {
		return "", ""
	}
	// Find last name: scan backward, skip suffixes and single-letter initials.
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if nameSuffixes[p] || len(p) == 1 {
			continue
		}
		last = p
		break
	}
	if last == "" || len(parts[0]) == 0 {
		return "", ""
	}
	firstInitial = string(parts[0][0])
	return last, firstInitial
}
