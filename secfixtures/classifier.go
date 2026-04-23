package secfixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DocumentClassification struct {
	Kind       string
	InlineXBRL bool
	FilingLike bool
}

func ReadAndClassifyDocument(path string) (DocumentClassification, int, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return DocumentClassification{}, 0, fmt.Errorf("read document %q: %w", path, err)
	}

	size := len(bytes)
	if size == 0 {
		return DocumentClassification{}, 0, fmt.Errorf("document %q is empty", path)
	}

	content := strings.ToLower(string(bytes))
	ext := strings.ToLower(filepath.Ext(path))

	classification := DocumentClassification{Kind: classifyKind(ext, content)}
	classification.InlineXBRL = containsAny(content,
		"xmlns:ix",
		"http://www.xbrl.org/2013/inlinexbrl",
		"ix:header",
		"xbrli",
		"sec/xbrl",
	)
	classification.FilingLike = containsAny(content,
		"united states securities and exchange commission",
		"accession number",
		"form 10-k",
		"form 10-q",
		"form 8-k",
		"<sec-document>",
	)

	return classification, size, nil
}

func classifyKind(ext, content string) string {
	switch ext {
	case ".htm", ".html":
		return "html"
	case ".xml", ".xsd":
		return "xml"
	case ".txt":
		return "text"
	}

	trimmed := strings.TrimSpace(content)
	if strings.Contains(trimmed, "<html") {
		return "html"
	}
	if strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<xbrl") {
		return "xml"
	}
	if strings.Contains(trimmed, "accession number") {
		return "text"
	}
	return "unknown"
}

func containsAny(content string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}
