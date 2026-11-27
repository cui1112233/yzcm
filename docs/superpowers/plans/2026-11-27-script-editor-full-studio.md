# 剧本编辑完整短剧工作室 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `/script-editor` 建成洛水“短剧工作室”的完整同名能力复刻，只把产品名改为“剧本编辑”、把 momo 画布映射到现有 CM 画布。

**Architecture:** `scripteditor` 继续负责作品、章节、剧本、任务和统一绑定；已有 `agentworkspace`、漫剧、AI-CUT、配音和媒体服务继续负责实际生产能力。前端增加洛水式三栏工作室外壳，通过稳定 ID 和绑定接口进入现有子系统，所有跨模块同步先预览差异再确认写入，绝不复制第二套 CM 画布或媒体引擎。

**Tech Stack:** Go、MySQL、Redis、Go embed、React、Ant Design、现有 CM Canvas/Agent Workspace、Node 内置测试、Go testing。

## Global Constraints

- 只在 Git `v88` 分支工作；每个可验收任务单独测试、单独提交，不创建新分支。
- 保留共享工作区内其他人的未提交修改，只暂存本计划列出的文件。
- 用户可见名称固定为“剧本编辑”；“momo 画布”固定显示为“CM 画布”。
- 系统预设提示词只放后端 `prompt` 模块，前端不硬编码真实系统提示词。
- MySQL 保存作品和绑定真相；Redis 只负责队列、锁和临时信号；长期媒体继续使用 TOS。
- 不复制 CM 画布、漫剧、AI-CUT、配音或媒体生成能力；跨模块以稳定 ID 绑定。
- 所有覆盖、同步、采用和交接操作必须有 revision 或 request key；409 冲突不得静默覆盖。
- 可能扣费的生成只做到可操作、可恢复和可追踪，不在自动验收中实际提交付费调用。
- 每个阶段先通过本地测试，再构建 Go embed 二进制并更新本机 4001；不发布公网，不使用 GitHub Actions。
- 源码优先，不能用 `frontend/dist` 或其他编译产物反向覆盖 React 源码。

---

## File map

- `backend/internal/storage/script_editor_schema.go`：新增四张跨模块绑定表的独立迁移。
- `backend/internal/scripteditor/bindings.go`：绑定、同步预览、冲突和工作室统计领域类型。
- `backend/internal/scripteditor/store.go`、`memory_store.go`、`mysql_store.go`：绑定和作品完整操作的持久化接口。
- `backend/internal/scripteditor/studio.go`：复制作品、剧本批量导入、全文搜索和工作室聚合服务。
- `backend/internal/scripteditor/canvas_sync.go`：作品与 CM 画布的创建、同步预览、应用和回写。
- `backend/internal/httpapi/script_editor_routes.go`：REST 路由和鉴权边界。
- `frontend/src/shared/api/scriptEditor.js`：完整工作室 API 客户端。
- `frontend/src/user/pages/ScriptEditorPage.jsx`：作品库与作品详情入口。
- `frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx`：三栏总外壳和标签恢复。
- `ScriptEditorLibrary.jsx`、`ScriptEditorSource.jsx`、`ScriptEditorScripts.jsx`：作品、原文、剧本完整编辑。
- `ScriptEditorAgent.jsx`、`ScriptEditorAssets.jsx`、`ScriptEditorStoryboard.jsx`：Agent、资产和分镜工作区。
- `ScriptEditorCanvas.jsx`、`ScriptEditorProduction.jsx`、`ScriptEditorDelivery.jsx`、`ScriptEditorSettings.jsx`：画布、生产、后期、交付和设置。
- `frontend/src/user/pages/script-editor.css`：洛水三栏结构、响应式尺寸和状态样式。

### Task 1: 固定完整复刻合同和导航骨架

