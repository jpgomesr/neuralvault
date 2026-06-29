// Package markdown provides a Splitter that splits Markdown text by ATX heading
// hierarchy, producing one span per section. Sections that exceed maxChars are
// subdivided by paragraph boundary.
package markdown

import (
	"context"
	"strings"

	"github.com/jpgomesr/NeuralVault/internal/chunking"
)

const defaultMaxChars = 1500

// Splitter splits Markdown by ATX headings (# / ## / ###).
type Splitter struct {
	maxChars int
}

// New returns a Splitter with default settings.
func New() *Splitter {
	return &Splitter{maxChars: defaultMaxChars}
}

// Split implements chunking.Splitter.
func (s *Splitter) Split(_ context.Context, text string) ([]chunking.Span, error) {
	lines := strings.Split(text, "\n")

	var spans []chunking.Span
	var currentLines []string
	currentHeading := ""
	currentLevel := 0
	startLine := 1

	flush := func(endLine int) {
		content := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if content == "" {
			return
		}
		if len(content) <= s.maxChars {
			spans = append(spans, chunking.Span{
				Content:   content,
				Heading:   currentHeading,
				Level:     currentLevel,
				StartLine: startLine,
				EndLine:   endLine,
			})
			return
		}
		// Section too large: subdivide by paragraph.
		paras := splitByParagraph(content)
		lineOffset := startLine
		for _, p := range paras {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			pLineCount := strings.Count(p, "\n") + 1
			spans = append(spans, chunking.Span{
				Content:   p,
				Heading:   currentHeading,
				Level:     currentLevel,
				StartLine: lineOffset,
				EndLine:   lineOffset + pLineCount - 1,
			})
			lineOffset += pLineCount + 1 // +1 for the blank separator line
		}
	}

	for i, line := range lines {
		level := headingLevel(line)
		if level > 0 {
			flush(i) // i is the 0-based index of the line *before* this heading
			currentHeading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentLevel = level
			currentLines = []string{line}
			startLine = i + 1 // convert to 1-based
		} else {
			currentLines = append(currentLines, line)
		}
	}
	flush(len(lines))

	return spans, nil
}

// headingLevel returns the ATX heading level (1–6) of a line, or 0 if it is not
// a heading. Requires a space after the # characters per the CommonMark spec.
func headingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0
	}
	if level < len(line) && line[level] == ' ' {
		return level
	}
	return 0
}

func splitByParagraph(text string) []string {
	parts := strings.Split(text, "\n\n")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
