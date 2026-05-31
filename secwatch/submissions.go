package secwatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// submissionsFile is one entry in filings.files — a paginated older-filing chunk.
type submissionsFile struct {
	Name string `json:"name"`
}

// filingArrays is the shared shape for both filings.recent (in the primary doc)
// and for paginated submissions files (whose root is the same array structure).
type filingArrays struct {
	AccessionNumber    []string `json:"accessionNumber"`
	Form               []string `json:"form"`
	FilingDate         []string `json:"filingDate"`
	PrimaryDocument    []string `json:"primaryDocument"`
	AcceptanceDateTime []string `json:"acceptanceDateTime"`
}

type submissionsDoc struct {
	CIK     string `json:"cik"`
	Filings struct {
		Recent filingArrays      `json:"recent"`
		Files  []submissionsFile `json:"files"`
	} `json:"filings"`
}

// ParseRecentFilings parses the primary submissions JSON for a ticker.
// It returns only the filings in the "recent" array; callers that want the full
// historical corpus should also call ParseFilingsPage for each entry in FilesPages.
func ParseRecentFilings(body []byte, ticker string) ([]Filing, error) {
	var doc submissionsDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode submissions: %w", err)
	}
	cik := NormalizeCIK(doc.CIK)
	return parseFilingArrays(doc.Filings.Recent, cik, ticker)
}

// SubmissionsPageNames returns the paginated file names embedded in a primary
// submissions JSON body. The caller should fetch each name via FetchSubmissionPage
// and merge results with ParseFilingsPage.
func SubmissionsPageNames(body []byte) ([]string, error) {
	var doc submissionsDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode submissions for page names: %w", err)
	}
	names := make([]string, 0, len(doc.Filings.Files))
	for _, f := range doc.Filings.Files {
		if f.Name != "" {
			names = append(names, f.Name)
		}
	}
	return names, nil
}

// ParseFilingsPage parses one paginated submissions file. The page JSON has the
// same array structure as filings.recent but without the outer cik/filings wrapper.
func ParseFilingsPage(body []byte, cik, ticker string) ([]Filing, error) {
	var arrays filingArrays
	if err := json.Unmarshal(body, &arrays); err != nil {
		return nil, fmt.Errorf("decode submissions page: %w", err)
	}
	return parseFilingArrays(arrays, NormalizeCIK(cik), ticker)
}

func parseFilingArrays(arrays filingArrays, cik, ticker string) ([]Filing, error) {
	n := len(arrays.AccessionNumber)
	if n == 0 {
		return []Filing{}, nil
	}
	if len(arrays.Form) < n || len(arrays.FilingDate) < n || len(arrays.PrimaryDocument) < n {
		return nil, fmt.Errorf("recent arrays length mismatch")
	}
	out := make([]Filing, 0, n)
	for i := 0; i < n; i++ {
		f := Filing{
			Ticker:          strings.ToUpper(strings.TrimSpace(ticker)),
			CIK:             cik,
			AccessionNumber: strings.TrimSpace(arrays.AccessionNumber[i]),
			Form:            strings.ToUpper(strings.TrimSpace(arrays.Form[i])),
			FilingDate:      strings.TrimSpace(arrays.FilingDate[i]),
			PrimaryDocument: DocumentURL(cik, strings.TrimSpace(arrays.AccessionNumber[i]), strings.TrimSpace(arrays.PrimaryDocument[i])),
			SubmissionsURL:  SubmissionsURL(cik),
		}
		if i < len(arrays.AcceptanceDateTime) {
			f.AcceptanceDateTime = strings.TrimSpace(arrays.AcceptanceDateTime[i])
		}
		if f.AccessionNumber == "" {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func FilterByAllowedForms(in []Filing, allowed []string) []Filing {
	if len(allowed) == 0 {
		return append([]Filing{}, in...)
	}
	set := map[string]struct{}{}
	for _, form := range allowed {
		set[strings.ToUpper(strings.TrimSpace(form))] = struct{}{}
	}
	out := make([]Filing, 0, len(in))
	for _, f := range in {
		if _, ok := set[strings.ToUpper(strings.TrimSpace(f.Form))]; ok {
			out = append(out, f)
		}
	}
	return out
}
