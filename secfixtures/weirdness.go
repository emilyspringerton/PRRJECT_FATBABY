package secfixtures

// WeirdnessRegistry is a first-pass explicit registry for known corpus exceptions.
// Keep entries intentional and reviewable instead of sprinkling ad-hoc test skips.
type WeirdnessRegistry struct {
	ByRelDir    map[string]string
	ByAccession map[string]string
}

func DefaultWeirdnessRegistry() WeirdnessRegistry {
	return WeirdnessRegistry{
		ByRelDir:    map[string]string{},
		ByAccession: map[string]string{},
	}
}

func (r WeirdnessRegistry) ReasonForRelDir(relDir string) (string, bool) {
	reason, ok := r.ByRelDir[relDir]
	return reason, ok
}

func (r WeirdnessRegistry) ReasonForAccession(accession string) (string, bool) {
	reason, ok := r.ByAccession[accession]
	return reason, ok
}