**Files:**
- Create: `frontend/src/user/pages/script-editor/studioContract.js`
- Create: `frontend/src/user/pages/script-editor/studioContract.test.js`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx`
- Modify: `frontend/src/user/pages/ScriptEditorPage.jsx`
- Modify: `frontend/src/user/pages/script-editor.css`

**Interfaces:**
- Produces: `SCRIPT_EDITOR_RAIL_ITEMS: Array<{key,label,icon}>`、`resolveStudioLocation(search, work): {workId, tab, itemId}`、`buildStudioSearch(location): string`。
- Consumes: 当前 `WorkDetail` JSON 和浏览器 `URLSearchParams`。

- [ ] **Step 1: Write the failing test**

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { SCRIPT_EDITOR_RAIL_ITEMS, resolveStudioLocation, buildStudioSearch } from './studioContract.js';

test('rail covers the complete Luo Shui studio with CM Canvas naming', () => {
  assert.deepEqual(SCRIPT_EDITOR_RAIL_ITEMS.map(item => item.key), ['source','scripts','agent','assets','storyboard','canvas','production','delivery','settings']);
  assert.equal(SCRIPT_EDITOR_RAIL_ITEMS.find(item => item.key === 'canvas').label, 'CM 画布');
});

test('location restores the work tab and selected item', () => {
  const value = resolveStudioLocation('?workId=w1&tab=scripts&itemId=s2', { id: 'w1', type: 'novel' });
  assert.deepEqual(value, { workId: 'w1', tab: 'scripts', itemId: 's2' });
  assert.equal(buildStudioSearch(value), '?workId=w1&tab=scripts&itemId=s2');
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test frontend/src/user/pages/script-editor/studioContract.test.js`
Expected: FAIL because `studioContract.js` does not exist.

- [ ] **Step 3: Implement the contract and workspace shell**

```js
export const SCRIPT_EDITOR_RAIL_ITEMS = [
  ['source','小说'], ['scripts','剧本'], ['agent','Agent'], ['assets','资产'],
  ['storyboard','分镜'], ['canvas','CM 画布'], ['production','生产'],
  ['delivery','交付'], ['settings','设置'],
].map(([key, label]) => ({ key, label }));

export function resolveStudioLocation(search, work) {
  const query = new URLSearchParams(search);
  return {
    workId: query.get('workId') || work?.id || '',
    tab: query.get('tab') || (work?.type === 'script' ? 'scripts' : 'source'),
    itemId: query.get('itemId') || '',
  };
}

export function buildStudioSearch(value) {
  const query = new URLSearchParams();
  if (value.workId) query.set('workId', value.workId);
  if (value.tab) query.set('tab', value.tab);
  if (value.itemId) query.set('itemId', value.itemId);
  return `?${query.toString()}`;
}
```

`ScriptEditorWorkspace` renders the 44px rail, 268px aside slot, main slot, top statistics and a persistent Agent dock. `ScriptEditorPage` replaces the old three-tab detail page with this component and updates `history.replaceState` on every location change.

- [ ] **Step 4: Run tests and build**

Run: `node --test frontend/src/user/pages/script-editor/studioContract.test.js frontend/src/user/pages/script-editor-page.source.test.js`
Expected: PASS.

Run: `npm --prefix frontend run build`
Expected: build completes without JSX or import errors.

- [ ] **Step 5: Commit**

```powershell
git add frontend/src/user/pages/ScriptEditorPage.jsx frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx frontend/src/user/pages/script-editor/studioContract.js frontend/src/user/pages/script-editor/studioContract.test.js frontend/src/user/pages/script-editor.css
git commit -m "feat(script-editor): add full studio workspace shell"
```

### Task 2: 新增统一绑定迁移和领域模型

**Files:**
- Modify: `backend/internal/storage/script_editor_schema.go`
- Modify: `backend/internal/storage/script_editor_schema_test.go`
- Create: `backend/internal/scripteditor/bindings.go`
- Create: `backend/internal/scripteditor/bindings_test.go`
- Modify: `backend/internal/scripteditor/store.go`

**Interfaces:**
- Produces: `WorkBinding`、`SourceLink`、`AssetLink`、`JobLink`、`SyncState`、`ConflictState`。
- Produces store methods `UpsertWorkBinding(ctx, binding)`, `WorkBinding(ctx, owner, workID, targetType)`, `ListSourceLinks(ctx, owner, workID, targetType)` and corresponding asset/job link methods.

- [ ] **Step 1: Write failing migration and validation tests**

