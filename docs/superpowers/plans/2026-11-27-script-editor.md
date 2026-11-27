# 剧本编辑复刻 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在4001新增独立“剧本编辑”，完成作品导入、章节与多版本剧本编辑、AI任务、导出和幂等交接漫剧创作。

**Architecture:** 新建 `scripteditor` Go领域包和独立MySQL表，HTTP统一挂载在 `/api/script-editor`；React新增 `/script-editor` 页面。剧本编辑不复制漫剧媒体能力，只通过后端 `ComicHandoff` 接口把正式剧本写入现有 `screenplay` 服务。AI任务以MySQL为真相、Redis为调度提示，worker重启时扫描可恢复任务。

**Tech Stack:** Go、MySQL 8、Redis、React、Ant Design、Node test runner、Go embed。

## Global Constraints

- 直接在当前 `v88` 分支工作，不新建分支或工作树。
- 保留当前614项无关工作区改动；每次提交只暂存本任务明确列出的文件。
- 系统提示词只放后端，不在React中硬编码正文。
- 不调用收费模型，不发布公网，不推送远端。
- 前端构建后必须通过Go embed生成单一运行二进制，并在本地4001验收。
- 任务队列使用Redis；MySQL保存任务、结果、修订号和幂等记录。
- 所有写接口必须按当前账号隔离，并使用 `expectedRevision` 防止静默覆盖。

---

## File Map

- `backend/internal/storage/script_editor_schema.go`：六张表的迁移 `8800410`。
- `backend/internal/scripteditor/model.go`：领域类型、状态常量和错误。
- `backend/internal/scripteditor/store.go`：Store接口。
- `backend/internal/scripteditor/memory_store.go`：路由和服务测试用内存实现。
- `backend/internal/scripteditor/mysql_store.go`：MySQL作品、章节、剧本、任务、审稿、交接。
- `backend/internal/scripteditor/service.go`：作品、章节、剧本规则。
- `backend/internal/scripteditor/importer.go`：TXT/MD/DOCX解析和章节识别。
- `backend/internal/scripteditor/jobs.go`：生成任务、Redis提示、租约和恢复。
- `backend/internal/scripteditor/export.go`：TXT/ZIP/JSON导出。
- `backend/internal/scripteditor/handoff.go`：正式版本校验和漫剧项目交接。
- `backend/internal/httpapi/script_editor_routes.go`：REST路由。
- `backend/internal/httpapi/script_editor_register.go`：RouterOptions装配。
- `backend/internal/prompt/script_editor.go`：后端提示词槽位。
- `frontend/src/shared/api/scriptEditor.js`：API封装。
- `frontend/src/user/pages/ScriptEditorPage.jsx`：页面状态与路由外壳。
- `frontend/src/user/pages/script-editor/ScriptEditorLibrary.jsx`：作品库。
- `frontend/src/user/pages/script-editor/ScriptEditorSource.jsx`：章节编辑。
- `frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx`：剧本版本与对照。
- `frontend/src/user/pages/script-editor/ScriptEditorDelivery.jsx`：导出与漫剧交接。
- `frontend/src/user/pages/script-editor/scriptEditorState.js`：纯函数。
- `frontend/src/user/pages/script-editor.css`：样式。

---

### Task 1: MySQL迁移与领域模型

**Files:**
- Create: `backend/internal/storage/script_editor_schema.go`
- Create: `backend/internal/storage/script_editor_schema_test.go`
- Modify: `backend/internal/storage/local_executor_schema.go:115-137`
- Modify: `backend/internal/storage/migrations_test.go:65-90`
- Create: `backend/internal/scripteditor/model.go`
- Create: `backend/internal/scripteditor/model_test.go`

**Interfaces:**
- Produces: `storage.ScriptEditorMigrations() []Migration`
- Produces: `scripteditor.Work`, `Chapter`, `Script`, `Review`, `Job`, `Handoff`
- Produces errors: `ErrNotFound`, `ErrConflict`, `ErrForbidden`, `ErrValidation`

- [ ] **Step 1: Write the failing migration tests**

