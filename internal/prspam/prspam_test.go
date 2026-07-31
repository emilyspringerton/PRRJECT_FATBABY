package prspam

import (
	"strings"
	"testing"
)

// Real litigation-alert headlines captured live from var/prwatch (S170-07)
// -- every one of these previously produced a fabricated financial-event
// article (a real dollar figure quoted somewhere inside the litigation
// copy, attributed to a garbled non-issuer) in at least one downstream
// watcher. This is not a rare edge case: pulling live output found this
// genre was the majority of guidance-watcher's dataset and a similarly
// large share of dividend-watcher's.
func TestIsLitigationAlertHeadline(t *testing.T) {
	alerts := []string{
		"13:46 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPNR SHAREHOLDER INVESTIGATION: SueWallSt Notifies Investors of Potential Securities Claims Involving Pentair plc",
		"10:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tINVESTOR ALERT: Pomerantz Law Firm Investigates Claims On Behalf of Investors of Matrix Service Company - MTRX",
		"09:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tSueWallSt Reminds Shareholders of a Lead Plaintiff Deadline of August 3, 2026 in Badger Meter, Inc. Lawsuit - BMI",
		"10:09 ET\n\t\t\t\n\t\t\t\n\t\t\t\tLost Money on GeneDx Holdings Corp. (WGS)? Join Class Action Suit Seeking Recovery - Contact SueWallSt",
		"08:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tHAGENS BERMAN, SOBOL SHAPIRO LLP ALERT: GRAIL, Inc. (NASDAQ: GRAL) Investors Urged to Contact Hagens Berman; Securities Fraud Class Action Filed, August 4, 2026 Lead Plaintiff Deadline",
		"10:05 ET\n\t\t\t\n\t\t\t\n\t\t\t\tThe Gross Law Firm Reminds Shareholders of a Lead Plaintiff Deadline of July 27, 2026 in Calix, Inc. Lawsuit - CALX",
		"10:05 ET\n\t\t\t\n\t\t\t\n\t\t\t\tVRRM Shareholder Alert: Investors With Losses May Seek to Lead the Class Action in Verra Mobility Corporation Securities Lawsuit - Contact The Gross Law Firm",
		"13:24 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPentair plc (PNR) Securities Investigation Notice - Levi &amp; Korsinsky",
		"14:59 ET\n\t\t\t\n\t\t\t\n\t\t\t\tPVH Corp. Investigation Initiated: SueWallSt Investigates the Officers and Directors of PVH Corp. (PVH)",
		"01:18 ET\n\t\t\t\n\t\t\t\n\t\t\t\tStellantis N.V. Sued for Securities Law Violations - Contact the DJS Law Group to Discuss Your Rights - STLA",
		"21:06 ET\n\t\t\t\n\t\t\t\n\t\t\t\tShareholders who lost money in shares of GPGI, Inc. (NYSE: GPGI) Should Contact Wolf Haldenstein Immediately",
		// dividend-watcher's own live contamination (S170-07 follow-up): a
		// litigation alert about a dividend-paying BDC, "dividend" appearing
		// somewhere in the boilerplate is enough to trip dividend.Classify's
		// own core regex without this filter running first.
		"05:50 ET\n\t\t\t\n\t\t\t\n\t\t\t\tFSK INVESTOR ALERT: FS KKR Capital Corp. Investors with Substantial Losses Have Opportunity to Lead Investor Class Action Lawsuit",
		"10:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tINVESTOR ALERT: Pomerantz Law Firm Reminds Investors with Losses on their Investment in FS KKR Capital Corp. of Class Action Lawsuit",
		"19:05 ET\n\t\t\t\n\t\t\t\n\t\t\t\tFSK INVESTOR NOTICE: Robbins Geller Rudman &amp; Dowd LLP Announces that FS KKR Capital Corp. Investors with Substantial Losses Have Opportunity to Lead",
		"10:13 ET\n\t\t\t\n\t\t\t\n\t\t\t\tFSK Shareholder Alert: Investors With Losses May Seek to Lead the Class Action in FS KKR CAPITAL CORP. Securities Lawsuit - Contact Levi & Korsinsky",
		"06:17 ET\n\t\t\t\n\t\t\t\n\t\t\t\tLawsuit Notice: $EMBC Legal News: Embecta Insulin Pen Issues and 57% Stock Drop Trigger Securities Fraud Class Action",
	}
	for _, h := range alerts {
		if !IsLitigationAlertHeadline(h) {
			t.Errorf("IsLitigationAlertHeadline(%q) = false, want true", h)
		}
	}

	real := []string{
		"06:08 ET\n\t\t\t\n\t\t\t\n\t\t\t\tOTIS REPORTS SECOND QUARTER 2026 RESULTS",
		"16:01 ET\n\t\t\t\n\t\t\t\n\t\t\t\tTI reports second quarter 2026 financial results",
		"06:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tAT&T Delivers Strong Second Quarter Results",
		"16:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tNucor",
		"08:31 ET\n\t\t\t\n\t\t\t\n\t\t\t\tSidus Space Issues Letter to Shareholders Highlighting Progress and Next Phase of Growth",
		"17:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tDaVita Inc. Schedules 2nd Quarter 2026 Investor Conference Call",
		// dividend-watcher's own real, correctly-not-spam records.
		"16:15 ET\n\t\t\t\n\t\t\t\n\t\t\t\tTarget Announces Voting Results from 2026 Annual Meeting of Shareholders",
	}
	for _, h := range real {
		if IsLitigationAlertHeadline(h) {
			t.Errorf("IsLitigationAlertHeadline(%q) = true, want false (real release)", h)
		}
	}
}

// S170-231: real body text captured live (var/prwatch-body) from the
// Target "Annual Meeting of Shareholders" press release that produced a
// fabricated dividend "raise" signal -- the real dividend language belongs
// to a DIFFERENT, related Target press release teased by PRNewswire's
// "Also from this source" widget further down the same page, not to the
// release actually being classified.
func TestStripRelatedArticles(t *testing.T) {
	body := "Target Announces Voting Results from 2026 Annual Meeting of Shareholders " +
		"the annual meeting was held virtually and all proposals passed. " +
		"Also from this source Target Corporation Increases Quarterly Dividend by 1.8 Percent " +
		"The board of directors of Target Corporation (NYSE:TGT) has declared a quarterly " +
		"dividend of $1.16 per common share, a 1.8% increase from the prior quarter."
	got := StripRelatedArticles(body)
	if strings.Contains(got, "Increases Quarterly Dividend") {
		t.Errorf("StripRelatedArticles did not remove the related-article widget: %q", got)
	}
	if !strings.Contains(got, "all proposals passed") {
		t.Errorf("StripRelatedArticles removed real article content it shouldn't have: %q", got)
	}

	noMarker := "A real press release with no related-articles widget at all."
	if got := StripRelatedArticles(noMarker); got != noMarker {
		t.Errorf("StripRelatedArticles(%q) = %q, want unchanged", noMarker, got)
	}
}

func TestStripTimePrefix(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"16:30 ET\n\t\t\t\n\t\t\t\n\t\t\t\tNucor", "Nucor"},
		{"06:00 ET\n\t\t\t\n\t\t\t\n\t\t\t\tDanaher", "Danaher"},
		{"No time prefix here", "No time prefix here"},
	}
	for _, tc := range cases {
		got := StripTimePrefix(tc.title)
		if got != tc.want {
			t.Errorf("StripTimePrefix(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
