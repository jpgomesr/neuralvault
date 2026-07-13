// Package types defines the shared domain types for reranking input and
// output. It has no internal imports so both the reranking interface package
// and concrete provider packages can import it without creating an import cycle.
package types

// Candidate is a chunk offered to the reranker for relevance scoring against a query.
type Candidate struct {
	ID   string // stable identifier used to correlate results with their source chunk
	Text string // raw text to be scored against the query
}

// Result is a Candidate's relevance score from the reranker, higher is more relevant.
type Result struct {
	CandidateID string
	Score       float32
}
