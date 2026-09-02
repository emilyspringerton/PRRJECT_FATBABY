package intelligence

import "time"

// SourceDocument is the payload for a source_document_persisted event.
// It captures cleaned plain-text content of a filing or press release
// before any LLM analysis, preserving the primary source for audit,
// search, and downstream enrichment.
type SourceDocument struct {
	// Identity is the canonical filing identity: "<normalized_cik>:<accession_number>".
	// For press releases it may be a URL-derived identity.
	Identity string `json:"identity"`

	// Ticker is the equity ticker associated with this document.
	Ticker string `json:"ticker"`

	// SourceType is "sec_8k", "press_release", or similar — matches the
	// kind label used when building the LLM prompt. This is the real,
	// existing top-level content-type field for this whole pipeline (see
	// SourceTypeForForm for the SEC-side values); the wider taxonomy today
	// also includes "market_movers" and "emily_commentary" downstream in
	// newssite, though those never flow through SourceDocument itself.
	SourceType string `json:"source_type"`

	// SourceProvider is the specific wire service or feed within
	// SourceType -- the real subtype dimension SourceType alone doesn't
	// carry. Only meaningful where SourceType=="press_release" today
	// ("prnewswire", "businesswire", ...); empty for SEC filings, which
	// have no equivalent "which provider" concept (EDGAR is the one and
	// only source). Added 2026-09-02 (founder real-time: "add businesswire
	// prs to our data sources same as prnewswire... tagged as... type
	// pressreleases... individual businesswire and prnewswire as options
	// for prtype") -- mirrors prwatch's own PressReleaseDiscovered.Source,
	// which is where this value actually originates.
	SourceProvider string `json:"source_provider,omitempty"`

	// Form is the SEC form type (e.g. "8-K") or empty for press releases.
	Form string `json:"form"`

	// DocumentURL is the URL that was fetched.
	DocumentURL string `json:"document_url"`

	// CleanedText is the full cleaned plain-text content.
	CleanedText string `json:"cleaned_text"`

	// CleanedCharCount is len(CleanedText) — stored for fast filtering without
	// loading the full text.
	CleanedCharCount int `json:"cleaned_char_count"`

	// FilingDate is the date the document was filed with the SEC (e.g. "2019-04-26"),
	// taken from the EDGAR submissions metadata. For press releases this is empty.
	// Always use this for display; PersistedAt is the pipeline-index timestamp.
	FilingDate string `json:"filing_date,omitempty"`

	// PersistedAt is the UTC time this event was written to the pipeline store.
	PersistedAt time.Time `json:"persisted_at"`
}
