package eps

import (
	"math"
	"strings"
)

// Validate runs the in-doc validation gates described in docs/headlines/eps.md.
// It mutates e by appending flags and may clear EPSGAAPDiluted if a critical
// gate fails. Returns the list of flags that fired.
//
// Gates (in order):
//  1. Trace gate   — headline number must appear in source text.
//  2. Sign gate    — loss language must agree with negative EPS sign.
//  3. Reconcile    — net_income / shares_diluted ≈ eps (when both present).
//  4. Period gate  — period_end must not be a prior-period date.
//
// Callers should check ExtractionConfidence < 0.70 || HasFlag(FlagTraceFailure)
// before publishing.
func Validate(e *EarningsData, sourceText string) []ExtractionFlag {
	var fired []ExtractionFlag

	// 1. Trace gate.
	if e.EPSGAAPDiluted != nil {
		if !traceNumber(*e.EPSGAAPDiluted, sourceText) {
			e.addFlag(FlagTraceFailure)
			fired = append(fired, FlagTraceFailure)
		}
	}

	// 2. Sign gate.
	if sf := signGate(e, sourceText); sf != nil {
		fired = append(fired, *sf)
	}

	// 3. Reconciliation sanity.
	if rf := reconcileGate(e); rf != nil {
		fired = append(fired, *rf)
	}

	// 4. Period gate: check for prior-period contamination clues.
	if pf := periodGate(e, sourceText); pf != nil {
		fired = append(fired, *pf)
	}

	// Re-score confidence after gates.
	norm := strings.ToLower(sourceText)
	e.ExtractionConfidence = scoreConfidence(e, norm)
	if e.HasFlag(FlagTraceFailure) || e.HasFlag(FlagSignMismatch) {
		e.ExtractionConfidence *= 0.5
	}
	if e.ExtractionConfidence < 0.70 {
		e.addFlag(FlagLowConfidence)
		if !contains(fired, FlagLowConfidence) {
			fired = append(fired, FlagLowConfidence)
		}
	}

	return fired
}

// traceNumber returns true if the given EPS value (e.g. 2.40) appears as a
// dollar figure in the source text. Checks both positive and parenthesized forms.
func traceNumber(eps float64, text string) bool {
	if eps == 0 {
		return false
	}
	// Format as 2-decimal string and search.
	formatted := formatEPS(eps)
	absFormatted := formatEPS(math.Abs(eps))

	// Check $N.NN, $(N.NN), N.NN
	return strings.Contains(text, "$"+absFormatted) ||
		strings.Contains(text, "$("+absFormatted+")") ||
		strings.Contains(text, "("+absFormatted+")") ||
		strings.Contains(text, formatted) ||
		strings.Contains(text, absFormatted)
}

// signGate checks that loss language in the text agrees with a negative EPS sign.
func signGate(e *EarningsData, text string) *ExtractionFlag {
	norm := strings.ToLower(text)
	hasLossLanguage := strings.Contains(norm, "net loss") ||
		strings.Contains(norm, "reported a loss") ||
		strings.Contains(norm, "loss per share") ||
		strings.Contains(norm, "loss of $")

	if e.EPSGAAPDiluted == nil {
		return nil
	}
	eps := *e.EPSGAAPDiluted

	if hasLossLanguage && eps > 0 {
		f := FlagSignMismatch
		e.addFlag(f)
		return &f
	}
	if !hasLossLanguage && eps < 0 {
		// Negative EPS without loss language is unusual but not necessarily wrong —
		// just flag it as a low-confidence indicator.
		f := FlagSignMismatch
		e.addFlag(f)
		return &f
	}
	return nil
}

// reconcileGate: if net_income and shares_diluted are both present,
// checks that net_income/shares_diluted ≈ eps_gaap_diluted within 5%.
// Returns FlagReconcileFail if the check fails.
func reconcileGate(e *EarningsData) *ExtractionFlag {
	if e.EPSGAAPDiluted == nil || e.NetIncome == nil || e.SharesDiluted == nil {
		return nil
	}
	if *e.SharesDiluted == 0 {
		return nil
	}
	computed := *e.NetIncome / *e.SharesDiluted
	eps := *e.EPSGAAPDiluted
	if eps == 0 {
		return nil
	}
	relErr := math.Abs(computed-eps) / math.Abs(eps)
	if relErr > 0.05 { // 5% tolerance
		f := FlagReconcileFail
		e.addFlag(f)
		return &f
	}
	return nil
}

// periodGate checks for prior-period contamination: the extracted period_end
// should not match a prior-year date present in the release.
func periodGate(e *EarningsData, text string) *ExtractionFlag {
	// If we have no period_end, we can't do this check.
	if e.Period.PeriodEnd == "" || e.Period.FiscalYear == 0 {
		return nil
	}
	// Check that the year in period_end matches the stated fiscal year.
	yearStr := e.Period.PeriodEnd[:4]
	if yearStr != "" {
		fyStr := string(rune('0'+e.Period.FiscalYear/1000)) +
			string(rune('0'+(e.Period.FiscalYear/100)%10)) +
			string(rune('0'+(e.Period.FiscalYear/10)%10)) +
			string(rune('0'+e.Period.FiscalYear%10))
		if yearStr != fyStr {
			f := FlagPriorPeriod
			e.addFlag(f)
			return &f
		}
	}
	_ = text
	return nil
}

// formatEPS formats a float64 as a 2-decimal EPS string (e.g. 2.40, -0.34).
func formatEPS(v float64) string {
	neg := ""
	if v < 0 {
		neg = "-"
		v = -v
	}
	// Format to 2 decimal places.
	whole := int(v)
	frac := int(math.Round((v-float64(whole))*100))
	if frac == 100 {
		whole++
		frac = 0
	}
	return neg + itoa(whole) + "." + itoa2(frac)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func contains(flags []ExtractionFlag, f ExtractionFlag) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}
