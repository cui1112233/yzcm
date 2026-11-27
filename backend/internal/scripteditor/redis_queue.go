package scripteditor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"qiantie/backend/internal/mergeworker"
)

const scriptEditorQueueKey = "qiantie:script-editor:jobs"

// RedisJobQueue only carries wake-up hints. MySQL remains the source of truth,
// so workers can recover queued jobs by scanning MySQL when Redis is unavailable.
type RedisJobQueue struct {
	transport *mergeworker.RedisStore
}

func NewRedisJobQueue(transport *mergeworker.RedisStore) *RedisJobQueue {
	return &RedisJobQueue{transport: transport}
}

func (q *RedisJobQueue) Enqueue(ctx context.Context, id string) error {
	if q == nil || q.transport == nil || q.transport.Do == nil {
		return fmt.Errorf("redis unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("job id is required")
	}
	_, err := q.transport.Do(ctx, "RPUSH", scriptEditorQueueKey, id)
	return err
}

func (q *RedisJobQueue) Dequeue(ctx context.Context, _ time.Duration) (string, error) {
	if q == nil || q.transport == nil || q.transport.Do == nil {
		return "", fmt.Errorf("redis unavailable")
	}
	value, err := q.transport.Do(ctx, "LPOP", scriptEditorQueueKey)
	if err != nil {
		return "", err
	}
	id, ok := value.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return "", ErrQueueEmpty
	}
	return strings.TrimSpace(id), nil
}
