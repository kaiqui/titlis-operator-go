package notification

import (
	"sync"
	"time"

	"github.com/titlis/operator/internal/model"
)

type NamespaceBuffer struct {
	mu             sync.Mutex
	buckets        map[string][]model.ResourceScorecard
	lastFlush      map[string]time.Time
	digestInterval time.Duration
	batchSize      int
}

func NewNamespaceBuffer(intervalMinutes, batchSize int) *NamespaceBuffer {
	return &NamespaceBuffer{
		buckets:        make(map[string][]model.ResourceScorecard),
		lastFlush:      make(map[string]time.Time),
		digestInterval: time.Duration(intervalMinutes) * time.Minute,
		batchSize:      batchSize,
	}
}

// Add appends sc to the namespace bucket. Returns a non-nil slice when the buffer
// should be flushed (batch size reached or digest interval elapsed), and resets the bucket.
func (b *NamespaceBuffer) Add(ns string, sc model.ResourceScorecard) []model.ResourceScorecard {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Initialize the flush timer on first add to a namespace.
	if b.lastFlush[ns].IsZero() {
		b.lastFlush[ns] = time.Now()
	}

	b.buckets[ns] = append(b.buckets[ns], sc)

	if time.Since(b.lastFlush[ns]) >= b.digestInterval || len(b.buckets[ns]) >= b.batchSize {
		out := b.buckets[ns]
		b.buckets[ns] = nil
		b.lastFlush[ns] = time.Now()
		return out
	}
	return nil
}
