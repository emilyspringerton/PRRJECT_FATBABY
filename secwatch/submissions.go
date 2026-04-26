package secwatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

type submissionsDoc struct {
	CIK     string `json:"cik"`
	Filings struct {
		Recent struct {
			AccessionNumber    []string `json:"accessionNumber"`
			Form               []string `json:"form"`
			FilingDate         []string `json:"filingDate"`
			PrimaryDocument    []string `json:"primaryDocument"`
			AcceptanceDateTime []string `json:"acceptanceDateTime"`
		} `json:"recent"`
	} `json:"filings"`
}

func ParseRecentFilings(body []byte, ticker string) ([]Filing, error) {
	var doc submissionsDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode submissions: %w", err)
	}
	cik := NormalizeCIK(doc.CIK)
	recent := doc.Filings.Recent
	n := len(recent.AccessionNumber)
	if n == 0 {
		return []Filing{}, nil
	}
	if len(recent.Form) < n || len(recent.FilingDate) < n || len(recent.PrimaryDocument) < n {
		return nil, fmt.Errorf("recent arrays length mismatch")
	}
	out := make([]Filing, 0, n)
	for i := 0; i < n; i++ {
		f := Filing{
			Ticker:          strings.ToUpper(strings.TrimSpace(ticker)),
			CIK:             cik,
			AccessionNumber: strings.TrimSpace(recent.AccessionNumber[i]),
			Form:            strings.ToUpper(strings.TrimSpace(recent.Form[i])),
			FilingDate:      strings.TrimSpace(recent.FilingDate[i]),
			PrimaryDocument: strings.TrimSpace(recent.PrimaryDocument[i]),
			SubmissionsURL:  SubmissionsURL(cik),
		}
		if i < len(recent.AcceptanceDateTime) {
			f.AcceptanceDateTime = strings.TrimSpace(recent.AcceptanceDateTime[i])
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
