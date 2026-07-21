// Package text provides a Splitter that splits plain text by paragraph
// boundaries (blank lines), merging adjacent paragraphs until a target size is
// reached while respecting a hard maximum size cap.
package text

import (
	"context"
	"strings"

	"github.com/jpgomesr/neuralvault/api/internal/chunking"
)

const (
	defaultTargetChars = 800
	defaultMaxChars    = 1500
)

// Splitter splits plain text by paragraph boundaries.
type Splitter struct {
	targetChars int
	maxChars    int
}

// New returns a Splitter with default settings.
func New() *Splitter {
	return &Splitter{targetChars: defaultTargetChars, maxChars: defaultMaxChars}
}

// Split implements chunking.Splitter.
// Line numbers are not tracked; StartLine and EndLine in returned spans are 0.
func (s *Splitter) Split(_ context.Context, text string) ([]chunking.Span, error) {
	paragraphs := splitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil, nil
	}

	var spans []chunking.Span
	var buf []string
	bufSize := 0

	flush := func() {
		if len(buf) == 0 {
			return
		}
		spans = append(spans, chunking.Span{
			Content: strings.Join(buf, "\n\n"),
		})
		buf = buf[:0]
		bufSize = 0
	}

	for _, para := range paragraphs {
		sep := 0
		if len(buf) > 0 {
			sep = 2 // len("\n\n")
		}
		// If adding this paragraph would breach the hard cap, flush first.
		if bufSize+sep+len(para) > s.maxChars && len(buf) > 0 {
			flush()
		}

		buf = append(buf, para)
		if len(buf) == 1 {
			bufSize = len(para)
		} else {
			bufSize += 2 + len(para)
		}

		if bufSize >= s.targetChars {
			flush()
		}
	}
	flush()

	return spans, nil
}

// splitParagraphs splits text on two or more consecutive newlines. Empty
// entries and whitespace-only paragraphs are discarded.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n\n")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
