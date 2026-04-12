package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/photonest/photonest/internal/job"
	"github.com/photonest/photonest/internal/platform/config"
)

type RedisQueue struct {
	client    *redis.Client
	queueName string
}

func NewRedisQueue(cfg config.QueueConfig) *RedisQueue {
	password, _ := cfg.Password.Resolve(context.Background(), config.ResolveOptions{})
	return &RedisQueue{
		client: redis.NewClient(&redis.Options{
			Addr:     cfg.Address,
			Password: password,
			DB:       cfg.DB,
		}),
		queueName: queueKey(cfg.Namespace),
	}
}

func (q *RedisQueue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}

func (q *RedisQueue) Enqueue(ctx context.Context, payload job.Payload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return q.client.LPush(ctx, q.queueName, encoded).Err()
}

func (q *RedisQueue) Dequeue(ctx context.Context) (job.Payload, bool, error) {
	result, err := q.client.BRPop(ctx, 2*time.Second, q.queueName).Result()
	if err != nil {
		if err == redis.Nil {
			return job.Payload{}, false, nil
		}
		if ctx.Err() != nil {
			return job.Payload{}, false, nil
		}
		return job.Payload{}, false, err
	}
	if len(result) != 2 {
		return job.Payload{}, false, fmt.Errorf("unexpected redis brpop response length %d", len(result))
	}

	var payload job.Payload
	if err := json.Unmarshal([]byte(result[1]), &payload); err != nil {
		return job.Payload{}, false, err
	}
	return payload, true, nil
}

func queueKey(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "photonest"
	}
	return namespace + ":enrichment:ready"
}
