// Package fabledata is the FABLE data engine (HQ-SPEC-AI-103 §4a, build order
// step 1): it reads the EPS headline corpus -- the richest oracle-graded set
// in the house -- and produces normalized training examples with per-record
// provenance, packaged into an immutable, content-addressed snapshot
// manifest. Training never reads live stores; it reads snapshots.
//
// v0 scope (S145-01): join var/eps/articles.ndjson (the generated headline
// text) against var/eps/oracle.ndjson (the reality-rooted grade, from the
// filed 8-K) by SourceIdentity. Per the spec's Löbian rule #1 ("no FABLE
// model's outputs may enter a training snapshot unless graded by a
// reality-rooted oracle first"), only resolved verdicts (confirmed/
// contradicts) are eligible -- pending/unavailable/unresolvable cases have
// no reality-rooted grade yet and are excluded, not included-and-flagged.
// Dedup is exact-hash for v0 (the SourceIdentity join key already prevents
// duplicate records by construction); minhash fuzzy dedup is out of scope
// here, a later filter per the spec's "filters as versioned config" design,
// not a v0 requirement.
package fabledata

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/example/prrject-fatbaby/internal/eps"
)

// LicenseClassOwnExhaust marks a record as EINHORN's own data exhaust --
// the primary diet per HQ-SPEC-AI-103 §5, rule 3 (frontier distillation is
// a separate licensing decision, not assumed here).
const LicenseClassOwnExhaust = "own_exhaust"

// OracleNameEPSReconciler is the oracle that grades EPS headline examples:
// the filed 8-K, matched by cmd/eps-reconciler.
const OracleNameEPSReconciler = "eps_reconciler"

// Example is one normalized, oracle-graded training example with full
// provenance: source event hash, the oracle that labeled it, label date,
// and license class -- exactly the fields HQ-SPEC-AI-103 §4a requires.
type Example struct {
	// ID is the content address of this example: sha256 of SourceIdentity.
	// Stable across rebuilds -- the same source record always gets the same
	// ID, which is what makes contamination tombstoning by hash meaningful.
	ID string `json:"id"`
	// SourceHash is the content hash of the graded text itself (headline +
	// dek), distinct from ID (which addresses the source *record*, not its
	// text) -- lets a downstream consumer detect if the same SourceIdentity
	// ever produced different text across a re-run without re-deriving it.
	SourceHash     string   `json:"source_hash"`
	SourceIdentity string   `json:"source_identity"`
	Ticker         string   `json:"ticker"`
	Headline       string   `json:"headline"`
	Dek            string   `json:"dek"`
	ExtractedEPS   *float64 `json:"extracted_eps"`
	FiledEPS       *float64 `json:"filed_eps"`
	Verdict        string   `json:"verdict"`
	OracleName     string   `json:"oracle_name"`
	LabelDate      string   `json:"label_date"`
	LicenseClass   string   `json:"license_class"`
}

// article is the subset of eps.Article's on-disk JSON shape fabledata
// reads. Deliberately not importing eps.Article's own struct for this --
// fabledata is a separate component per the spec, and only needs a few of
// Article's fields; a local, minimal shape keeps that boundary honest
// rather than pulling in eps's full publishing-oriented type.
type article struct {
	SourceIdentity string `json:"source_identity"`
	Ticker         string `json:"ticker"`
	Headline       string `json:"headline"`
	Dek            string `json:"dek"`
}

func loadArticles(dir string) (map[string]article, error) {
	path := filepath.Join(dir, "articles.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]article)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var a article
		if err := json.Unmarshal(line, &a); err != nil {
			continue
		}
		if a.SourceIdentity == "" {
			continue
		}
		out[a.SourceIdentity] = a
	}
	return out, sc.Err()
}

func contentHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // separator, avoids "ab"+"c" == "a"+"bc" collisions
	}
	return hex.EncodeToString(h.Sum(nil))
}

// BuildExamples joins articles.ndjson against oracle.ndjson by
// SourceIdentity in epsDir (typically "var/eps"), keeping only reality-
// graded (confirmed/contradicts) cases, minus any ID present in tombstones.
// Returns examples sorted by ID for deterministic output.
func BuildExamples(epsDir string, tombstones map[string]bool) ([]Example, error) {
	articles, err := loadArticles(epsDir)
	if err != nil {
		return nil, err
	}
	cases, err := eps.LoadOracleCases(epsDir)
	if err != nil {
		return nil, err
	}

	var examples []Example
	for _, c := range cases {
		if c.Verdict != eps.VerdictConfirmed && c.Verdict != eps.VerdictContradicts {
			continue
		}
		a, ok := articles[c.SourceIdentity]
		if !ok {
			// A resolved oracle case with no matching article is a real gap
			// (the case exists but its generated headline text doesn't) --
			// skip rather than fabricate a partial example.
			continue
		}
		id := contentHash(c.SourceIdentity)
		if tombstones[id] {
			continue
		}
		examples = append(examples, Example{
			ID:             id,
			SourceHash:     contentHash(a.Headline, a.Dek),
			SourceIdentity: c.SourceIdentity,
			Ticker:         a.Ticker,
			Headline:       a.Headline,
			Dek:            a.Dek,
			ExtractedEPS:   c.ExtractedEPS,
			FiledEPS:       c.FiledEPS,
			Verdict:        string(c.Verdict),
			OracleName:     OracleNameEPSReconciler,
			LabelDate:      c.RecordedAt,
			LicenseClass:   LicenseClassOwnExhaust,
		})
	}

	sort.Slice(examples, func(i, j int) bool { return examples[i].ID < examples[j].ID })
	return examples, nil
}
