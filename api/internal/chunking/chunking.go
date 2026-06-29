// Package chunking defines the Splitter interface and domain types for
// converting raw text into ordered, non-overlapping spans.
// No business logic should depend on a concrete splitter — only this interface.
package chunking

import "context"

// Span is a contiguous slice of source text produced by a Splitter.
type Span struct {
	Content   string
	Heading   string // populated by the markdown splitter only
	Level     int    // ATX heading level 1-6; 0 means no heading or not tracked
	StartLine int    // 1-based; 0 means not tracked by this splitter
	EndLine   int    // 1-based inclusive; 0 means not tracked by this splitter
}

// Splitter converts raw text into an ordered sequence of non-overlapping spans.
// Implementations must be safe for concurrent use.
type Splitter interface {
	Split(ctx context.Context, text string) ([]Span, error)
}
