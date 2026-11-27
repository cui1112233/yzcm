package novelfetchworkshop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"qiantie/backend/internal/mergeworker"
)

// Exercise the production RESP decoder, rather than a fake that returns
// (nil,nil), because real Redis nil is a typed error at this boundary.
func TestRedisQueueIsBookCancelledRESPAbsence(t *testing.T) {
	for _, tc := range []struct {
		name, reply          string
		cancelled, wantError bool
	}{
		{name: "missing_cancel_key", reply: "$-1\r\n"},
		{name: "existing_cancel_marker", reply: "$1\r\n1\r\n", cancelled: true},
		{name: "connection_closed", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			client, err := mergeworker.NewRedisStore("redis://"+listener.Addr().String(), "cancel-test")
			if err != nil {
				t.Fatal(err)
			}
			queue := NewRedisQueueWithDo("cancel-test", RedisDoFunc(client.Do))
			served := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					served <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				reader := bufio.NewReader(conn)
				want := []string{"*2\r\n", "$3\r\n", "GET\r\n", fmt.Sprintf("$%d\r\n", len(queue.cancelKey("alice", "123"))), queue.cancelKey("alice", "123") + "\r\n"}
				for _, line := range want {
					got, readErr := reader.ReadString('\n')
					if readErr != nil {
						served <- readErr
						return
					}
					if got != line {
						served <- fmt.Errorf("unexpected Redis command line %q", got)
						return
					}
				}
				if tc.reply != "" {
					_, err = io.WriteString(conn, tc.reply)
				}
				served <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cancelled, err := queue.IsBookCancelled(ctx, "alice", "123")
			if cancelled != tc.cancelled || (err != nil) != tc.wantError {
				t.Fatalf("cancelled=%v err=%v; want cancelled=%v error=%v", cancelled, err, tc.cancelled, tc.wantError)
			}
			if tc.wantError && mergeworker.IsRedisNil(err) {
				t.Fatalf("connection failure became Redis absence: %v", err)
			}
			if err := <-served; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRedisQueueIsBookCancelledDoesNotSwallowUntypedError(t *testing.T) {
	failure := errors.New("redis nil")
	queue := NewRedisQueueWithDo("cancel-test", func(context.Context, ...string) (any, error) { return nil, failure })
	cancelled, err := queue.IsBookCancelled(context.Background(), "alice", "123")
	if cancelled || !errors.Is(err, failure) {
		t.Fatalf("untyped error swallowed: cancelled=%v err=%v", cancelled, err)
	}
}
