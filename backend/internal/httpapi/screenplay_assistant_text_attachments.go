package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"qiantie/backend/internal/screenplay"
	"strconv"
	"sync"
	"time"
)

type assistantAttachmentEntry struct {
	ID             string                                 `json:"id"`
	Filename       string                                 `json:"filename"`
	Status         string                                 `json:"status"`
	StateRevision  int64                                  `json:"state_revision"`
	ContentVersion int64                                  `json:"content_version"`
	RawBytes       int                                    `json:"raw_bytes"`
	CanReference   bool                                   `json:"can_reference"`
	CanRetry       bool                                   `json:"can_retry"`
	RetryAfter     string                                 `json:"retry_after,omitempty"`
	ReasonCode     string                                 `json:"reason_code,omitempty"`
	Selector       *screenplay.AssistantReferenceSelector `json:"selector,omitempty"`
}

func attachmentEntryDTO(entry screenplay.AssistantAttachmentPreparationEntry, canRetry bool, now time.Time) assistantAttachmentEntry {
	a := entry.Attachment
	out := assistantAttachmentEntry{ID: a.ID, Filename: a.Filename, Status: a.Status, StateRevision: a.StateRevision, ContentVersion: a.ContentVersion, RawBytes: a.RawBytes}
	if a.Status == "ready" && a.Verified {
		if snapshot, e := a.Snapshot(); e == nil {
			sel := snapshot.Selector()
			out.Selector = &sel
			out.CanReference = true
		} else {
			out.ReasonCode = "invalid_evidence"
		}
	} else {
		out.ReasonCode = "pending_or_unknown"
		out.CanRetry = canRetry && !entry.WorkDeadline.After(now)
		if entry.WorkDeadline.After(now) {
			out.RetryAfter = entry.WorkDeadline.UTC().Format(time.RFC3339Nano)
		}
	}
	return out
}

type assistantAttachmentMultipart struct {
	Session, Key, Filename string
	Expected               int64
	Raw                    []byte
}

func readAssistantAttachmentMultipart(w http.ResponseWriter, r *http.Request, retry bool) (out assistantAttachmentMultipart, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, 96<<10)
	reader, e := r.MultipartReader()
	if e != nil {
		return out, screenplay.ErrInvalidArgument
	}
	seen := map[string]bool{}
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, screenplay.ErrInvalidArgument
		}
		name := part.FormName()
		if seen[name] || name == "" {
			part.Close()
			return out, screenplay.ErrInvalidArgument
		}
		seen[name] = true
		if name == "file" {
			if part.FileName() == "" {
				part.Close()
				return out, screenplay.ErrInvalidArgument
			}
			out.Filename = part.FileName()
			out.Raw, e = io.ReadAll(io.LimitReader(part, (64<<10)+1))
			if e != nil || len(out.Raw) == 0 || len(out.Raw) > 64<<10 {
				part.Close()
				return out, screenplay.ErrInvalidArgument
			}
		} else {
			if part.FileName() != "" {
				part.Close()
				return out, screenplay.ErrInvalidArgument
			}
			raw, readErr := io.ReadAll(io.LimitReader(part, 257))
			if readErr != nil || len(raw) > 256 {
				part.Close()
				return out, screenplay.ErrInvalidArgument
			}
			switch name {
			case "session_id":
				out.Session = string(raw)
			case "request_key":
				out.Key = string(raw)
			case "expected_state_revision":
				if !retry {
					part.Close()
					return out, screenplay.ErrInvalidArgument
				}
				out.Expected, e = strconv.ParseInt(string(raw), 10, 64)
				if e != nil || out.Expected < 1 {
					part.Close()
					return out, screenplay.ErrInvalidArgument
				}
			default:
				part.Close()
				return out, screenplay.ErrInvalidArgument
			}
		}
		part.Close()
	}
	if out.Session == "" || len(out.Session) > 96 || !seen["request_key"] || !retry && !seen["file"] || retry && !seen["expected_state_revision"] {
		return out, screenplay.ErrInvalidArgument
	}
	if _, e := screenplay.AssistantAttachmentRequestKeyHash(out.Key); e != nil {
		return out, e
	}
	return out, nil
}
func attachmentHTTPError(w http.ResponseWriter, err error, entry *assistantAttachmentEntry) {
	status, code, message := 500, "attachment_failed", "附件操作失败，请稍后重试"
	switch {
	case errors.Is(err, screenplay.ErrAccessDenied):
		status, code, message = 403, "access_denied", "没有此资料的操作权限"
	case errors.Is(err, screenplay.ErrNotFound):
		status, code, message = 404, "not_found", "资料不存在或不可读取"
	case errors.Is(err, screenplay.ErrRevisionConflict):
		status, code, message = 409, "stale", "资料状态已变化，请重新读取"
	case errors.Is(err, screenplay.ErrInvalidArgument), errors.Is(err, screenplay.ErrEmptyContent):
		status, code, message = 400, "invalid_input", "文件或请求不符合限制"
	case errors.Is(err, screenplay.ErrAssistantAttachmentOriginalRequired):
		status, code, message = 409, "original_required", "请补交同一份原文件后显式重试"
	case errors.Is(err, screenplay.ErrAssistantAttachmentStorageUnavailable):
		status, code, message = 503, "storage_unavailable", "附件存储不可用或状态未确认"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = 408, "cancelled_or_timeout", "操作已取消或超时，原准备记录仍保留"
	case errors.Is(err, screenplay.ErrInvalidState):
		status, code, message = 409, "inactive_scope", "作品或会话当前不可操作"
	}
	body := map[string]any{"code": code, "error": message}
	if entry != nil && entry.ID != "" {
		body["attachment"] = entry
	}
	writeJSON(w, status, body)
}

