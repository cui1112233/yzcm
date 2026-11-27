package scripteditor

import (
	"context"
	"reflect"
	"testing"
	"time"

	"qiantie/backend/internal/mergeworker"
)

func TestRedisJobQueueUsesDedicatedList(t *testing.T) {
	var calls [][]string
	transport := &mergeworker.RedisStore{Do: func(_ context.Context, args ...string) (any, error) {
		calls = append(calls, append([]string(nil), args...))
		if args[0] == "LPOP" {
			return "job-1", nil
		}
		return int64(1), nil
	}}
	queue := NewRedisJobQueue(transport)
	if err := queue.Enqueue(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	id, err := queue.Dequeue(context.Background(), time.Second)
	if err != nil || id != "job-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	want := [][]string{{"RPUSH", "qiantie:script-editor:jobs", "job-1"}, {"LPOP", "qiantie:script-editor:jobs"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}