```go
func TestScriptEditorMigrationContainsRequiredTables(t *testing.T) {
    sql := strings.Join(ScriptEditorMigrations()[0].SQL, "\n")
    for _, name := range []string{
        "script_editor_works", "script_editor_chapters", "script_editor_scripts",
        "script_editor_reviews", "script_editor_jobs", "script_editor_handoffs",
    } {
        if !strings.Contains(sql, name) { t.Fatalf("missing %s", name) }
    }
}

func TestAppMigrationsIncludesScriptEditor(t *testing.T) {
    for _, m := range AppMigrations() {
        if m.Version == 8800410 { return }
    }
    t.Fatal("script editor migration missing")
}
```

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/storage -run ScriptEditor -count=1` from `backend`  
Expected: FAIL because `ScriptEditorMigrations` does not exist.

- [ ] **Step 3: Implement migration and models**

Use migration version `8800410` and callback checksum `v88-script-editor-v1`. Add owner-scoped indexes, `(work_id, episode, version)` uniqueness, `(owner_username, request_key)` handoff uniqueness, JSON columns, revision columns, and soft delete on works.

```go
func ScriptEditorMigrations() []Migration {
    return []Migration{{
        Version: 8800410,
        CallbackChecksum: "v88-script-editor-v1",
        SQL: scriptEditorStatements(),
    }}
}
```

Append `ScriptEditorMigrations()` in `AppMigrations()` after account-center migrations. Define enum-like string types with `Valid()` methods and rune-based title validation.

- [ ] **Step 4: Run GREEN and full storage tests**

Run: `go test ./internal/storage ./internal/scripteditor -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit only Task 1 files**

```powershell
git add -- backend/internal/storage/script_editor_schema.go backend/internal/storage/script_editor_schema_test.go backend/internal/storage/local_executor_schema.go backend/internal/storage/migrations_test.go backend/internal/scripteditor/model.go backend/internal/scripteditor/model_test.go
git commit -m "feat(script-editor): add domain schema"
```

---

### Task 2: 作品、章节和多版本剧本存储

**Files:**
- Create: `backend/internal/scripteditor/store.go`
- Create: `backend/internal/scripteditor/memory_store.go`
- Create: `backend/internal/scripteditor/mysql_store.go`
- Create: `backend/internal/scripteditor/store_test.go`
- Create: `backend/internal/scripteditor/mysql_store_test.go`

**Interfaces:**
- Produces: `Store` with `CreateWork`, `ListWorks`, `GetWork`, `UpdateWork`, `DeleteWork`
- Produces: chapter CRUD/reorder and script CRUD/copy/prefer methods
- All mutations consume owner plus expected revision

- [ ] **Step 1: Write failing store contract tests**

```go
func TestStoreKeepsOnePreferredScriptPerEpisode(t *testing.T) {
    s := NewMemoryStore()
    w, _ := s.CreateWork(ctx, "alice", CreateWorkInput{Title:"测试", Type:WorkNovel})
    a, _ := s.CreateScript(ctx, "alice", w.ID, CreateScriptInput{Episode:1, Content:"A"})
    b, _ := s.CreateScript(ctx, "alice", w.ID, CreateScriptInput{Episode:1, Content:"B"})
    _, _ = s.PreferScript(ctx, "alice", w.ID, a.ID, a.Revision)
    got, err := s.PreferScript(ctx, "alice", w.ID, b.ID, b.Revision)
    if err != nil || !got.Preferred { t.Fatalf("prefer: %#v %v", got, err) }
    rows, _ := s.ListScripts(ctx, "alice", w.ID)
    if preferredCount(rows, 1) != 1 { t.Fatalf("rows=%#v", rows) }
}
```

Also test owner isolation, soft delete, stale revision conflict, stable sort order, episode/version uniqueness, copy-to-next-version, and work statistics.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/scripteditor -run 'Store|MySQL' -count=1`  
Expected: FAIL because Store implementations are missing.

- [ ] **Step 3: Implement memory and MySQL stores**

Use transactions for reorder, preferred-version selection, copy and statistics. Use `SELECT ... FOR UPDATE` before cross-row changes. Map duplicate keys to `ErrConflict` and missing owner-scoped rows to `ErrNotFound` without revealing foreign ownership.

```go
type Store interface {
    CreateWork(context.Context, string, CreateWorkInput) (Work, error)
    GetWork(context.Context, string, string) (WorkDetail, error)
    ListWorks(context.Context, string, WorkFilter) ([]WorkSummary, error)
    UpdateChapter(context.Context, string, string, string, UpdateChapterInput, int64) (Chapter, error)
    CreateScript(context.Context, string, string, CreateScriptInput) (Script, error)
    PreferScript(context.Context, string, string, string, int64) (Script, error)
}
```

- [ ] **Step 4: Run GREEN and race-free tests**

Run: `go test ./internal/scripteditor -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```powershell
git add -- backend/internal/scripteditor/store.go backend/internal/scripteditor/memory_store.go backend/internal/scripteditor/mysql_store.go backend/internal/scripteditor/store_test.go backend/internal/scripteditor/mysql_store_test.go
git commit -m "feat(script-editor): persist works and versions"
```

