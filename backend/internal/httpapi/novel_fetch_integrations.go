package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/safehttp"
)

// 参考实现暴露的三个集成接口：
//   POST /api/integrations/workbench/push
//   POST /api/integrations/changdu/bookid-fetch/run
//   GET  /api/integrations/changdu/bookid-fetch/status
// 这里复用工作台现有的建任务 + 入队逻辑，不单独维护第二套处理管线。

type novelFetchBookIDFetchState struct {
	mu            sync.Mutex
	lastRunAt     string
	lastStatus    string
	lastError     string
	lastFetched   int
	lastAccepted  int
	lastSkipped   int
	lastFailed    int
	lastSourceURL string
}

var novelFetchBookIDFetchStateStore = &novelFetchBookIDFetchState{}

type novelFetchIntegrationBook struct {
	BookID    string `json:"book_id"`
	BookIDAlt string `json:"bookId"`
	BookName  string `json:"book_name"`
	Name      string `json:"name"`
	Author    string `json:"author"`
	Category  string `json:"category"`
}

type novelFetchWorkbenchPushRequest struct {
	Source       string                      `json:"source"`
	SourceRunID  string                      `json:"source_run_id"`
	SourceRunAlt string                      `json:"sourceRunId"`
	TriggerMode  string                      `json:"trigger_mode"`
	PlatformID   string                      `json:"platform_id"`
	PlatformAlt  string                      `json:"platformId"`
	Books        []novelFetchIntegrationBook `json:"books"`
}

type novelFetchBookIDFetchRunRequest struct {
	URL         string `json:"url"`
	PlatformID  string `json:"platform_id"`
	PlatformAlt string `json:"platformId"`
	InputText   string `json:"input_text"`
	InputAlt    string `json:"inputText"`
	Force       bool   `json:"force"`
}

// integrationPlatformID 与参考实现一致：未指定时默认平台 2。
func integrationPlatformID(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "2"
}

type integrationResult struct {
	created, accepted, skipped, failed, pending, reconciled int
	unavailable                                             bool
	errors                                                  map[string]string
}

func (r integrationResult) status() string {
	if r.failed == 0 {
		return "ok"
	}
	if r.accepted > 0 || r.skipped > 0 || r.reconciled > 0 || r.pending > 0 {
		return "partial_failed"
	}
	return "failed"
}

func (r integrationResult) payload() map[string]any {
	payload := map[string]any{
		"ok": r.failed == 0, "status": r.status(), "created": r.created, "accepted": r.accepted,
		"failed": r.failed, "skipped_duplicate": r.skipped, "state_pending": r.pending,
		"reconciled": r.reconciled, "errors": r.errors, "results": []any{},
	}
	if r.failed > 0 {
		payload["error"] = "部分或全部书籍尚未成功处理"
	}
	return payload
}

func (r integrationResult) statusCode() int {
	if r.unavailable {
		return http.StatusServiceUnavailable
	}
	return http.StatusOK
}

func integrationInputReady(doc novelfetchworkshop.Document, platformID string) bool {
	return toString(doc.Meta["importSource"]) == "integration" &&
		toString(doc.Meta["platformId"]) == platformID &&
		novelfetchworkshop.DeriveTaskStatus(doc) == novelfetchworkshop.TaskStatusInputReady &&
		firstText(doc.Meta["currentProcessJobId"], doc.Meta["current_process_job_id"]) == ""
}

