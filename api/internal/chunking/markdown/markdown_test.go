package markdown

import (
	"context"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	s := New()
	ctx := context.Background()

	tests := []struct {
		name        string
		input       string
		wantCount   int
		wantHeading []string
		wantLevel   []int
	}{
		{
			name:      "empty input",
			input:     "",
			wantCount: 0,
		},
		{
			name:        "single heading with content",
			input:       "# Introduction\nThis is the intro.",
			wantCount:   1,
			wantHeading: []string{"Introduction"},
			wantLevel:   []int{1},
		},
		{
			name: "two headings",
			input: `# Section One
Content for one.

# Section Two
Content for two.`,
			wantCount:   2,
			wantHeading: []string{"Section One", "Section Two"},
			wantLevel:   []int{1, 1},
		},
		{
			name: "nested headings flush on same level",
			input: `## Alpha
Text alpha.
## Beta
Text beta.`,
			wantCount:   2,
			wantHeading: []string{"Alpha", "Beta"},
			wantLevel:   []int{2, 2},
		},
		{
			name: "mixed heading levels",
			input: `# H1
Content.
## H2
Sub-content.
### H3
Deep content.`,
			wantCount:   3,
			wantHeading: []string{"H1", "H2", "H3"},
			wantLevel:   []int{1, 2, 3},
		},
		{
			name:        "no headings treated as single span",
			input:       "Just some plain text without any headings.",
			wantCount:   1,
			wantHeading: []string{""},
			wantLevel:   []int{0},
		},
		{
			name:        "heading line included in content",
			input:       "# My Title\nBody text here.",
			wantCount:   1,
			wantHeading: []string{"My Title"},
			wantLevel:   []int{1},
		},
		{
			name:      "hash without space is not a heading",
			input:     "#notaheading\nsome text",
			wantCount: 1,
			wantLevel: []int{0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spans, err := s.Split(ctx, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(spans) != tc.wantCount {
				t.Errorf("got %d spans, want %d", len(spans), tc.wantCount)
			}
			for i, h := range tc.wantHeading {
				if i >= len(spans) {
					break
				}
				if spans[i].Heading != h {
					t.Errorf("span[%d].Heading = %q, want %q", i, spans[i].Heading, h)
				}
			}
			for i, l := range tc.wantLevel {
				if i >= len(spans) {
					break
				}
				if spans[i].Level != l {
					t.Errorf("span[%d].Level = %d, want %d", i, spans[i].Level, l)
				}
			}
		})
	}
}

func TestSplitLineNumbers(t *testing.T) {
	s := New()
	ctx := context.Background()

	input := "# First\nLine two.\n# Second\nLine four."
	spans, err := s.Split(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}

	if spans[0].StartLine != 1 {
		t.Errorf("span[0].StartLine = %d, want 1", spans[0].StartLine)
	}
	if spans[0].EndLine != 2 {
		t.Errorf("span[0].EndLine = %d, want 2", spans[0].EndLine)
	}
	if spans[1].StartLine != 3 {
		t.Errorf("span[1].StartLine = %d, want 3", spans[1].StartLine)
	}
	if spans[1].EndLine != 4 {
		t.Errorf("span[1].EndLine = %d, want 4", spans[1].EndLine)
	}
}

func TestHeadingLevel(t *testing.T) {
	tests := []struct {
		line  string
		level int
	}{
		{"# Title", 1},
		{"## Title", 2},
		{"### Title", 3},
		{"###### Title", 6},
		{"####### Title", 0}, // > 6
		{"#Title", 0},        // no space
		{"not a heading", 0},
		{"", 0},
		{"# ", 1}, // empty heading is still valid
	}

	for _, tc := range tests {
		got := headingLevel(tc.line)
		if got != tc.level {
			t.Errorf("headingLevel(%q) = %d, want %d", tc.line, got, tc.level)
		}
	}
}

func TestSplitLargeSectionFallsBackToParagraph(t *testing.T) {
	// Build a section that exceeds defaultMaxChars.
	longPara := "word word word word word word word word word word "
	// Repeat to get well above 1500 chars.
	content := "# Big Section\n"
	for i := 0; i < 40; i++ {
		content += longPara + "\n\n"
	}

	s := New()
	spans, err := s.Split(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) <= 1 {
		t.Errorf("expected multiple spans after paragraph fallback, got %d", len(spans))
	}
	for _, sp := range spans {
		if len(sp.Content) > defaultMaxChars*2 {
			t.Errorf("span content length %d exceeds expected maximum", len(sp.Content))
		}
	}
}

func TestSplitPreservesHeadingOnParagraphSubdivision(t *testing.T) {
	// Reproduces a real bug: a heading followed by a single blank line, then a
	// large fenced diagram with no internal blank lines. The old splitter
	// dropped the heading from every sub-chunk after the first, so the
	// embedded diagram content carried no topic/section context.
	diagramLine := "  │ some-component │ -> │ other-component │"
	var fence strings.Builder
	fence.WriteString("```\n")
	for i := 0; i < 40; i++ {
		fence.WriteString(diagramLine + "\n")
	}
	fence.WriteString("```")

	content := "## Component diagram\n\n" + fence.String()

	s := New()
	spans, err := s.Split(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) <= 1 {
		t.Fatalf("expected multiple spans after paragraph fallback, got %d", len(spans))
	}

	for i, sp := range spans {
		if sp.Heading != "Component diagram" {
			t.Errorf("span[%d].Heading = %q, want %q", i, sp.Heading, "Component diagram")
		}
		if i > 0 && !strings.Contains(sp.Content, "Component diagram") {
			t.Errorf("span[%d].Content missing heading text: %q", i, sp.Content)
		}
	}
	if got := strings.Count(spans[0].Content, "Component diagram"); got != 1 {
		t.Errorf("span[0].Content has heading %d times, want exactly 1", got)
	}
}

func TestSplitFenceAwareHeadingDetection(t *testing.T) {
	input := "# First\n" +
		"Intro text.\n\n" +
		"```bash\n" +
		"# comment, not a heading\n" +
		"echo hi\n" +
		"```\n\n" +
		"# Second\n" +
		"More text."

	s := New()
	spans, err := s.Split(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	wantHeadings := []string{"First", "Second"}
	if len(spans) != len(wantHeadings) {
		t.Fatalf("got %d spans, want %d: %+v", len(spans), len(wantHeadings), spans)
	}
	for i, h := range wantHeadings {
		if spans[i].Heading != h {
			t.Errorf("span[%d].Heading = %q, want %q", i, spans[i].Heading, h)
		}
	}
	if !strings.Contains(spans[0].Content, "# comment, not a heading") {
		t.Errorf("span[0].Content should retain the fenced comment line, got: %q", spans[0].Content)
	}
}

func TestSplitByParagraphKeepsFenceIntact(t *testing.T) {
	text := "before-blank\n" +
		"```\n" +
		"line one\n" +
		"\n" +
		"line two after-blank\n" +
		"```\n" +
		"after"

	paras := splitByParagraph(text)

	var found bool
	for _, p := range paras {
		hasBefore := strings.Contains(p, "line one")
		hasAfter := strings.Contains(p, "line two after-blank")
		if hasBefore && hasAfter {
			found = true
			if got := strings.Count(p, "```"); got != 2 {
				t.Errorf("paragraph containing fence should have exactly 2 fence markers, got %d: %q", got, p)
			}
		}
	}
	if !found {
		t.Errorf("expected a single paragraph containing both sides of the internal blank line, got: %#v", paras)
	}
}

func TestIsFenceDelimiter(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"```", true},
		{"```go", true},
		{"  ```", true},
		{"", false},
		{"not a fence", false},
		{"some `backticks` mid-line", false},
	}

	for _, tc := range tests {
		if got := isFenceDelimiter(tc.line); got != tc.want {
			t.Errorf("isFenceDelimiter(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
