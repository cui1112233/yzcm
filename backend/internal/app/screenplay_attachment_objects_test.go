package app

import (
	"context"
	"errors"
	"qiantie/backend/internal/config"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
)

type fakePrivateAttachmentStore struct {
	namespace   string
	puts, opens int
}

func (f *fakePrivateAttachmentStore) NamespaceID() string                { return f.namespace }
func (f *fakePrivateAttachmentStore) CheckPrivacy(context.Context) error { return nil }
func (f *fakePrivateAttachmentStore) PutVerified(ctx context.Context, namespace, key string, raw []byte) error {
	f.puts++
	return ctx.Err()
}
func (f *fakePrivateAttachmentStore) OpenVerified(ctx context.Context, namespace, key string, n int, hash string) ([]byte, error) {
	f.opens++
	return []byte("hello"), ctx.Err()
}
func TestScreenplayAttachmentObjectsBinding(t *testing.T) {
	s, _ := screenplay.NewAssistantReferenceScope("o", "p", "default", "u", "actual")
	f := &fakePrivateAttachmentStore{namespace: strings.Repeat("a", 64)}
	objects := &screenplayAttachmentObjects{store: f}
	a, e := screenplay.PrepareAssistantTextAttachment(s, "id", "x.txt", f.namespace, []byte("hello"))
	if e != nil {
		t.Fatal(e)
	}
	a.ObjectKey = "assistant-text/id"
	if e = objects.PutVerified(context.Background(), a, []byte("hello")); e != nil || f.puts != 1 {
		t.Fatal(e)
	}
	if e = objects.PutVerified(context.Background(), a, []byte("wrong")); e == nil || f.puts != 1 {
		t.Fatal("hash preflight", e)
	}
	a.Namespace = strings.Repeat("b", 64)
	if e = objects.PutVerified(context.Background(), a, []byte("hello")); e == nil || f.puts != 1 {
		t.Fatal("binding", e)
	}
	if _, e = objects.OpenVerified(context.Background(), a); e == nil || f.opens != 0 {
		t.Fatal("wrong namespace read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Namespace = f.namespace
	if e = objects.PutVerified(ctx, a, []byte("hello")); !errors.Is(e, context.Canceled) || f.puts != 1 {
		t.Fatal(e)
	}
}

func TestScreenplayAttachmentOptionalConfiguration(t *testing.T) {
	for _, v := range []struct {
		cfg    config.Config
		reason string
	}{{config.Config{}, "not_configured"}, {config.Config{AssistantAttachmentTOSBucket: "private"}, "incomplete_configuration"}, {config.Config{AssistantAttachmentTOSEndpoint: "http://unsafe.invalid", AssistantAttachmentTOSRegion: "r", AssistantAttachmentTOSBucket: "private", AssistantAttachmentTOSAccessKey: "fake", AssistantAttachmentTOSSecretKey: "fake"}, "invalid_configuration"}} {
		o, reason := configuredScreenplayAttachmentObjects(v.cfg)
		if o != nil || reason != v.reason {
			t.Fatal("optional config must disable only attachments", reason)
		}
	}
}