```go
func TestScriptEditorStudioBindingMigration(t *testing.T) {
    migrations := ScriptEditorMigrations()
    latest := migrations[len(migrations)-1]
    require.Equal(t, int64(8800412), latest.Version)
    for _, table := range []string{"script_editor_work_bindings", "script_editor_source_links", "script_editor_asset_links", "script_editor_job_links"} {
        require.Contains(t, latest.UpSQL, "CREATE TABLE IF NOT EXISTS "+table)
        require.Contains(t, latest.UpSQL, "owner_username")
        require.Contains(t, latest.UpSQL, "work_id")
    }
}

func TestWorkBindingValidateRequiresStableIdentity(t *testing.T) {
    err := (WorkBinding{WorkID: "work_1", TargetType: BindingCMCanvas}).Validate()
    require.ErrorIs(t, err, ErrValidation)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage ./internal/scripteditor -run 'Binding|Studio' -count=1`
Expected: FAIL because migration 8800412 and binding types are absent.

- [ ] **Step 3: Add migration and exact domain types**

```go
type BindingTargetType string
const (
    BindingCMCanvas BindingTargetType = "cm_canvas"
    BindingComicProject BindingTargetType = "comic_project"
    BindingAICutProject BindingTargetType = "aicut_project"
)

type WorkBinding struct {
    ID, OwnerUsername, WorkID, TargetID string
    TargetType BindingTargetType
    SourceRevision int64
    SyncState SyncState
    ConflictState ConflictState
    CreatedAt, UpdatedAt time.Time
}
```

Migration 8800412 gives every table an owner/work index and a unique stable-source key. Work deletion does not cascade into downstream project tables. Version 8800411 is already occupied by the energy receipt binding upgrade.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/storage ./internal/scripteditor -run 'Binding|Studio' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/storage/script_editor_schema.go backend/internal/storage/script_editor_schema_test.go backend/internal/scripteditor/bindings.go backend/internal/scripteditor/bindings_test.go backend/internal/scripteditor/store.go
git commit -m "feat(script-editor): add studio binding schema"
```

### Task 3: 持久化绑定和作品库完整操作

**Files:**
- Modify: `backend/internal/scripteditor/memory_store.go`
- Modify: `backend/internal/scripteditor/mysql_store.go`
- Create: `backend/internal/scripteditor/studio.go`
- Create: `backend/internal/scripteditor/studio_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `backend/internal/httpapi/script_editor_routes_test.go`

**Interfaces:**
- Produces: `Service.CopyWork(ctx, owner, workID, requestKey) (WorkDetail, error)` and binding store implementation.
- HTTP: `POST /api/script-editor/works/:workId/copy`, `GET|PUT /api/script-editor/works/:workId/bindings/:targetType`.

- [ ] **Step 1: Write failing service and route tests**

```go
func TestCopyWorkCopiesContentWithoutDownstreamBindings(t *testing.T) {
    copied, err := service.CopyWork(ctx, "alice", source.ID, "copy-once")
    require.NoError(t, err)
    require.Equal(t, source.Title+" 副本", copied.Title)
    require.Len(t, copied.Chapters, len(source.Chapters))
    require.Len(t, copied.Scripts, len(source.Scripts))
    _, err = store.WorkBinding(ctx, "alice", copied.ID, BindingCMCanvas)
    require.ErrorIs(t, err, ErrNotFound)
}
```

Route tests assert owner isolation, idempotent copy and 409 on stale revision.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'CopyWork|WorkBinding' -count=1`
Expected: FAIL with missing methods/routes.

- [ ] **Step 3: Implement stores, service and routes**

MySQL upserts use `(owner_username, work_id, target_type)` uniqueness. `CopyWork` opens one transaction, copies chapters/scripts with new IDs, changes AI/copied generation source to `copy`, clears preferred flags only when the source episode has no valid preferred version, and records request-key idempotency through a work-scoped job link.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'CopyWork|WorkBinding' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/memory_store.go backend/internal/scripteditor/mysql_store.go backend/internal/scripteditor/studio.go backend/internal/scripteditor/studio_test.go backend/internal/httpapi/script_editor_routes.go backend/internal/httpapi/script_editor_routes_test.go
git commit -m "feat(script-editor): persist studio links and work copies"
```

