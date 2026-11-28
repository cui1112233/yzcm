package screenplay

import (
	"context"
	"errors"
	"time"
)

var ErrAssistantAttachmentObjectNotFound = errors.New("附件原件不存在")
var ErrAssistantAttachmentOriginalRequired = errors.New("请补交同一份原文件后显式重试")
var ErrAssistantAttachmentStorageUnavailable = errors.New("附件存储当前不可用或状态未确认")

type AssistantAttachmentPreparationRepository interface {
	BeginIntent(context.Context, AssistantReferenceScope, AssistantTextAttachment, AssistantAttachmentGuard) (AssistantTextAttachment, string, error)
	InspectOwnIntentByID(context.Context, AssistantReferenceScope, string, AssistantAttachmentGuard) (AssistantAttachmentPreparationEntry, error)
	ClaimRetry(context.Context, AssistantReferenceScope, string, string, int64, AssistantAttachmentGuard) (AssistantTextAttachment, string, error)
	MarkReady(context.Context, AssistantReferenceScope, string, string, string, AssistantAttachmentGuard) error
}
type AssistantTextAttachmentPreparation struct {
	Repository AssistantAttachmentPreparationRepository
	Objects    AssistantTextAttachmentObjects
}

func AssistantAttachmentRequestKeyHash(key string) (string, error) {
	if len(key) < 16 || len(key) > 128 {
		return "", ErrInvalidArgument
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", ErrInvalidArgument
		}
	}
	return contentDigest(key), nil
}
func (s AssistantTextAttachmentPreparation) Upload(ctx context.Context, scope AssistantReferenceScope, name, key string, raw []byte, verify func(context.Context) error, guard AssistantAttachmentGuard) (out AssistantTextAttachment, err error) {
	if scope.ValidatePrepare() != nil || verify == nil || guard == nil {
		return out, ErrAccessDenied
	}
	hash, e := AssistantAttachmentRequestKeyHash(key)
	if e != nil {
		return out, e
	}
	if s.Repository == nil || s.Objects == nil {
		return out, ErrAssistantAttachmentStorageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if e = verify(ctx); e != nil {
		return out, e
	}
	a, e := PrepareAssistantTextAttachment(scope, newScreenplayID("attachment"), name, s.Objects.NamespaceID(), raw)
	if e != nil {
		return out, e
	}
	a.ObjectKey = "assistant-text/" + a.ID
	a.RequestKeyHash = hash
	out, token, e := s.Repository.BeginIntent(ctx, scope, a, guard)
	if e != nil {
		return out, e
	}
	if token == "" {
		return out, nil
	}
	if e = s.Objects.PutVerified(ctx, out, raw); e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, ErrAssistantAttachmentStorageUnavailable
	}
	return s.finish(ctx, scope, out, token, verify, guard)
}
func (s AssistantTextAttachmentPreparation) Retry(ctx context.Context, scope AssistantReferenceScope, id, key string, expected int64, raw []byte, verify func(context.Context) error, guard AssistantAttachmentGuard) (out AssistantTextAttachment, err error) {
	if scope.ValidatePrepare() != nil || verify == nil || guard == nil {
		return out, ErrAccessDenied
	}
	hash, e := AssistantAttachmentRequestKeyHash(key)
	if e != nil {
		return out, e
	}
	if s.Repository == nil || s.Objects == nil {
		return out, ErrAssistantAttachmentStorageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if e = verify(ctx); e != nil {
		return out, e
	}
	entry, e := s.Repository.InspectOwnIntentByID(ctx, scope, id, guard)
	if e != nil {
		return out, e
	}
	out = entry.Attachment
	if out.RequestKeyHash != hash || out.Namespace != s.Objects.NamespaceID() || out.StateRevision != expected {
		return out, ErrRevisionConflict
	}
	if len(raw) > 0 && (len(raw) != out.RawBytes || contentDigest(string(raw)) != out.RawSHA256) {
		return out, ErrInvalidArgument
	}
	out, token, e := s.Repository.ClaimRetry(ctx, scope, id, out.Namespace, expected, guard)
	if e != nil {
		return out, e
	}
	_, e = s.Objects.OpenVerified(ctx, out)
	if errors.Is(e, ErrAssistantAttachmentObjectNotFound) {
		if len(raw) == 0 {
			return out, ErrAssistantAttachmentOriginalRequired
		}
		e = s.Objects.PutVerified(ctx, out, raw)
	}
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, ErrAssistantAttachmentStorageUnavailable
	}
	return s.finish(ctx, scope, out, token, verify, guard)
}
func (s AssistantTextAttachmentPreparation) finish(ctx context.Context, scope AssistantReferenceScope, a AssistantTextAttachment, token string, verify func(context.Context) error, guard AssistantAttachmentGuard) (AssistantTextAttachment, error) {
	if e := ctx.Err(); e != nil {
		return a, e
	}
	if e := verify(ctx); e != nil {
		return a, e
	}
	if e := ctx.Err(); e != nil {
		return a, e
	}
	if e := s.Repository.MarkReady(ctx, scope, a.ID, token, a.RawSHA256, guard); e != nil {
		return a, e
	}
	a.Status = "ready"
	a.Verified = true
	a.StateRevision++
	return a, nil
}
