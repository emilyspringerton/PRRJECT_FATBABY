package secwatch

import "testing"

func TestDocumentURL(t *testing.T) {
	cases := []struct {
		cik, accession, doc, want string
	}{
		{
			cik:       "0001321655",
			accession: "0001193125-22-144264",
			doc:       "d259921d8k.htm",
			want:      "https://www.sec.gov/Archives/edgar/data/1321655/000119312522144264/d259921d8k.htm",
		},
		{
			cik:       "320193",
			accession: "0000320193-23-000106",
			doc:       "aapl-20230930.htm",
			want:      "https://www.sec.gov/Archives/edgar/data/320193/000032019323000106/aapl-20230930.htm",
		},
		{
			cik:       "0001045810",
			accession: "0001045810-26-000001",
			doc:       "",
			want:      "",
		},
	}
	for _, tc := range cases {
		got := DocumentURL(tc.cik, tc.accession, tc.doc)
		if got != tc.want {
			t.Errorf("DocumentURL(%q, %q, %q)\n got  %q\n want %q",
				tc.cik, tc.accession, tc.doc, got, tc.want)
		}
	}
}
