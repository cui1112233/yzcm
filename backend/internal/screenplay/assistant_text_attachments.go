package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const AssistantTextAttachmentRawBytes = 64 << 10
const AssistantTextAttachmentParser = "utf8-bom-v1"

// Evidence binds immutable content, not the mutable upload/publication CAS.
type AssistantTextAttachmentEvidence struct {
	RawSHA256  string `json:"raw_sha256"`
	TextSHA256 string `json:"text_sha256"`
	Parser     string `json:"parser_version"`
	Namespace  string `json:"namespace_id"`
}
type AssistantTextAttachment struct {
	ID, Owner, Project, Session, Uploader, ActualUploader string
	Filename, Namespace, ObjectKey, RequestKeyHash        string
	RawSHA256, TextSHA256, Parser, Text                   string
	RawBytes                                              int
	ContentVersion, StateRevision                         int64
	Status                                                string
	Verified, Published                                   bool
}

func PrepareAssistantTextAttachment(s AssistantReferenceScope, id, name, namespace string, raw []byte) (AssistantTextAttachment, error) {
	a := AssistantTextAttachment{}
	if e := s.ValidatePrepare(); e != nil {
		return a, e
	}
	if !validAssistantAttachmentID(id) || name == "" || len(name) > 191 || !utf8.ValidString(name) || name != path.Base(name) || strings.ContainsAny(name, "\\\x00\r\n") || !validAttachmentHash(namespace) {
		return a, ErrInvalidArgument
	}
	ext := strings.ToLower(path.Ext(name))
	if ext != ".txt" && ext != ".md" {
		return a, ErrInvalidArgument
	}
	if len(raw) == 0 || len(raw) > AssistantTextAttachmentRawBytes || !utf8.Valid(raw) || invalidAssistantAttachmentControls(string(raw)) {
		return a, ErrInvalidArgument
	}
	text := strings.TrimPrefix(string(raw), "\ufeff")
	if strings.TrimSpace(text) == "" {
		return a, ErrInvalidArgument
	}
	a = AssistantTextAttachment{ID: id, Owner: s.owner, Project: s.project, Session: s.session, Uploader: s.effective, ActualUploader: s.actual, Filename: name, Namespace: namespace, RawSHA256: contentDigest(string(raw)), TextSHA256: contentDigest(text), Parser: AssistantTextAttachmentParser, Text: text, RawBytes: len(raw), ContentVersion: 1, StateRevision: 1, Status: "preparing"}
	_, e := a.Snapshot()
	return a, e
}
func validAttachmentHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func (a AssistantTextAttachment) Visible(s AssistantReferenceScope) bool {
	if s.ValidateRead() != nil || a.Owner != s.owner || a.Project != s.project || a.Session != s.session || a.Status != "ready" || !a.Verified {
		return false
	}
	return a.Published || (s.ValidatePrepare() == nil && a.Uploader == s.effective && a.ActualUploader == s.actual)
}
func (a AssistantTextAttachment) Snapshot() (AssistantReferenceSnapshot, error) {
	out := AssistantReferenceSnapshot{}
	if !validAssistantAttachmentID(a.ID) || a.Filename == "" || len(a.Filename) > 191 || !utf8.ValidString(a.Filename) || a.RawBytes < 1 || a.RawBytes > AssistantTextAttachmentRawBytes || !utf8.ValidString(a.Text) || strings.TrimSpace(a.Text) == "" || invalidAssistantAttachmentControls(a.Text) || a.ContentVersion < 1 || !validAttachmentHash(a.RawSHA256) || !validAttachmentHash(a.TextSHA256) || !validAttachmentHash(a.Namespace) || a.Parser != AssistantTextAttachmentParser || a.TextSHA256 != contentDigest(a.Text) {
		return out, ErrInvalidArgument
	}
	out = AssistantReferenceSnapshot{Kind: "attachment_text", ID: a.ID, Name: a.Filename, Revision: a.ContentVersion, Text: a.Text, Bytes: len(a.Text), Attachment: &AssistantTextAttachmentEvidence{a.RawSHA256, a.TextSHA256, a.Parser, a.Namespace}}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	out.Digest = contentDigest(string(raw))
	raw, e = json.Marshal(out)
	if e != nil {
		return out, e
	}
	if len(raw) > AssistantReferenceItemBytes {
		return out, fmt.Errorf("%w: 单项引用超过16KiB", ErrInvalidArgument)
	}
	return out, nil
}

// Implementations perform all object/network work outside any SQL transaction.
type AssistantTextAttachmentObjects interface {
	NamespaceID() string
	CheckPrivacy(context.Context) error
	PutVerified(context.Context, AssistantTextAttachment, []byte) error
	OpenVerified(context.Context, AssistantTextAttachment) ([]byte, error)
}

func invalidAssistantAttachmentControls(text string) bool {
	for _, r := range text {
		if r < 32 && r != '\r' && r != '\n' && r != '\t' || r == 127 {
			return true
		}
	}
	return false
}
func validAssistantAttachmentID(id string) bool {
	if len(id) == 0 || len(id) > 96 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
