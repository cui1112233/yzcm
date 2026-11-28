package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"qiantie/backend/internal/novelcreation"
	"qiantie/backend/internal/screenplay"
)

// Only the dedicated private capability can be injected here. Generic media or
// local DocumentObjectStore is deliberately not accepted.
type privateScreenplayAttachmentStore interface {
	NamespaceID() string
	CheckPrivacy(context.Context) error
	PutVerified(context.Context, string, string, []byte) error
	OpenVerified(context.Context, string, string, int, string) ([]byte, error)
}
type screenplayAttachmentObjects struct {
	store privateScreenplayAttachmentStore
}

func newScreenplayAttachmentObjects(c novelcreation.DocumentTOSConfig) (screenplay.AssistantTextAttachmentObjects, error) {
	s, e := novelcreation.NewPrivateTOSDocumentStore(c)
	if e != nil {
		return nil, e
	}
	return &screenplayAttachmentObjects{store: s}, nil
}
func (s *screenplayAttachmentObjects) NamespaceID() string { return s.store.NamespaceID() }
func (s *screenplayAttachmentObjects) CheckPrivacy(ctx context.Context) error {
	return s.store.CheckPrivacy(ctx)
}
func (s *screenplayAttachmentObjects) validate(ctx context.Context, a screenplay.AssistantTextAttachment) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if a.Namespace != s.NamespaceID() {
		return novelcreation.ErrAttachmentNamespace
	}
	if a.ObjectKey != "assistant-text/"+a.ID || a.RawBytes < 1 || a.RawBytes > screenplay.AssistantTextAttachmentRawBytes {
		return errors.New("附件原件记录无效")
	}
	if _, e := a.Snapshot(); e != nil {
		return e
	}
	return nil
}
func (s *screenplayAttachmentObjects) PutVerified(ctx context.Context, a screenplay.AssistantTextAttachment, raw []byte) error {
	if e := s.validate(ctx, a); e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if len(raw) != a.RawBytes || hex.EncodeToString(h[:]) != a.RawSHA256 {
		return errors.New("附件原件与登记hash不同")
	}
	return s.store.PutVerified(ctx, a.Namespace, a.ObjectKey, raw)
}
func (s *screenplayAttachmentObjects) OpenVerified(ctx context.Context, a screenplay.AssistantTextAttachment) ([]byte, error) {
	if e := s.validate(ctx, a); e != nil {
		return nil, e
	}
	return s.store.OpenVerified(ctx, a.Namespace, a.ObjectKey, a.RawBytes, a.RawSHA256)
}

var _ screenplay.AssistantTextAttachmentObjects = (*screenplayAttachmentObjects)(nil)