func ensureIntegrationDocument(ctx context.Context, store novelfetchworkshop.Store, owner, bookID, platformID string, maxTxt int, book novelFetchIntegrationBook) (bool, error) {
	if _, err := store.GetDocument(ctx, owner, bookID); err == nil {
		return false, nil
	} else if !errors.Is(err, novelfetchworkshop.ErrNotFound) {
		return false, err
	}
	bookName := strings.TrimSpace(firstText(book.BookName, book.Name))
	meta := map[string]any{
		"platformId": platformID, "bookName": bookName, "name": bookName,
		"importSource": "integration", "importedAt": time.Now().UTC().Format(time.RFC3339Nano), "status": "input_ready",
	}
	if author := strings.TrimSpace(book.Author); author != "" {
		meta["author"] = author
	}
	if category := strings.TrimSpace(book.Category); category != "" {
		meta["category"] = category
	}
	doc := novelfetchworkshop.Document{BookID: bookID, Meta: meta, MaxTxt: maxTxt}
	_, err := store.CompareAndSwapDocument(ctx, owner, doc)
	if errors.Is(err, novelfetchworkshop.ErrDocumentChanged) {
		// Another request created the same account's book. Reuse its document.
		return false, nil
	}
	return err == nil, err
}

func recordIntegrationQueued(ctx context.Context, store novelfetchworkshop.Store, owner, bookID, platformID, jobID string) (bool, error) {
	for range 3 {
		doc, err := store.GetDocument(ctx, owner, bookID)
		if err != nil || !integrationInputReady(doc, platformID) {
			return false, err
		}
		doc.Meta["status"], doc.Meta["queueState"], doc.Meta["currentProcessJobId"] = "queued", "queued", jobID
		appendNovelFetchDocumentLog(&doc, "queue_queued", "任务已进入小说获取队列", time.Now().UTC())
		if _, err = store.CompareAndSwapDocument(ctx, owner, doc); !errors.Is(err, novelfetchworkshop.ErrDocumentChanged) {
			return err == nil, err
		}
		// A worker or another write changed the version. Recheck eligibility
		// before attempting to add the queue receipt to the new document.
	}
	return false, novelfetchworkshop.ErrDocumentChanged
}

func admitIntegrationBook(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner, bookID, platformID string, result *integrationResult) error {
	doc, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return errors.New("读取书籍任务失败，可重试")
	}
	if !integrationInputReady(doc, platformID) {
		result.skipped++
		return nil
	}
	admission, canAdmit := queue.(novelfetchworkshop.ProcessBookAdmissionQueue)
	bookLocks, canCheckQueue := queue.(novelfetchworkshop.ProcessBookConflictQueue)
	if !canAdmit || !canCheckQueue {
		result.unavailable = true
		return errors.New("任务队列未配置或无法核对已有任务，可重试")
	}
	entered := false
	err = admission.WithBookAdmission(ctx, owner, bookID, func(lockedCtx context.Context) error {
		entered = true
		doc, err := store.GetDocument(lockedCtx, owner, bookID)
		if err != nil {
			return errors.New("读取书籍任务失败，可重试")
		}
		eligible := func(doc novelfetchworkshop.Document) bool { return integrationInputReady(doc, platformID) }
		if !eligible(doc) {
			result.skipped++
			return nil
		}
		scope := novelfetchworkshop.ProcessJob{Owner: owner, PlatformID: platformID, BookIDs: []string{bookID}}
		conflicts, err := bookLocks.ConflictingBookIDs(lockedCtx, scope)
		if err != nil {
			return errors.New("无法核对已有队列任务，可重试")
		}
		jobID := conflicts[bookID]
		accepted := false
		if jobID == "" {
			submitted, err := enqueueWorkshopRetriesWhere(lockedCtx, store, queue, owner, []string{bookID}, false, eligible)
			if err != nil || submitted.Failed != 0 {
				return errors.New(firstText(submitted.Errors[bookID], "任务入队失败，可重试"))
			}
			accepted = submitted.Queued > 0
			if accepted {
				result.accepted++
			}
			if len(submitted.Jobs) > 0 {
				jobID = toString(submitted.Jobs[0]["id"])
			}
		}
		if jobID != "" {
			reconciled, err := recordIntegrationQueued(lockedCtx, store, owner, bookID, platformID, jobID)
			if err != nil {
				result.pending++
				return errors.New("队列已接收，任务状态待补记，可重试")
			}
			if reconciled && !accepted {
				result.reconciled++
			}
		}
		if !accepted {
			result.skipped++
		}
		return nil
	})
	if err != nil && !entered {
		return errors.New("入队准备暂不可用，可重试")
	}
	return err
}

