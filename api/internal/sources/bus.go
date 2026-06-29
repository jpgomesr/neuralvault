package sources

import (
	"sync"

	"github.com/google/uuid"
)

// EventType classifies a progress event.
type EventType string

const (
	EventIndexing EventType = "indexing" // a file is being processed
	EventDone     EventType = "done"     // all files indexed successfully
	EventError    EventType = "error"    // indexing failed
)

// ProgressEvent is sent over the SSE stream for a source.
type ProgressEvent struct {
	Type   EventType `json:"type"`
	File   string    `json:"file,omitempty"`   // file name (EventIndexing)
	Chunks int       `json:"chunks,omitempty"` // chunks produced for this file (EventIndexing)
	Total  int       `json:"total,omitempty"`  // total chunks across all files (EventDone)
	Error  string    `json:"error,omitempty"`  // error message (EventError)
}

const subBufSize = 64

// ProgressBus routes progress events from background indexing goroutines to SSE handlers.
// It is safe for concurrent use.
type ProgressBus struct {
	mu   sync.Mutex
	subs map[uuid.UUID][]chan ProgressEvent
}

// NewProgressBus returns an empty ProgressBus.
func NewProgressBus() *ProgressBus {
	return &ProgressBus{subs: make(map[uuid.UUID][]chan ProgressEvent)}
}

// Subscribe returns a channel that receives events for sourceID and a cancel
// function that must be called when the subscriber is done (mirrors context.WithCancel).
func (b *ProgressBus) Subscribe(sourceID uuid.UUID) (<-chan ProgressEvent, func()) {
	b.mu.Lock()
	ch := make(chan ProgressEvent, subBufSize)
	b.subs[sourceID] = append(b.subs[sourceID], ch)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[sourceID]
		for i, s := range subs {
			if s == ch {
				b.subs[sourceID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
		if len(b.subs[sourceID]) == 0 {
			delete(b.subs, sourceID)
		}
	}

	return ch, cancel
}

// publish sends event to all current subscribers of sourceID.
// Slow subscribers are skipped (non-blocking send) to avoid blocking the indexer.
func (b *ProgressBus) publish(sourceID uuid.UUID, event ProgressEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[sourceID] {
		select {
		case ch <- event:
		default:
		}
	}
}