---

### Task 3: 文件解析、导入预览和提交

**Files:**
- Create: `backend/internal/scripteditor/importer.go`
- Create: `backend/internal/scripteditor/importer_test.go`
- Create: `backend/internal/scripteditor/testdata/chapters.txt`
- Create: `backend/internal/scripteditor/testdata/episodes.md`
- Create: `backend/internal/scripteditor/testdata/sample.docx`

**Interfaces:**
- Produces: `PreviewImport(filename string, content []byte) (ImportPreview, error)`
- Produces: `Service.CommitImport(ctx, owner, ImportCommitInput) (WorkDetail, error)`

- [ ] **Step 1: Write failing parser tests**

```go
func TestPreviewImportRecognizesChineseAndEnglishHeadings(t *testing.T) {
    got, err := PreviewImport("book.txt", []byte("前言\n说明\n第一章 开始\n正文\nChapter 2 Return\nNext"))
    if err != nil { t.Fatal(err) }
    if len(got.Sections) != 3 { t.Fatalf("sections=%#v", got.Sections) }
    if got.Sections[0].Title != "前言" || got.Sections[2].Number != 2 { t.Fatalf("preview=%#v", got) }
}
```

Test UTF-8 BOM, GB18030, no headings as `全文`, empty content, `.doc` rejection, unsupported extension, DOCX paragraphs/tables, and the 20 MiB boundary.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/scripteditor -run 'Import|Heading|DOCX' -count=1`  
Expected: FAIL because parser is missing.

- [ ] **Step 3: Implement bounded parsing**

Use `archive/zip` + `encoding/xml` for DOCX; do not add Word COM or an external office dependency. Return preview only; do not create work until `CommitImport`. Accept only selected section IDs on commit.

- [ ] **Step 4: Run GREEN**

Run: `go test ./internal/scripteditor -run 'Import|Heading|DOCX' -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```powershell
git add -- backend/internal/scripteditor/importer.go backend/internal/scripteditor/importer_test.go backend/internal/scripteditor/testdata
git commit -m "feat(script-editor): preview and import documents"
```

---

### Task 4: HTTP API和应用装配

**Files:**
- Create: `backend/internal/httpapi/script_editor_routes.go`
- Create: `backend/internal/httpapi/script_editor_routes_test.go`
- Create: `backend/internal/httpapi/script_editor_register.go`
- Modify: `backend/internal/httpapi/router.go:58-130,690-710`
- Modify: `backend/internal/app/app.go:730-810`

**Interfaces:**
- Consumes: `*scripteditor.Service`
- Produces all `/api/script-editor` work/chapter/script/import/search routes
- Adds `RouterOptions.ScriptEditor *scripteditor.Service`

- [ ] **Step 1: Write failing route tests**

```go
func TestScriptEditorRoutesKeepAccountsIsolated(t *testing.T) {
    api := newScriptEditorTestRouter(t)
    created := signedJSONRequest(t, api, now, "alice", "POST", "/api/script-editor/works", map[string]any{"title":"甲","type":"novel"})
    id := decodeID(t, created)
    foreign := signedJSONRequest(t, api, now, "bob", "GET", "/api/script-editor/works/"+id, nil)
    if foreign.Code != http.StatusNotFound { t.Fatalf("status=%d body=%s", foreign.Code, foreign.Body.String()) }
}
```

