package secwatch

import (
	"fmt"
	"strings"
)

type Filing struct {
	Ticker             string
	CIK                string
	AccessionNumber    string
	Form               string
	FilingDate         string
	PrimaryDocument    string
	AcceptanceDateTime string
	SubmissionsURL     string
}

func (f Filing) Identity() string {
	return FilingIdentity(f.CIK, f.AccessionNumber)
}

func FilingIdentity(cik, accession string) string {
	return NormalizeCIK(cik) + ":" + strings.TrimSpace(accession)
}

func NormalizeCIK(cik string) string {
	digitsOnly := make([]rune, 0, len(cik))
	for _, r := range strings.TrimSpace(cik) {
		if r >= '0' && r <= '9' {
			digitsOnly = append(digitsOnly, r)
		}
	}
	if len(digitsOnly) == 0 {
		return ""
	}
	d := string(digitsOnly)
	if len(d) >= 10 {
		return d
	}
	return strings.Repeat("0", 10-len(d)) + d
}

func SubmissionsURL(cik string) string {
	return fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", NormalizeCIK(cik))
}
