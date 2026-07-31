package main

import "testing"

func TestExtractIssuerFromTitle(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Apple Inc. Reports Strong Q3 Results", "Apple Inc."},
		{"Microsoft Announces Record Revenue", "Microsoft"},
		{"Schwab Provides 2026 Guidance Update", "Schwab"},
		{"Tesla Issues Q2 Earnings Report", "Tesla"},
		{"Goldman Sachs Updates Full-Year Outlook", "Goldman Sachs"},
		// No keyword — fallback to first 40 chars.
		{"A very long company name that exceeds forty characters here", "A very long company name that exceeds fo"},
		// Short title without keyword.
		{"Short Title", "Short Title"},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractIssuerFromTitle(tc.title)
		if got != tc.want {
			t.Errorf("extractIssuerFromTitle(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestExtractIssuerFromTitle_TrimsSpace(t *testing.T) {
	// Leading/trailing spaces should be trimmed.
	got := extractIssuerFromTitle("  Company Name  Reports Results")
	if got != "Company Name" {
		t.Errorf("expected trimmed issuer, got %q", got)
	}
}

// S170-07: real ev.Headline captured live from var/guidance/articles.ndjson --
// prwatch's discovery scraper carries a leading "HH:MM ET" timestamp artifact
// that garbled every issuer this pipeline had ever extracted, worst-case on
// the 40-char fallback (see reHeadlineTimePrefix's own doc comment).
func TestExtractIssuerFromTitle_StripsTimePrefix(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"16:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tNucor", "Nucor"},
		{"06:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tDanaher", "Danaher"},
		{"06:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tColumbus McKinnon Reports Order Growth of 20%", "Columbus McKinnon"},
	}
	for _, tc := range cases {
		got := extractIssuerFromTitle(tc.title)
		if got != tc.want {
			t.Errorf("extractIssuerFromTitle(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

// S170-07: real litigation-alert headlines captured live -- every one of
// these previously produced a fabricated "guidance" article (a real EPS
// figure quoted somewhere inside the litigation copy, attributed to a
// garbled non-issuer). This is not a rare edge case: pulling
// var/guidance/articles.ndjson found this genre was the overwhelming
// majority of the live dataset.
func TestIsLitigationAlertHeadline(t *testing.T) {
	alerts := []string{
		"13:46 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPNR SHAREHOLDER INVESTIGATION: SueWallSt Notifies Investors of Potential Securities Claims Involving Pentair plc",
		"10:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tINVESTOR ALERT: Pomerantz Law Firm Investigates Claims On Behalf of Investors of Matrix Service Company - MTRX",
		"09:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tSueWallSt Reminds Shareholders of a Lead Plaintiff Deadline of August 3, 2026 in Badger Meter, Inc. Lawsuit - BMI",
		"10:09 ET\n\t\t\t\n\t\t\t\n\t\t\t\tLost Money on GeneDx Holdings Corp. (WGS)? Join Class Action Suit Seeking Recovery - Contact SueWallSt",
		"08:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tHAGENS BERMAN, SOBOL SHAPIRO LLP ALERT: GRAIL, Inc. (NASDAQ: GRAL) Investors Urged to Contact Hagens Berman; Securities Fraud Class Action Filed, August 4, 2026 Lead Plaintiff Deadline",
		"10:05 ET\n\t\t\t\n\t\t\t\n\t\t\t\tThe Gross Law Firm Reminds Shareholders of a Lead Plaintiff Deadline of July 27, 2026 in Calix, Inc. Lawsuit - CALX",
		"10:05 ET\n\t\t\t\n\t\t\t\n\t\t\t\tVRRM Shareholder Alert: Investors With Losses May Seek to Lead the Class Action in Verra Mobility Corporation Securities Lawsuit - Contact The Gross Law Firm",
		// "investigat*" (not literal "shareholder/investor investigation") --
		// the specific record this ticket started from.
		"13:24 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPentair plc (PNR) Securities Investigation Notice - Levi &amp; Korsinsky",
		"14:59 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPVH Corp. Investigation Initiated: SueWallSt Investigates the Officers and Directors of PVH Corp. (PVH)",
		// "securities law violations" -- a different firm/template (DJS Law
		// Group) than the SueWallSt/Pomerantz/Gross Law Firm phrasing above.
		"01:18 ET\n\t\t\t\n\t\t\t\n\t\t\t\tStellantis N.V. Sued for Securities Law Violations - Contact the DJS Law Group to Discuss Your Rights - STLA",
		// bare "lost money" (no "on"/"investing" suffix) -- Wolf Haldenstein's
		// own template.
		"21:06 ET\n\t\t\t\n\t\t\t\n\t\t\t\tShareholders who lost money in shares of GPGI, Inc. (NYSE: GPGI) Should Contact Wolf Haldenstein Immediately",
	}
	for _, h := range alerts {
		if !isLitigationAlertHeadline(h) {
			t.Errorf("isLitigationAlertHeadline(%q) = false, want true", h)
		}
	}

	real := []string{
		"06:08 ET\n\t\t\t\n\t\t\t\n\t\t\t\tOTIS REPORTS SECOND QUARTER 2026 RESULTS",
		"16:01 ET\n\t\t\t\n\t\t\t\n\t\t\t\tTI reports second quarter 2026 financial results",
		"06:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tAT&T Delivers Strong Second Quarter Results",
		"16:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tNucor",
		// Genuinely mentions "Shareholders"/"Investor" but is real corporate
		// news, not a litigation solicitation -- must not trip the filter.
		"08:31 ET\n\t\t\t\n\t\t\t\n\t\t\t\tSidus Space Issues Letter to Shareholders Highlighting Progress and Next Phase of Growth",
		"17:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tDaVita Inc. Schedules 2nd Quarter 2026 Investor Conference Call",
	}
	for _, h := range real {
		if isLitigationAlertHeadline(h) {
			t.Errorf("isLitigationAlertHeadline(%q) = true, want false (real release)", h)
		}
	}
}
