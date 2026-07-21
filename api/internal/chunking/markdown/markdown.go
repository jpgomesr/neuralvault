// Package markdown provides a Splitter that splits Markdown text by ATX heading
// hierarchy, producing one span per section. Sections that exceed maxChars are
// subdivided by paragraph boundary.
package markdown

import (
	"context"
	"strings"

	"github.com/jpgomesr/neuralvault/api/internal/chunking"
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
	currentHeadingLine := ""
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
		// Section too large: subdivide by paragraph. Every sub-chunk after the
		// first needs the heading line re-prepended to its content, since the
		// first paragraph is the only one that naturally retains it — without
		// this, later sub-chunks get embedded with no topic/section context.
		paras := splitByParagraph(content)
		lineOffset := startLine
		for i, p := range paras {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			pLineCount := strings.Count(p, "\n") + 1
			spanContent := p
			if i > 0 && currentHeadingLine != "" {
				spanContent = currentHeadingLine + "\n\n" + p
			}
			spans = append(spans, chunking.Span{
				Content:   spanContent,
				Heading:   currentHeading,
				Level:     currentLevel,
				StartLine: lineOffset,
				EndLine:   lineOffset + pLineCount - 1,
			})
			lineOffset += pLineCount + 1 // +1 for the blank separator line
		}
	}

	inFence := false
	for i, line := range lines {
		if isFenceDelimiter(line) {
			inFence = !inFence
			currentLines = append(currentLines, line)
			continue
		}
		if !inFence {
			if level := headingLevel(line); level > 0 {
				flush(i) // i is the 0-based index of the line *before* this heading
				currentHeading = strings.TrimSpace(strings.TrimLeft(line, "#"))
				currentHeadingLine = line
				currentLevel = level
				currentLines = []string{line}
				startLine = i + 1 // convert to 1-based
				continue
			}
		}
		currentLines = append(currentLines, line)
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

// isFenceDelimiter reports whether line opens or closes a fenced code block
// (a line whose trimmed content starts with three or more backticks).
func isFenceDelimiter(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

// splitByParagraph splits text on blank lines, treating a blank line inside a
// fenced code block as ordinary content rather than a paragraph boundary, so a
// fence's opening/closing markers never end up split across paragraphs.
func splitByParagraph(text string) []string {
	lines := strings.Split(text, "\n")

	var result []string
	var current []string
	inFence := false

	flushCurrent := func() {
		p := strings.TrimSpace(strings.Join(current, "\n"))
		if p != "" {
			result = append(result, p)
		}
		current = nil
	}

	for _, line := range lines {
		if isFenceDelimiter(line) {
			inFence = !inFence
			current = append(current, line)
			continue
		}
		if !inFence && strings.TrimSpace(line) == "" {
			flushCurrent()
			continue
		}
		current = append(current, line)
	}
	flushCurrent()

	return result
}