Cover CRUD, expectedRevision 409, preview/commit separation, upload 413, `.doc` 422, search, reorder, prefer and soft delete.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/httpapi -run ScriptEditor -count=1`  
Expected: FAIL because routes are not registered.

- [ ] **Step 3: Implement routes and wire production store**

Use existing `BridgeAuth`. Multipart import reads at most `20 MiB + 1 byte`. Map domain errors consistently:

```go
switch {
case errors.Is(err, scripteditor.ErrConflict): writeJSONError(w, 409, "内容已被其他窗口修改，请重新载入")
case errors.Is(err, scripteditor.ErrValidation): writeJSONError(w, 422, err.Error())
case errors.Is(err, scripteditor.ErrNotFound): writeJSONError(w, 404, "作品不存在或已删除")
default: writeJSONError(w, 500, "剧本编辑服务暂时不可用")
}
```

In app startup create `scripteditor.NewService(scripteditor.NewMySQLStore(db), now)` and pass it through `RouterOptions`.

- [ ] **Step 4: Run GREEN**

Run: `go test ./internal/httpapi ./internal/app -run ScriptEditor -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit Task 4**

```powershell
git add -- backend/internal/httpapi/script_editor_routes.go backend/internal/httpapi/script_editor_routes_test.go backend/internal/httpapi/script_editor_register.go backend/internal/httpapi/router.go backend/internal/app/app.go
git commit -m "feat(script-editor): expose work APIs"
```

---

### Task 5: 导航、API客户端和作品库页面

**Files:**
- Create: `frontend/src/shared/api/scriptEditor.js`
- Create: `frontend/src/user/pages/ScriptEditorPage.jsx`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorLibrary.jsx`
- Create: `frontend/src/user/pages/script-editor/scriptEditorState.js`
- Create: `frontend/src/user/pages/script-editor/scriptEditorState.test.js`
- Create: `frontend/src/user/pages/script-editor-page.source.test.js`
- Create: `frontend/src/user/pages/script-editor.css`
- Modify: `frontend/src/shared/layouts/UserLayout.jsx:1-43`
- Modify: `frontend/src/user/App.jsx:15-110`

**Interfaces:**
- Produces route `/script-editor`
- Produces `listScriptEditorWorks`, `createScriptEditorWork`, `previewScriptEditorImport`, `commitScriptEditorImport`
- Produces pure `filterAndSortWorks(rows, filters)`

- [ ] **Step 1: Write failing frontend tests**

```js
test('剧本编辑筛选排序不修改接口原数组', () => {
  const rows=[{id:'2',title:'乙',type:'script',updatedAt:'2026-01-02'},{id:'1',title:'甲',type:'novel',updatedAt:'2026-01-01'}];
  const snapshot=structuredClone(rows);
  assert.deepEqual(filterAndSortWorks(rows,{type:'novel',sort:'name-asc'}).map(x=>x.id),['1']);
  assert.deepEqual(rows,snapshot);
});
```

Source test must assert exact `剧本编辑` label, `/script-editor` route, import copy and no system prompt body in React.

- [ ] **Step 2: Run RED**

Run: `node --test src/user/pages/script-editor/scriptEditorState.test.js src/user/pages/script-editor-page.source.test.js` from `frontend`  
Expected: FAIL because files and route are missing.

- [ ] **Step 3: Implement library UI**

Add nav item after `小说创作`. Build compact glass-theme-compatible toolbar, grid/list cards, empty state, create modal and import preview modal. Use real API results; errors must preserve currently displayed rows.

- [ ] **Step 4: Run GREEN and build**

Run: `node --test src/user/pages/script-editor/scriptEditorState.test.js src/user/pages/script-editor-page.source.test.js`  
Run: `npm run build`  
Expected: tests PASS and build exit 0.

- [ ] **Step 5: Commit Task 5**

```powershell
git add -- frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/ScriptEditorPage.jsx frontend/src/user/pages/script-editor frontend/src/user/pages/script-editor-page.source.test.js frontend/src/user/pages/script-editor.css frontend/src/shared/layouts/UserLayout.jsx frontend/src/user/App.jsx
git commit -m "feat(script-editor): add project library"
```

---

### Task 6: 原文章节和剧本版本编辑器

**Files:**
- Create: `frontend/src/user/pages/script-editor/ScriptEditorSource.jsx`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx`
- Create: `frontend/src/user/pages/script-editor/scriptEditorDrafts.js`
- Create: `frontend/src/user/pages/script-editor/scriptEditorDrafts.test.js`
- Modify: `frontend/src/user/pages/ScriptEditorPage.jsx`
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Modify: `frontend/src/user/pages/script-editor.css`

**Interfaces:**
- Produces `createAutosaveController({save, delay:800})`
- Produces chapter editor and script version comparison
- Consumes expected revisions from Task 4

- [ ] **Step 1: Write failing autosave and conflict tests**

