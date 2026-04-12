package persistence

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/photonest/photonest/internal/job"
	"github.com/photonest/photonest/internal/platform/config"
)

func TestRedisQueueEnqueueAndDequeue(t *testing.T) {
	redisServer := miniredis.RunT(t)
	queue := NewRedisQueue(config.QueueConfig{
		Address:   redisServer.Addr(),
		Namespace: "photonest-test",
		Password:  config.SecretValue{AllowEmpty: true},
	})
	defer queue.Close()

	ctx := context.Background()
	payload := job.Payload{
		TaskID:    "asset-1:metadata",
		AssetID:   "asset-1",
		Operation: "asset-enrichment",
		Stage:     "metadata",
	}

	if err := queue.Enqueue(ctx, payload); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if got, err := redisServer.List("photonest-test:enrichment:ready"); err != nil || len(got) != 1 {
		t.Fatalf("expected redis list to contain one payload, got %d", len(got))
	}

	dequeued, ok, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if !ok {
		t.Fatal("expected payload to be dequeued")
	}
	if dequeued.TaskID != payload.TaskID || dequeued.AssetID != payload.AssetID || dequeued.Stage != payload.Stage {
		t.Fatalf("unexpected payload: %+v", dequeued)
	}

	_, ok, err = queue.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue empty queue: %v", err)
	}
	if ok {
		t.Fatal("expected empty queue to return ok=false")
	}
}