func processIntegrationBooks(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, books []novelFetchIntegrationBook, platformID string, maxTxt int) integrationResult {
	result := integrationResult{errors: map[string]string{}}
	seen := map[string]bool{}
	for _, book := range books {
		bookID := novelFetchSafeBookID(firstText(book.BookID, book.BookIDAlt))
		if bookID == "" || !novelFetchBookIDPattern.MatchString(bookID) {
			result.failed++
			continue
		}
		if seen[bookID] {
			result.skipped++
			continue
		}
		seen[bookID] = true
		created, err := ensureIntegrationDocument(ctx, store, owner, bookID, platformID, maxTxt, book)
		if err != nil {
			result.failed++
			result.errors[bookID] = "保存书籍任务失败，可重试"
			continue
		}
		if created {
			result.created++
		}
		if err := admitIntegrationBook(ctx, store, queue, owner, bookID, platformID, &result); err != nil {
			result.failed++
			result.errors[bookID] = err.Error()
		}
	}
	return result
}

func RegisterNovelFetchIntegrations(root *http.ServeMux, middleware BridgeAuth, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue) {
	if root == nil || store == nil {
		return
	}

	pushHandler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, ok := BridgeIdentityFromContext(req.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		var input novelFetchWorkbenchPushRequest
		if err := decodeNovelFetchJSON(w, req, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求内容无效"})
			return
		}
		settings := mustGetNovelFetchConfig(req, store, identity.Username)
		if len(input.Books) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "skipped", "accepted": 0, "created": 0, "skipped_duplicate": 0, "failed": 0, "results": []any{}, "auto_queue_enabled": novelFetchAutomationEnabled(settings)})
			return
		}
		platformID := integrationPlatformID(input.PlatformID, input.PlatformAlt, toString(mapOrEmpty(settings["fetch"])["default_platform_id"]))
		maxTxt := intValue(mapOrEmpty(settings["fetch"])["default_max_txt"])
		if maxTxt <= 0 {
			maxTxt = 4000
		}
		result := processIntegrationBooks(req.Context(), store, queue, identity.Username, input.Books, platformID, maxTxt)
		payload := result.payload()
		payload["job_id"] = ""
		payload["source"] = firstText(input.Source, "changdu-workbench")
		payload["poll_url"] = "/api/process/jobs/latest"
		payload["auto_queue_enabled"] = novelFetchAutomationEnabled(settings)
		writeJSON(w, result.statusCode(), payload)
	}))

	bookIDFetchHandler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, ok := BridgeIdentityFromContext(req.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		var input novelFetchBookIDFetchRunRequest
		if err := decodeNovelFetchJSON(w, req, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求内容无效"})
			return
		}
		settings := mustGetNovelFetchConfig(req, store, identity.Username)
		sourceURL := strings.TrimSpace(input.URL)
		if sourceURL == "" {
			sourceURL = strings.TrimSpace(toString(mapOrEmpty(settings["integrations"])["bookid_fetch_url"]))
		}
		text := strings.TrimSpace(firstText(input.InputText, input.InputAlt))
		if text == "" && sourceURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": "disabled", "error": "未配置书城 ID 接口地址", "accepted": 0, "skipped_duplicate": 0, "failed": 0, "results": []any{}})
			return
		}
		novelFetchBookIDFetchStateStore.mu.Lock()
		novelFetchBookIDFetchStateStore.lastRunAt = time.Now().UTC().Format(time.RFC3339Nano)
		novelFetchBookIDFetchStateStore.lastSourceURL = sourceURL
		novelFetchBookIDFetchStateStore.mu.Unlock()
		if text == "" {
			if err := safehttp.CheckURL(sourceURL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": "failed", "error": "书城 ID 接口地址不可用：" + err.Error()})
				return
			}
			client := safehttp.NewClientWithTimeout(30 * time.Second)
			response, err := client.Get(sourceURL)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": "failed", "error": "拉取书城 ID 失败：" + err.Error()})
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 5<<20))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "status": "failed", "error": "读取书城 ID 响应失败"})
				return
			}
			text = string(body)
		}
		rawIDs := novelFetchNumericIDPattern.FindAllString(text, -1)
		books := make([]novelFetchIntegrationBook, 0, len(rawIDs))
		seen := map[string]bool{}
		for _, id := range rawIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			books = append(books, novelFetchIntegrationBook{BookID: id})
		}
		platformID := integrationPlatformID(input.PlatformID, input.PlatformAlt, toString(mapOrEmpty(settings["fetch"])["default_platform_id"]))
		maxTxt := intValue(mapOrEmpty(settings["fetch"])["default_max_txt"])
		if maxTxt <= 0 {
			maxTxt = 4000
		}
		result := processIntegrationBooks(req.Context(), store, queue, identity.Username, books, platformID, maxTxt)
		result.skipped += len(rawIDs) - len(books)
		novelFetchBookIDFetchStateStore.mu.Lock()
		novelFetchBookIDFetchStateStore.lastStatus = result.status()
		novelFetchBookIDFetchStateStore.lastError = ""
		if result.failed > 0 {
			novelFetchBookIDFetchStateStore.lastError = "部分或全部书籍尚未成功处理，可重试"
		}
		novelFetchBookIDFetchStateStore.lastFetched = len(rawIDs)
		novelFetchBookIDFetchStateStore.lastAccepted = result.accepted
		novelFetchBookIDFetchStateStore.lastSkipped = result.skipped
		novelFetchBookIDFetchStateStore.lastFailed = result.failed
		novelFetchBookIDFetchStateStore.mu.Unlock()
		payload := result.payload()
		payload["fetched"], payload["unique"], payload["source"] = len(rawIDs), len(books), sourceURL
		writeJSON(w, result.statusCode(), payload)
	}))

	statusHandler := middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, ok := BridgeIdentityFromContext(req.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		settings := mustGetNovelFetchConfig(req, store, identity.Username)
		enabled := strings.TrimSpace(toString(mapOrEmpty(settings["integrations"])["bookid_fetch_url"])) != ""
		novelFetchBookIDFetchStateStore.mu.Lock()
		payload := map[string]any{
			"ok": true, "enabled": enabled, "running": false,
			"last_run_at":            novelFetchBookIDFetchStateStore.lastRunAt,
			"last_status":            novelFetchBookIDFetchStateStore.lastStatus,
			"last_error":             novelFetchBookIDFetchStateStore.lastError,
			"last_fetched":           novelFetchBookIDFetchStateStore.lastFetched,
			"last_accepted":          novelFetchBookIDFetchStateStore.lastAccepted,
			"last_skipped_duplicate": novelFetchBookIDFetchStateStore.lastSkipped,
			"last_failed":            novelFetchBookIDFetchStateStore.lastFailed,
			"source_url":             novelFetchBookIDFetchStateStore.lastSourceURL,
		}
		novelFetchBookIDFetchStateStore.mu.Unlock()
		writeJSON(w, http.StatusOK, payload)
	}))

	root.Handle("POST /api/integrations/workbench/push", pushHandler)
	root.Handle("POST /api/integrations/changdu/bookid-fetch/run", bookIDFetchHandler)
	root.Handle("GET /api/integrations/changdu/bookid-fetch/status", statusHandler)
}

func mustGetNovelFetchConfig(req *http.Request, store novelfetchworkshop.Store, owner string) map[string]any {
	settings, err := novelfetchworkshop.ReadWorkflowConfig(req.Context(), store, owner)
	if err != nil || settings == nil {
		return map[string]any{}
	}
	return settings
}