type assistantAttachmentHTTP struct {
	auth    BridgeAuth
	svc     screenplay.AuthoringService
	repo    *screenplay.MySQLAssistantTextAttachments
	objects screenplay.AssistantTextAttachmentObjects
	reason  string
	mu      sync.Mutex
	privacy string
}

func (s *assistantAttachmentHTTP) privacyState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.privacy
}
func (s *assistantAttachmentHTTP) setPrivacy(v string) { s.mu.Lock(); s.privacy = v; s.mu.Unlock() }
func RegisterScreenplayAssistantTextAttachments(mux *http.ServeMux, auth BridgeAuth, store screenplay.Store, objects screenplay.AssistantTextAttachmentObjects, reason string) {
	if store == nil {
		return
	}
	var repo screenplay.AuthoringRepository
	var attachments *screenplay.MySQLAssistantTextAttachments
	if factory, ok := store.(interface {
		AuthoringRepository() screenplay.AuthoringRepository
	}); ok {
		repo = factory.AuthoringRepository()
	}
	if repo == nil {
		repo = screenplay.NewMemoryAuthoringRepository()
	}
	if factory, ok := store.(interface {
		AssistantTextAttachments() *screenplay.MySQLAssistantTextAttachments
	}); ok {
		attachments = factory.AssistantTextAttachments()
	}
	h := &assistantAttachmentHTTP{auth: auth, svc: screenplay.AuthoringService{Store: store, Repository: repo}, repo: attachments, objects: objects, reason: reason, privacy: "unknown"}
	base := "/api/comic-screenplays/projects/{id}/assistant/attachments"
	for _, v := range []struct {
		method, path string
		fn           func(http.ResponseWriter, *http.Request)
	}{{"GET", base + "/availability", h.availability}, {"POST", base + "/storage/check", h.checkStorage}, {"GET", base, h.list}, {"POST", base, h.upload}, {"GET", base + "/{attachment}", h.detail}, {"GET", base + "/{attachment}/content", h.content}, {"POST", base + "/{attachment}/retry", h.retry}} {
		mux.Handle(v.method+" "+v.path, auth.Middleware(http.HandlerFunc(v.fn)))
	}
}
func (s *assistantAttachmentHTTP) access(r *http.Request, session string, strong bool) (assistantAttachmentAccess, error) {
	bridge, ok := BridgeIdentityFromContext(r.Context())
	if !ok {
		return assistantAttachmentAccess{}, screenplay.ErrAccessDenied
	}
	owner, e := s.svc.Resolve(r.Context(), bridge.Username, r.PathValue("id"), "read")
	if e != nil {
		return assistantAttachmentAccess{}, e
	}
	return assistantAttachmentRequestAccess(s.auth, r, s.svc, owner, bridge.Username, session, strong)
}
func (s *assistantAttachmentHTTP) availability(w http.ResponseWriter, r *http.Request) {
	a, e := s.access(r, r.URL.Query().Get("session_id"), false)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if s.repo != nil {
		if e = s.repo.CheckScope(r.Context(), a.Scope, false, a.ReadGuard); e != nil {
			attachmentHTTPError(w, e, nil)
			return
		}
	}
	configured := s.objects != nil
	reason := s.reason
	if s.repo == nil {
		reason = "repository_unavailable"
	}
	privacy := s.privacyState()
	available := configured && s.repo != nil && privacy == "private" && a.CanPrepare
	if reason == "" && privacy != "private" {
		reason = "privacy_unconfirmed"
	}
	writeJSON(w, 200, map[string]any{"configured": configured || reason == "incomplete_configuration" || reason == "invalid_configuration", "privacy_state": privacy, "upload_available": available, "prepare_allowed": a.CanPrepare, "reason_code": reason, "raw_byte_limit": screenplay.AssistantTextAttachmentRawBytes, "item_byte_limit": screenplay.AssistantReferenceItemBytes})
}
func (s *assistantAttachmentHTTP) checkStorage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string `json:"session_id"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
		attachmentHTTPError(w, screenplay.ErrInvalidArgument, nil)
		return
	}
	a, e := s.access(r, in.Session, true)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if s.repo == nil || s.objects == nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	if e = s.repo.CheckScope(r.Context(), a.Scope, true, a.CommitPrepare); e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if e = s.objects.CheckPrivacy(ctx); e != nil {
		s.setPrivacy("unavailable")
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	if e = s.repo.CheckScope(ctx, a.Scope, true, a.CommitPrepare); e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	s.setPrivacy("private")
	writeJSON(w, 200, map[string]any{"privacy_state": "private"})
}
func attachmentPage(r *http.Request) (offset, limit int, err error) {
	limit = 100
	for _, v := range []struct {
		key    string
		target *int
	}{{"offset", &offset}, {"limit", &limit}} {
		if raw := r.URL.Query().Get(v.key); raw != "" {
			*v.target, err = strconv.Atoi(raw)
			if err != nil {
				return
			}
		}
	}
	if offset < 0 || offset > 10000 || limit < 1 || limit > 100 {
		err = screenplay.ErrInvalidArgument
	}
	return
}
func (s *assistantAttachmentHTTP) list(w http.ResponseWriter, r *http.Request) {
	a, e := s.access(r, r.URL.Query().Get("session_id"), false)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	offset, limit, e := attachmentPage(r)
	if e != nil {
		attachmentHTTPError(w, screenplay.ErrInvalidArgument, nil)
		return
	}
	if s.repo == nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	rows, e := s.repo.ListPreparationEntries(r.Context(), a.Scope, offset, limit, a.ReadGuard)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := []assistantAttachmentEntry{}
	for _, v := range rows {
		out = append(out, attachmentEntryDTO(v, a.CanPrepare && v.Attachment.UploadedBy(a.Scope), time.Now()))
	}
	writeJSON(w, 200, map[string]any{"attachments": out, "has_more": more, "next_offset": offset + len(out)})
}
func (s *assistantAttachmentHTTP) upload(w http.ResponseWriter, r *http.Request) {
	s.prepare(w, r, false)
}
func (s *assistantAttachmentHTTP) retry(w http.ResponseWriter, r *http.Request) {
	s.prepare(w, r, true)
}
func (s *assistantAttachmentHTTP) prepare(w http.ResponseWriter, r *http.Request, retry bool) {
	in, e := readAssistantAttachmentMultipart(w, r, retry)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	a, e := s.access(r, in.Session, true)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if s.repo == nil || s.objects == nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	service := screenplay.AssistantTextAttachmentPreparation{Repository: s.repo, Objects: s.objects}
	var record screenplay.AssistantTextAttachment
	if retry {
		record, e = service.Retry(r.Context(), a.Scope, r.PathValue("attachment"), in.Key, in.Expected, in.Raw, a.VerifyPrepare, a.CommitPrepare)
	} else {
		record, e = service.Upload(r.Context(), a.Scope, in.Filename, in.Key, in.Raw, a.VerifyPrepare, a.CommitPrepare)
	}
	entry := attachmentEntryDTO(screenplay.AssistantAttachmentPreparationEntry{Attachment: record}, false, time.Now())
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	status := 200
	if record.Status != "ready" {
		status = 202
	} else {
		s.setPrivacy("private")
	}
	writeJSON(w, status, map[string]any{"attachment": entry})
}
func (s *assistantAttachmentHTTP) detail(w http.ResponseWriter, r *http.Request) {
	a, e := s.access(r, r.URL.Query().Get("session_id"), false)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if s.repo == nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	entry, e := s.repo.InspectOwnIntentByID(r.Context(), a.Scope, r.PathValue("attachment"), a.ReadGuard)
	if errors.Is(e, screenplay.ErrNotFound) {
		entry.Attachment, e = s.repo.GetVisible(r.Context(), a.Scope, r.PathValue("attachment"), a.ReadGuard)
	}
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	out := map[string]any{"attachment": attachmentEntryDTO(entry, a.CanPrepare && entry.Attachment.UploadedBy(a.Scope), time.Now())}
	if entry.Attachment.Status == "ready" && entry.Attachment.Verified {
		snapshot, e := entry.Attachment.Snapshot()
		if e != nil {
			attachmentHTTPError(w, e, nil)
			return
		}
		out["reference"] = snapshot
	}
	writeJSON(w, 200, out)
}
func (s *assistantAttachmentHTTP) content(w http.ResponseWriter, r *http.Request) {
	a, e := s.access(r, r.URL.Query().Get("session_id"), false)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if s.repo == nil || s.objects == nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	record, e := s.repo.GetVisible(ctx, a.Scope, r.PathValue("attachment"), a.ReadGuard)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	raw, e := s.objects.OpenVerified(ctx, record)
	if e != nil {
		attachmentHTTPError(w, screenplay.ErrAssistantAttachmentStorageUnavailable, nil)
		return
	}
	current, e := s.repo.GetVisible(ctx, a.Scope, record.ID, a.ReadGuard)
	if e != nil {
		attachmentHTTPError(w, e, nil)
		return
	}
	if current.ContentVersion != record.ContentVersion || current.RawSHA256 != record.RawSHA256 {
		attachmentHTTPError(w, screenplay.ErrRevisionConflict, nil)
		return
	}
	if ctx.Err() != nil {
		attachmentHTTPError(w, ctx.Err(), nil)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.Filename}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.Write(raw)
}