```js
test('409冲突保留本地剧本并要求选择恢复方式', async () => {
  const ctl=createAutosaveController({delay:0,save:async()=>{const e=new Error('conflict');e.status=409;throw e;}});
  ctl.edit({content:'本地新稿',revision:2});
  await ctl.flush();
  assert.equal(ctl.state().draft.content,'本地新稿');
  assert.equal(ctl.state().status,'conflict');
});
```

Also test debounce coalescing, leaving waits for save, reorder payload, 2–4 version comparison, and one preferred version.

- [ ] **Step 2: Run RED**

Run: `node --test src/user/pages/script-editor/scriptEditorDrafts.test.js`  
Expected: FAIL because controller is missing.

- [ ] **Step 3: Implement editor UI**

Use three detail tabs: `原文 / 剧本 / 导出与交接`. Source tab has chapter list and editor. Script tab has episode/version list, source/script split view, compare drawer, copy/new/import/prefer actions, exact save-state copy, and conflict modal with `重新载入 / 复制为新版本`.

- [ ] **Step 4: Run GREEN and focused build**

Run: `node --test src/user/pages/script-editor/*.test.js`  
Run: `npm run build`  
Expected: PASS.

- [ ] **Step 5: Commit Task 6**

```powershell
git add -- frontend/src/user/pages/script-editor/ScriptEditorSource.jsx frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx frontend/src/user/pages/script-editor/scriptEditorDrafts.js frontend/src/user/pages/script-editor/scriptEditorDrafts.test.js frontend/src/user/pages/ScriptEditorPage.jsx frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/script-editor.css
git commit -m "feat(script-editor): edit chapters and versions"
```

---

### Task 7: 后端提示词与可恢复AI任务

**Files:**
- Create: `backend/internal/prompt/script_editor.go`
- Create: `backend/internal/prompt/script_editor_test.go`
- Create: `backend/internal/scripteditor/jobs.go`
- Create: `backend/internal/scripteditor/jobs_test.go`
- Create: `backend/internal/scripteditor/worker.go`
- Create: `backend/internal/scripteditor/worker_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `backend/internal/httpapi/script_editor_register.go`
- Modify: `backend/internal/app/app.go`

**Interfaces:**
- Produces prompt slots `script-editor.generate`, `.review`, `.polish`, `.viral-opening`
- Produces `JobQueue.Enqueue`, `Worker.Run`, `Service.CancelJob`, `Service.RetryJob`
- Consumes existing text provider resolver and Redis URL

- [ ] **Step 1: Write failing prompt and job tests**

```go
func TestRetryOnlyQueuesFailedEpisodes(t *testing.T) {
    svc, queue := newJobFixture(t)
    job := seedPartialJob(t, svc, []int{1,2}, []int{3,4})
    retried, err := svc.RetryJob(ctx, "alice", job.WorkID, job.ID)
    if err != nil { t.Fatal(err) }
    if diff := cmp.Diff([]int{3,4}, queue.Last().Episodes); diff != "" { t.Fatal(diff) }
    if retried.RequestKey == job.RequestKey { t.Fatal("retry needs a derived idempotency key") }
}
```

Test missing model/prompt rejects before enqueue, requestKey idempotency, cancellation stops new episodes, complete response saves one new version, partial success preservation, expired lease recovery, and prompt bodies only in Go.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/scripteditor ./internal/prompt -run 'Job|Worker|ScriptEditorPrompt' -count=1`  
Expected: FAIL because job service and prompts are missing.

- [ ] **Step 3: Implement durable worker**

Reuse the proven lease/fence pattern from `backend/internal/novelcreation/worker.go`; do not share its tables. Redis list key is `qiantie:script-editor:jobs`. Startup scans MySQL for queued or expired leased jobs so Redis loss cannot strand work. Every model call uses a fingerprint persisted before send; uncertain sends become failed-with-readback-required and are never blindly repeated.

- [ ] **Step 4: Run GREEN**

