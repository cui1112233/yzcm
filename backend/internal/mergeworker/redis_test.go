package mergeworker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestEncodeRESPCommand(t *testing.T) {
	got := string(encodeRESPCommand("SET", "job:key", "value"))
	want := "*3\r\n$3\r\nSET\r\n$7\r\njob:key\r\n$5\r\nvalue\r\n"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestReadRESPArray(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBufferString("*2\r\n$5\r\nqueue\r\n$8\r\ntask-123\r\n"))
	value, err := readRESP(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, []any{"queue", "task-123"}) {
		t.Fatalf("value=%#v", value)
	}
}

func TestRedisClientMGETPreservesValuePositions(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		want        []any
	}{
		{"all missing", "*3\r\n$-1\r\n$-1\r\n$-1\r\n", []any{nil, nil, nil}},
		{"some missing", "*3\r\n$3\r\none\r\n$-1\r\n$5\r\nthree\r\n", []any{"one", nil, "three"}},
		{"all present", "*3\r\n$3\r\none\r\n$3\r\ntwo\r\n$5\r\nthree\r\n", []any{"one", "two", "three"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := redisClientWithReply(t, tc.reply, false)
			got, err := client.Do(context.Background(), "MGET", "one", "two", "three")
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MGET = %#v, %v, want %#v", got, err, tc.want)
			}
		})
	}
}

func TestRedisClientKeepsCommandProtocolAndAuthenticationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, reply, want string
		auth              bool
	}{
		{"command", "-ERR synthetic command failure\r\n", "redis error: ERR synthetic command failure", false},
		{"protocol", "!unexpected\r\n", "unsupported redis response", false},
		{"truncated array", "*2\r\n$-1\r\n", "redis response failed", false},
		{"array element command error", "*2\r\n$-1\r\n-ERR synthetic element failure\r\n", "redis error: ERR synthetic element failure", false},
		{"authentication", "-WRONGPASS synthetic authentication failure\r\n", "redis authentication failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := redisClientWithReply(t, tc.reply, tc.auth)
			value, err := client.Do(context.Background(), "MGET", "one", "two")
			if value != nil || err == nil || IsRedisNil(err) || err.Error() != tc.want {
				t.Fatalf("MGET = %#v, %v, want %q", value, err, tc.want)
			}
		})
	}
}

func TestRedisClientKeepsConnectionErrors(t *testing.T) {
	client := redisClientWithReply(t, "+OK\r\n", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := client.Do(ctx, "MGET", "one")
	if value != nil || err == nil || IsRedisNil(err) || err.Error() != "redis connection failed" {
		t.Fatalf("MGET = %#v, %v, want connection error", value, err)
	}
}

func redisClientWithReply(t *testing.T, reply string, auth bool) *RedisStore {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().String()
	wantCommand := "MGET"
	if auth {
		address = ":test-password@" + address
		wantCommand = "AUTH"
	}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		value, err := readRESP(bufio.NewReader(conn))
		if args, ok := value.([]any); err != nil || !ok || len(args) == 0 || args[0] != wantCommand {
			return
		}
		_, _ = io.WriteString(conn, reply)
	}()
	client, err := NewRedisStore("redis://"+address, "test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRedisStoreCommandContract(t *testing.T) {
	fake := &fakeRedisDo{}
	store := &RedisStore{Prefix: "qiantie:test:merge", Do: fake.Do}
	job := Job{ID: "task-1", BatchID: "batch-1", Status: StateQueued, Sources: []Source{{ProductionJobID: "p1", VideoID: "v1", MediaURL: "https://media.example/1.mp4", Order: 0}}}
	created, err := store.Create(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != job.ID {
		t.Fatalf("created=%+v", created)
	}
	if err := store.Enqueue(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	fake.next = []any{"qiantie:test:merge:queue", "task-1"}
	id, err := store.Dequeue(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if id != "task-1" {
		t.Fatalf("id=%q", id)
	}
	if len(fake.commands) < 3 || fake.commands[0][0] != "SET" || fake.commands[1][0] != "RPUSH" || fake.commands[2][0] != "BLPOP" {
		t.Fatalf("commands=%v", fake.commands)
	}
	var persisted Job
	if err := json.Unmarshal([]byte(fake.commands[0][2]), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StateQueued || len(persisted.Sources) != 1 {
		t.Fatalf("persisted=%+v", persisted)
	}
}

type fakeRedisDo struct {
	commands [][]string
	next     any
}

func (f *fakeRedisDo) Do(_ context.Context, args ...string) (any, error) {
	f.commands = append(f.commands, append([]string(nil), args...))
	if f.next != nil {
		value := f.next
		f.next = nil
		return value, nil
	}
	return "OK", nil
}

// Exercise the transport deadline with a Redis reply arriving after the
// default ten-second I/O timeout but inside the requested blocking wait.
func TestRedisBlockingClaimWaitsForLateReply(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the real ten-second socket deadline")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
	defer cancel()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := readRESP(bufio.NewReader(conn)); err != nil {
			return
		}
		select {
		case <-time.After(11 * time.Second):
			_, _ = io.WriteString(conn, "$3\r\njob\r\n")
		case <-ctx.Done():
		}
	}()
	store, err := NewRedisStore("redis://"+ln.Addr().String(), "test")
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.Do(ctx, "BRPOPLPUSH", "pending", "processing", "30")
	if err != nil || value != "job" {
		t.Fatalf("blocking claim was cut short: value=%v err=%v", value, err)
	}
}
