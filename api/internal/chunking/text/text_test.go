package text

import (
	"context"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	s := New()
	ctx := context.Background()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "empty input",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "single paragraph",
			input:     "Hello world.",
			wantCount: 1,
		},
		{
			name:      "two paragraphs merged below target",
			input:     "Short para one.\n\nShort para two.",
			wantCount: 1,
		},
		{
			name:      "windows line endings normalised",
			input:     "Para one.\r\n\r\nPara two.",
			wantCount: 1,
		},
		{
			name:      "triple newlines treated as paragraph boundary",
			input:     "Para one.\n\n\nPara two.",
			wantCount: 1,
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
			for _, sp := range spans {
				if sp.Content == "" {
					t.Errorf("got empty span content")
				}
			}
		})
	}
}

func TestSplitRespectsMaxChars(t *testing.T) {
	s := &Splitter{targetChars: 50, maxChars: 100}

	// Build two paragraphs each of 80 chars — together they exceed maxChars.
	p1 := strings.Repeat("a", 80)
	p2 := strings.Repeat("b", 80)
	input := p1 + "\n\n" + p2

	spans, err := s.Split(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Errorf("got %d spans, want 2", len(spans))
	}
}

func TestSplitReachesTargetChars(t *testing.T) {
	s := &Splitter{targetChars: 30, maxChars: 200}

	// Each paragraph is 10 chars; 3 paragraphs = 34 chars (with separators) ≥ targetChars.
	input := "0123456789\n\n0123456789\n\n0123456789\n\n0123456789"

	spans, err := s.Split(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	// We should get at least 2 spans since we flush once target is reached.
	if len(spans) < 2 {
		t.Errorf("got %d spans, want >= 2", len(spans))
	}
}

func TestSplitParagraphs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"hello", []string{"hello"}},
		{"a\n\nb", []string{"a", "b"}},
		{"a\n\n\nb", []string{"a", "b"}},
		{"\n\na\n\n", []string{"a"}},
	}

	for _, tc := range tests {
		got := splitParagraphs(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("splitParagraphs(%q): got %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitParagraphs(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}