Run: `go test ./internal/scripteditor ./internal/prompt ./internal/httpapi -run 'ScriptEditor|Job|Worker' -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit Task 7**

```powershell
git add -- backend/internal/prompt/script_editor.go backend/internal/prompt/script_editor_test.go backend/internal/scripteditor/jobs.go backend/internal/scripteditor/jobs_test.go backend/internal/scripteditor/worker.go backend/internal/scripteditor/worker_test.go backend/internal/httpapi/script_editor_routes.go backend/internal/httpapi/script_editor_register.go backend/internal/app/app.go
git commit -m "feat(script-editor): add recoverable AI jobs"
```

---

### Task 8: AI任务前端和审稿应用

**Files:**
- Create: `frontend/src/user/pages/script-editor/ScriptEditorJobs.jsx`
- Create: `frontend/src/user/pages/script-editor/scriptEditorJobs.js`
- Create: `frontend/src/user/pages/script-editor/scriptEditorJobs.test.js`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx`
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Modify: `frontend/src/user/pages/script-editor.css`

**Interfaces:**
- Produces job create/list/cancel/retry UI
- Produces `jobProgressView(job)` pure formatter
- Review application creates a new script version

- [ ] **Step 1: Write failing state tests**

```js
test('部分成功显示已保存和失败集数',()=>{
  assert.deepEqual(jobProgressView({status:'partial',completedEpisodes:[1,2],failedEpisodes:[3]}),{
    tone:'warning', label:'已保存2集，1集失败', retryable:true
  });
});
```

- [ ] **Step 2: Run RED**

Run: `node --test src/user/pages/script-editor/scriptEditorJobs.test.js`  
Expected: FAIL.

- [ ] **Step 3: Implement dialogs and recovery display**

AI生剧本 dialog selects chapters, chapters-per-episode, versions-per-episode, model and extra requirement. Job panel polls only while nonterminal and refetches after page reload. Review suggestions require explicit selection and `应用为新版本`.

- [ ] **Step 4: Run GREEN and build**

Run: `node --test src/user/pages/script-editor/*.test.js`  
Run: `npm run build`  
Expected: PASS.

- [ ] **Step 5: Commit Task 8**

```powershell
git add -- frontend/src/user/pages/script-editor/ScriptEditorJobs.jsx frontend/src/user/pages/script-editor/scriptEditorJobs.js frontend/src/user/pages/script-editor/scriptEditorJobs.test.js frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/script-editor.css
git commit -m "feat(script-editor): show generation and reviews"
```

---

### Task 9: 导出与幂等交接漫剧创作

**Files:**
- Create: `backend/internal/scripteditor/export.go`
- Create: `backend/internal/scripteditor/export_test.go`
- Create: `backend/internal/scripteditor/handoff.go`
- Create: `backend/internal/scripteditor/handoff_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `backend/internal/httpapi/script_editor_register.go`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorDelivery.jsx`
- Create: `frontend/src/user/pages/script-editor/scriptEditorDelivery.test.js`
- Modify: `frontend/src/user/pages/ScriptEditorPage.jsx`
- Modify: `frontend/src/shared/api/scriptEditor.js`

**Interfaces:**
- Produces `Exporter.Export(ctx, owner, workID, ExportInput)`
- Produces `ComicHandoff.Create(ctx, owner, workID, HandoffInput) (screenplay.Project, error)`
- Consumes `screenplay.Store` and `screenplay.AuthoringRepository`

- [ ] **Step 1: Write failing export and handoff tests**

```go
func TestHandoffIsIdempotentAndPreservesEpisodes(t *testing.T) {
    svc, comics := newHandoffFixture(t)
    first, err := svc.Create(ctx,"alice","work-1",HandoffInput{RequestKey:"handoff-1",Episodes:[]int{1,2},Title:"新项目"})
    if err != nil { t.Fatal(err) }
    second, err := svc.Create(ctx,"alice","work-1",HandoffInput{RequestKey:"handoff-1",Episodes:[]int{1,2},Title:"新项目"})
    if err != nil || first.ID != second.ID { t.Fatalf("first=%#v second=%#v err=%v",first,second,err) }
    if comics.ProjectCount() != 1 { t.Fatalf("projects=%d", comics.ProjectCount()) }
}
```

