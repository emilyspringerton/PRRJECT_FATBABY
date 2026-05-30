package eps

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OracleSummary aggregates oracle case verdicts for accuracy reporting.
type OracleSummary struct {
	Total       int     `json:"total"`
	Confirmed   int     `json:"confirmed"`
	Contradicts int     `json:"contradicts"`
	Pending     int     `json:"pending"`
	Unavailable int     `json:"unavailable"`
	// Precision = confirmed / (confirmed + contradicts); 0 when no resolved cases.
	Precision float64 `json:"precision"`
}

// BuildOracleSummary aggregates cases into a summary report.
func BuildOracleSummary(cases []OracleCase) OracleSummary {
	var s OracleSummary
	for _, c := range cases {
		s.Total++
		switch c.Verdict {
		case VerdictConfirmed:
			s.Confirmed++
		case VerdictContradicts:
			s.Contradicts++
		case VerdictPending:
			s.Pending++
		case VerdictUnavailable:
			s.Unavailable++
		}
	}
	resolved := s.Confirmed + s.Contradicts
	if resolved > 0 {
		s.Precision = float64(s.Confirmed) / float64(resolved)
	}
	return s
}

// LoadOracleCases reads all cases from <dir>/oracle.ndjson.
// Returns nil, nil when the file does not exist yet.
func LoadOracleCases(dir string) ([]OracleCase, error) {
	path := filepath.Join(dir, "oracle.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cases []OracleCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var c OracleCase
		if err := json.Unmarshal(line, &c); err != nil {
			continue
		}
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

// AppendOracleCase appends one case to <dir>/oracle.ndjson.
// Creates the directory and file if they do not exist.
func AppendOracleCase(dir string, c OracleCase) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "oracle.ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// WriteOracleCases rewrites <dir>/oracle.ndjson with the given cases.
// Used when updating verdicts on existing cases.
func WriteOracleCases(dir string, cases []OracleCase) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "oracle.ndjson")
	f, err := os.CreateTemp(dir, "oracle-*.ndjson.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()

	w := bufio.NewWriter(f)
	for _, c := range cases {
		line, err := json.Marshal(c)
		if err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