### Task 4: 补齐作品库菜单、标记、团队空状态和任务摘要

**Files:**
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorLibrary.jsx`
- Create: `frontend/src/user/pages/script-editor/libraryModel.js`
- Create: `frontend/src/user/pages/script-editor/libraryModel.test.js`
- Modify: `frontend/src/user/pages/ScriptEditorPage.jsx`
- Modify: `frontend/src/user/pages/script-editor.css`

**Interfaces:**
- Produces: `buildWorkMenu(work)` and `summarizeWorkJobs(jobs)`.
- Consumes: copy/update/delete/export/binding endpoints from Task 3.

- [ ] **Step 1: Write failing model tests**

```js
test('work menu exposes all Luo Shui actions', () => {
  assert.deepEqual(buildWorkMenu({ id: 'w1' }).map(item => item.key), ['open','rename','copy','export','workspace','mark','delete']);
});

test('running and failed jobs remain visible on work cards', () => {
  assert.deepEqual(summarizeWorkJobs([{status:'running'},{status:'failed'}]), { running: 1, failed: 1, queued: 0 });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test frontend/src/user/pages/script-editor/libraryModel.test.js`
Expected: FAIL because the helper is absent.

- [ ] **Step 3: Implement visible, working library actions**

Use Ant Design `Dropdown` and confirmation modals. Rename sends the current revision; copy uses one UUID request key per click; delete is soft-delete with title confirmation; team tab shows “暂未接入团队作品” without fake rows; cards show asset counts when provided and real job badges.

- [ ] **Step 4: Run tests and build**

Run: `node --test frontend/src/user/pages/script-editor/libraryModel.test.js`
Expected: PASS.

Run: `npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/ScriptEditorPage.jsx frontend/src/user/pages/script-editor/ScriptEditorLibrary.jsx frontend/src/user/pages/script-editor/libraryModel.js frontend/src/user/pages/script-editor/libraryModel.test.js frontend/src/user/pages/script-editor.css
git commit -m "feat(script-editor): complete works library actions"
```

### Task 5: 全文搜索、导入模式和章节引用状态

**Files:**
- Modify: `backend/internal/scripteditor/studio.go`
- Modify: `backend/internal/scripteditor/studio_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorSource.jsx`
- Create: `frontend/src/user/pages/script-editor/sourceSearch.js`
- Create: `frontend/src/user/pages/script-editor/sourceSearch.test.js`

**Interfaces:**
- HTTP: `GET /works/:id/search?q=...`, `POST /works/:id/import` with `mode: append|replace_chapter` and `expectedRevision`.
- Produces: `SearchHit{chapterId,title,start,end,excerpt}` and chapter `referencedByScriptCount`.

- [ ] **Step 1: Write failing tests**

```go
func TestSearchWorkReturnsTitleAndBodyOffsets(t *testing.T) {
    hits, err := service.SearchWork(ctx, "alice", work.ID, "月台")
    require.NoError(t, err)
    require.Equal(t, chapter.ID, hits[0].ChapterID)
    require.GreaterOrEqual(t, hits[0].Start, 0)
}
```

```js
test('search excerpt keeps the matching text', () => {
  assert.equal(buildSearchExcerpt('他在深夜月台等待列车', 4, 6, 4).includes('月台'), true);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'SearchWork|ImportIntoWork' -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/sourceSearch.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement search and three import destinations**

Search is owner-scoped and returns offsets, not rendered HTML. Replace-current requires exact chapter revision and a second confirmation. “导入为新作品” keeps using the existing preview/commit flow. The outline shows a referenced badge derived from `Script.SourceChapterIDs`.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'SearchWork|ImportIntoWork' -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/sourceSearch.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/studio.go backend/internal/scripteditor/studio_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/script-editor/ScriptEditorSource.jsx frontend/src/user/pages/script-editor/sourceSearch.js frontend/src/user/pages/script-editor/sourceSearch.test.js
git commit -m "feat(script-editor): add source search and import modes"
```

### Task 6: 剧本导入、对照、多版本比较和查找替换

**Files:**
- Modify: `backend/internal/scripteditor/studio.go`
- Modify: `backend/internal/scripteditor/studio_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorCompare.jsx`
- Create: `frontend/src/user/pages/script-editor/scriptCompare.js`
- Create: `frontend/src/user/pages/script-editor/scriptCompare.test.js`

**Interfaces:**
- HTTP: `POST /works/:id/scripts/import`, `POST /works/:id/scripts/replace`.
- Produces: `ImportScripts(ctx, owner, workID, []CreateScriptInput, requestKey) ([]Script,error)` and `ReplaceScripts(scope, find, replace, expectedRevisions)`.

- [ ] **Step 1: Write failing tests**

```go
func TestImportScriptsNeverOverwritesExistingEpisodeVersion(t *testing.T) {
    rows, err := service.ImportScripts(ctx, "alice", work.ID, []CreateScriptInput{{Episode:1, Version:1, Title:"第一集", Content:"新版"}}, "import-1")
    require.NoError(t, err)
    require.Equal(t, 2, rows[0].Version)
}
```

```js
test('line diff keeps insert delete and equal groups', () => {
  assert.deepEqual(diffScriptLines('A\nB', 'A\nC').map(x => x.type), ['equal','delete','insert']);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'ImportScripts|ReplaceScripts' -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/scriptCompare.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement exact version rules and side-by-side UI**

Imported versions receive `max(existing version for episode)+1`; no previous text changes. Compare supports two to four panes, line-level additions/deletions, selected-block copy into a new version, source chapter selection, current-version/current-episode/all-scripts replacement scopes and conflict result rows.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'ImportScripts|ReplaceScripts' -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/scriptCompare.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/studio.go backend/internal/scripteditor/studio_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/script-editor/ScriptEditorScripts.jsx frontend/src/user/pages/script-editor/ScriptEditorCompare.jsx frontend/src/user/pages/script-editor/scriptCompare.js frontend/src/user/pages/script-editor/scriptCompare.test.js
git commit -m "feat(script-editor): add script import and version comparison"
```

### Task 7: 接入作品 Agent 和审计写入

**Files:**
- Create: `backend/internal/scripteditor/agent_bridge.go`
- Create: `backend/internal/scripteditor/agent_bridge_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorAgent.jsx`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx`

**Interfaces:**
- Produces: `AgentContext{WorkID, ChapterIDs, ScriptIDs, AssetIDs, StoryboardIDs}` and `ApplyAgentDraft(ctx, owner, draft, expectedRevision)`.
- HTTP: `GET /works/:id/agent-context`, `POST /works/:id/agent-drafts/:draftId/apply`.

- [ ] **Step 1: Write failing tests**

```go
func TestAgentContextRejectsForeignReferences(t *testing.T) {
    _, err := bridge.BuildContext(ctx, "alice", work.ID, []Reference{{Type:"script", ID:bobsScript.ID}})
    require.ErrorIs(t, err, ErrForbidden)
}

func TestApplyAgentDraftCreatesAuditedVersion(t *testing.T) {
    script, err := bridge.ApplyDraft(ctx, "alice", draft, expectedRevision)
    require.NoError(t, err)
    require.Equal(t, SourceAI, script.GenerationSource)
    require.NotEmpty(t, draft.AuditID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scripteditor -run Agent -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement work-scoped Agent bridge and dock**

The dock reuses current Agent sessions, file references and skills; it serializes references as stable IDs, offers direct/confirm/read-only/ask modes, and applies text through the script-editor API so revision checks and audit records cannot be bypassed.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run Agent -count=1`
Expected: PASS.

Run: `npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/agent_bridge.go backend/internal/scripteditor/agent_bridge_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/user/pages/script-editor/ScriptEditorAgent.jsx frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx
git commit -m "feat(script-editor): connect work scoped agent"
```

### Task 8: 接入资产和分镜工作台

**Files:**
- Create: `backend/internal/scripteditor/asset_bridge.go`
- Create: `backend/internal/scripteditor/storyboard.go`
- Create: `backend/internal/scripteditor/storyboard_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorAssets.jsx`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorStoryboard.jsx`
- Create: `frontend/src/user/pages/script-editor/storyboardModel.js`
- Create: `frontend/src/user/pages/script-editor/storyboardModel.test.js`

**Interfaces:**
- HTTP: asset link list/preview/apply endpoints and storyboard CRUD/coverage endpoints under `/works/:id`.
- Produces: `StoryboardShot` with picture, shot size, camera, movement, bindings, dialogue, narration, duration and three prompt fields; `CoverageReport{Covered,Missing,Duplicate}`.

- [ ] **Step 1: Write failing tests**

```go
func TestStoryboardCoverageReportsMissingAndDuplicateSourceRanges(t *testing.T) {
    report := ComputeCoverage(100, []SourceRange{{Start:0, End:30},{Start:20, End:50}})
    require.Equal(t, []SourceRange{{Start:50, End:100}}, report.Missing)
    require.Equal(t, []SourceRange{{Start:20, End:30}}, report.Duplicate)
}
```

```js
test('invalid JSON preserves the last valid storyboard', () => {
  const result = parseStoryboardJson('{bad', [{id:'shot-1'}]);
  assert.equal(result.valid, false);
  assert.deepEqual(result.value, [{id:'shot-1'}]);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor -run Storyboard -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/storyboardModel.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement asset links and storyboard editor**

Assets use existing screenplay asset IDs and show character/scene/prop/director categories. Analysis results stay in preview until apply. Storyboards are grouped episode → scene → shot, preserve stable IDs during drag reorder, expose JSON/text views, and reject invalid JSON without replacing the last valid array.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'Asset|Storyboard' -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/storyboardModel.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/asset_bridge.go backend/internal/scripteditor/storyboard.go backend/internal/scripteditor/storyboard_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/user/pages/script-editor/ScriptEditorAssets.jsx frontend/src/user/pages/script-editor/ScriptEditorStoryboard.jsx frontend/src/user/pages/script-editor/storyboardModel.js frontend/src/user/pages/script-editor/storyboardModel.test.js
git commit -m "feat(script-editor): add assets and storyboard workspace"
```

### Task 9: 绑定 CM 画布并实现同步预览、冲突和回写

**Files:**
- Create: `backend/internal/scripteditor/canvas_sync.go`
- Create: `backend/internal/scripteditor/canvas_sync_test.go`
- Modify: `backend/internal/app/app.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Modify: `frontend/src/shared/api/scriptEditor.js`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorCanvas.jsx`
- Create: `frontend/src/user/pages/script-editor/canvasSyncModel.js`
- Create: `frontend/src/user/pages/script-editor/canvasSyncModel.test.js`

**Interfaces:**
- Produces: `EnsureCanvas(ctx, owner, workID) (WorkBinding,error)`, `PreviewCanvasSync(...) (SyncPreview,error)`, `ApplyCanvasSync(..., decisions, requestKey)`, `ApplyCanvasResult(...)`.
- HTTP: `POST /works/:id/canvas/ensure`, `POST /canvas/sync/preview`, `POST /canvas/sync/apply`, `POST /canvas/results/apply`.

- [ ] **Step 1: Write failing tests**

```go
func TestEnsureCanvasIsIdempotent(t *testing.T) {
    first, err := syncer.EnsureCanvas(ctx, "alice", work.ID)
    require.NoError(t, err)
    second, err := syncer.EnsureCanvas(ctx, "alice", work.ID)
    require.NoError(t, err)
    require.Equal(t, first.TargetID, second.TargetID)
}

func TestPreviewCanvasSyncDoesNotOverwriteManualNode(t *testing.T) {
    preview, err := syncer.Preview(ctx, "alice", work.ID, CanvasSyncRequest{ScriptIDs:[]string{script.ID}})
    require.NoError(t, err)
    require.Equal(t, "conflict", preview.Items[0].State)
}
```

```js
test('canvas href keeps work and return location', () => {
  assert.equal(buildCanvasHref('task-1','work-1'), '/agent/canvas?taskId=task-1&workId=work-1&returnTo=%2Fscript-editor%3FworkId%3Dwork-1%26tab%3Dcanvas');
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor ./internal/httpapi -run CanvasSync -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/canvasSyncModel.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement real `agentworkspace` integration**

`EnsureCanvas` calls `agentworkspace.Store.CreateProject` with `ProjectType: comic` and stores the returned task ID once. Synced nodes store `scriptEditorSourceType`, `scriptEditorSourceId`, `scriptEditorSourceRevision` and `scriptEditorLastSyncedAt` in `Node.Config`. Apply requires the preview revision and an explicit decision for every conflict: `keep_canvas`, `use_work`, or `copy_node`. Result write-back always shows the target asset/shot before applying.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi ./internal/agentworkspace -run 'CanvasSync|Canvas' -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/canvasSyncModel.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/canvas_sync.go backend/internal/scripteditor/canvas_sync_test.go backend/internal/app/app.go backend/internal/httpapi/script_editor_routes.go frontend/src/shared/api/scriptEditor.js frontend/src/user/pages/script-editor/ScriptEditorCanvas.jsx frontend/src/user/pages/script-editor/canvasSyncModel.js frontend/src/user/pages/script-editor/canvasSyncModel.test.js
git commit -m "feat(script-editor): bind works to CM Canvas"
```

### Task 10: 生产、后期和交付统一入口

**Files:**
- Create: `backend/internal/scripteditor/production.go`
- Create: `backend/internal/scripteditor/production_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorProduction.jsx`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorDelivery.jsx`
- Create: `frontend/src/user/pages/script-editor/productionModel.js`
- Create: `frontend/src/user/pages/script-editor/productionModel.test.js`

**Interfaces:**
- Produces: `ProductionStatus` aggregated from linked image/video/audio/subtitle/cut jobs.
- HTTP: `GET /works/:id/production`, binding creation for comic and AI-CUT, existing export/handoff endpoints remain canonical.

- [ ] **Step 1: Write failing tests**

```go
func TestProductionStatusKeepsUnknownCallSeparateFromFailed(t *testing.T) {
    view := AggregateProduction([]LinkedJob{{Stage:"video", Status:"may_have_sent"}})
    require.Equal(t, 1, view.ResultPendingConfirmation)
    require.Equal(t, 0, view.Failed)
}
```

```js
test('production tabs cover image video voice subtitle cut and export', () => {
  assert.deepEqual(PRODUCTION_STAGES.map(x => x.key), ['image','video','voice','subtitle','cut','export']);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor -run Production -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/productionModel.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement orchestration without duplicate engines**

The production page lists existing linked jobs and opens their canonical workspaces with work/script/shot IDs. It exposes model, reference, duration, ratio and voice controls only through existing service contracts. Buttons that can charge show a final submit confirmation. Delivery adds selected episodes, official-version validation, TXT/ZIP/JSON/manifest/archive formats, comic handoff, AI-CUT and Jianying capability checks.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run 'Production|Handoff|Export' -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/productionModel.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/production.go backend/internal/scripteditor/production_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/user/pages/script-editor/ScriptEditorProduction.jsx frontend/src/user/pages/script-editor/ScriptEditorDelivery.jsx frontend/src/user/pages/script-editor/productionModel.js frontend/src/user/pages/script-editor/productionModel.test.js
git commit -m "feat(script-editor): connect production and delivery flow"
```

### Task 11: 设置、布局恢复和全状态视觉验收

**Files:**
- Create: `backend/internal/scripteditor/settings.go`
- Create: `backend/internal/scripteditor/settings_test.go`
- Modify: `backend/internal/httpapi/script_editor_routes.go`
- Create: `frontend/src/user/pages/script-editor/ScriptEditorSettings.jsx`
- Modify: `frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx`
- Modify: `frontend/src/user/pages/script-editor.css`
- Create: `frontend/src/user/pages/script-editor/settingsModel.test.js`

**Interfaces:**
- Produces: `WorkSettings` with model IDs, script style, visual style, ratio, concurrency, autosave and storyboard density; every field carries `source: system|account|work`.
- HTTP: `GET|PUT /works/:id/settings` with revision checks.

- [ ] **Step 1: Write failing settings tests**

```go
func TestResolveSettingsReportsInheritanceSource(t *testing.T) {
    resolved := ResolveSettings(system, account, WorkSettings{Ratio: "9:16"})
    require.Equal(t, "work", resolved.Sources["ratio"])
    require.Equal(t, "account", resolved.Sources["modelId"])
}
```

```js
test('layout storage is scoped by account and work', () => {
  assert.equal(layoutStorageKey('alice','work-1'), 'script-editor:layout:alice:work-1');
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scripteditor -run Settings -count=1`
Expected: FAIL.

Run: `node --test frontend/src/user/pages/script-editor/settingsModel.test.js`
Expected: FAIL.

- [ ] **Step 3: Implement settings and polished states**

Persist work settings in MySQL, never secrets. Store rail width, compare visibility, storyboard visibility, Agent dock mode, font size and last location per account/work. Add designed empty/loading/error/forbidden states to every rail item and preserve the 44px/268px/main structure on desktop with a drawer fallback below 1100px.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/scripteditor ./internal/httpapi -run Settings -count=1`
Expected: PASS.

Run: `node --test frontend/src/user/pages/script-editor/settingsModel.test.js && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add backend/internal/scripteditor/settings.go backend/internal/scripteditor/settings_test.go backend/internal/httpapi/script_editor_routes.go frontend/src/user/pages/script-editor/ScriptEditorSettings.jsx frontend/src/user/pages/script-editor/ScriptEditorWorkspace.jsx frontend/src/user/pages/script-editor/settingsModel.test.js frontend/src/user/pages/script-editor.css
git commit -m "feat(script-editor): add studio settings and state polish"
```

### Task 12: 总体验证、Go embed 和本机 4001 验收

**Files:**
- Modify: `docs/acceptance/2026-11-27-script-editor-full-studio.md`
- Modify only if verification finds a defect: files owned by Tasks 1–11.

**Interfaces:**
- Consumes all prior interfaces; produces a traceable acceptance record with commit SHA, build identity and browser evidence.

- [ ] **Step 1: Run all focused frontend tests**

Run: `node --test frontend/src/user/pages/script-editor/*.test.js frontend/src/user/pages/script-editor-page.source.test.js`
Expected: every test passes.

- [ ] **Step 2: Run backend and integration tests**

Run: `go test ./internal/scripteditor ./internal/storage ./internal/httpapi ./internal/agentworkspace -count=1`
Expected: every package passes.

- [ ] **Step 3: Run full builds**

Run: `npm --prefix frontend run build`
Expected: React production build succeeds.

Run from `backend`: `go test ./... -count=1`
Expected: all Go tests pass.

- [ ] **Step 4: Build and restart through the approved Go embed path**

Run: `powershell -ExecutionPolicy Bypass -File scripts/deploy-go-embedded.ps1 -LocalOnly`
Expected: the script builds the frontend, embeds it into the Go binary, restarts only the confirmed local 4001 runtime and returns success.

- [ ] **Step 5: Verify runtime identity and no-charge browser flow**

Run: `Invoke-RestMethod http://127.0.0.1:4001/health`
Expected: healthy response.

Run: `Invoke-RestMethod http://127.0.0.1:4001/api/runtime-build-info | ConvertTo-Json -Depth 6`
Expected: Git SHA matches the locally built commit.

Browser acceptance covers: works grid/list and menu; import preview; source search; script comparison; Agent dock; asset and storyboard empty/data states; canvas ensure and sync preview without media submission; production status; export validation; refresh restoration. Do not press a button that submits a paid model or media job.

- [ ] **Step 6: Record evidence and commit**

```powershell
git add docs/acceptance/2026-11-27-script-editor-full-studio.md
git commit -m "docs(script-editor): record full studio acceptance"
```

## Self-review result

- Spec coverage: Tasks 1–6 cover works/source/script parity; Tasks 7–8 cover Agent/assets/storyboard; Task 9 covers momo→CM Canvas binding and two-way sync; Task 10 covers media/post/delivery; Task 11 covers settings and visual states; Task 12 covers local runtime acceptance.
- Placeholder scan: all tasks name exact files, interfaces, test commands, expected failures/passes and commit commands.
- Type consistency: all cross-module links use owner + work ID + stable target/source ID; canvas uses existing `agentworkspace.Project`, `Canvas`, `Node.Config` and revisions; all modifying operations carry revision or request key.