Test missing preferred versions, single-version confirmation, order, merged TXT, ZIP paths, JSON metadata, duplicate title, owner isolation, and retry after persisted handoff.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/scripteditor -run 'Export|Handoff' -count=1`  
Expected: FAIL.

- [ ] **Step 3: Implement exports and service-level handoff**

The handoff must call screenplay Go services directly. It creates one project, one source document, ordered episode script documents, then stores `script_editor_handoffs` before returning. If the handoff record already exists, read back the original project.

- [ ] **Step 4: Implement and test delivery UI**

```js
test('缺少正式版时阻止交接并列出集数',()=>{
  assert.deepEqual(validateDelivery([{episode:1,preferred:true},{episode:2,preferred:false}]),{ok:false,missing:[2]});
});
```

Run: `node --test src/user/pages/script-editor/scriptEditorDelivery.test.js`  
Expected: PASS after implementation.

- [ ] **Step 5: Run backend and frontend GREEN**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'ScriptEditor|Export|Handoff' -count=1`  
Run: `node --test src/user/pages/script-editor/*.test.js`  
Expected: PASS.

- [ ] **Step 6: Commit Task 9**

```powershell
git add -- backend/internal/scripteditor/export.go backend/internal/scripteditor/export_test.go backend/internal/scripteditor/handoff.go backend/internal/scripteditor/handoff_test.go backend/internal/httpapi/script_editor_routes.go backend/internal/httpapi/script_editor_register.go frontend/src/user/pages/script-editor/ScriptEditorDelivery.jsx frontend/src/user/pages/script-editor/scriptEditorDelivery.test.js frontend/src/user/pages/ScriptEditorPage.jsx frontend/src/shared/api/scriptEditor.js
git commit -m "feat(script-editor): export and hand off scripts"
```

---

### Task 10: 全量回归、Go embed与4001验收

**Files:**
- Modify: `docs/acceptance/2026-11-27-script-editor.md`
- Generated only: `frontend/dist/`, `backend/web/dist/`, `backend/qiantie-local.exe`

**Interfaces:**
- Verifies all prior tasks; introduces no new product behavior

- [ ] **Step 1: Run focused backend tests**

Run from `backend`:

```powershell
go test ./internal/scripteditor ./internal/storage ./internal/httpapi ./internal/prompt -count=1
```

Expected: PASS with zero failures.

- [ ] **Step 2: Run frontend tests and build**

Run from `frontend`:

```powershell
node --test src/user/pages/script-editor/*.test.js src/user/pages/script-editor-page.source.test.js
npm run build
```

Expected: PASS and build exit 0.

- [ ] **Step 3: Rebuild embedded Go binary**

Use the repository's current local build path; do not copy dist over source. Run the frontend embed/build command used by `scripts/deploy-go-embedded.ps1` in local-only mode, then build `backend/qiantie-local.exe`.

- [ ] **Step 4: Restart only confirmed local4001 process**

Resolve the listener PID first, verify its executable is this checkout's `backend/qiantie-local.exe`, then stop/restart that exact process. Do not stop other Go, Node, MySQL, Redis or worker processes.

- [ ] **Step 5: Verify runtime identity and health**

```powershell
Invoke-RestMethod http://127.0.0.1:4001/health
Invoke-RestMethod http://127.0.0.1:4001/api/runtime-build-info
```

Expected: healthy response and runtime identity matching the newly built local binary.

- [ ] **Step 6: Browser acceptance without paid calls**

Using an authenticated 4001 browser:

1. Open `剧本编辑`.
2. Create a uniquely named test work.
3. Import local TXT with two chapters.
4. Edit and refresh to prove persistence.
5. Create two script versions for episode 1 and select one preferred.
6. Export TXT/ZIP/JSON and read the files.
7. Handoff episodes to a uniquely named comic project.
8. Open the comic project and verify title, count, order and text.
9. Repeat the same handoff request key and verify no second project.
10. Keep the test data isolated; do not modify existing works.

- [ ] **Step 7: Record exact evidence**

Write `docs/acceptance/2026-11-27-script-editor.md` with commit SHA, modified paths, test commands/results, runtime build info, browser actions, created test IDs, and any remaining unverified paid-model behavior.

- [ ] **Step 8: Commit acceptance evidence only**

```powershell
git add -- docs/acceptance/2026-11-27-script-editor.md
git commit -m "docs: record script editor acceptance"
```

---

## Plan Self-Review

- Every spec section maps to a task: schema/store (1–2), import (3), API (4), library (5), editors (6), AI/review (7–8), export/handoff (9), runtime acceptance (10).
- All new domain names use `scripteditor` in Go, `script_editor_*` in MySQL, `/api/script-editor` in HTTP, and `/script-editor` in React.
- Every mutation carries owner scope and expected revision.
- No task requires paid generation, a new branch, public deployment or destructive cleanup.
- No task copies build output back as source.
