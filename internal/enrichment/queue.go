package enrichment

import (
	"context"
	"sync"

	"github.com/photonest/photonest/internal/job"
)

type Queue interface {
	Enqueue(ctx context.Context, payload job.Payload) error
	Dequeue(ctx context.Context) (job.Payload, bool, error)
}

type MemoryQueue struct {
	mu    sync.Mutex
	items []job.Payload
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{}
}

func (q *MemoryQueue) Enqueue(_ context.Context, payload job.Payload) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = append(q.items, payload)
	return nil
}

func (q *MemoryQueue) Dequeue(_ context.Context) (job.Payload, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return job.Payload{}, false, nil
	}

	payload := q.items[0]
	q.items = append(q.items[:0], q.items[1:]...)
	return payload, true, nil
}
