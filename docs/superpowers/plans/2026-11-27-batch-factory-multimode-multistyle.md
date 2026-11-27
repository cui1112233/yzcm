# 批量工厂多画风、多模式实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Keep the existing `v88` branch; do not create branches or worktrees. Execution stays in this chat unless the user selects delegated execution.

**Goal:** 同一漫画作品项目支持1～5种画风，分别出图、合成和上传；新建批量可按同一书单创建视频、漫画、文字插画、文本四类独立项目。

**Architecture:** 分两段交付：任务1～5完成可独立使用的漫画多画风，任务6～7完成新建多模式，任务8完成整体运行验收。漫画原文和中性分镜共用，持久化成果按画风隔离；多模式仅在创建阶段复用来源数据，分发完成后的项目互相独立。

**Tech Stack:** Existing Go backend, React + Ant Design frontend, MySQL persistence, Redis dispatch/locks, TOS, Go embed. Follow the current repository directories; no unrelated relocation, Node application server, Actions deployment or price changes.

## Global Constraints

- 设计依据：`docs/superpowers/specs/2026-11-27-batch-factory-multimode-multistyle-design.md`。用户已确认“创建时复用，创建后独立修改”和“一个漫画项目内最多5种画风”。
- 所有工作直接在 `F:\脚本测试\一战晟铭-v88` 的 `v88` 维护；保留其他聊天的未提交修改。每项只提交自己的明确文件。
- 已知基线 `e8bdda71`，执行每项前重新读取最新 HEAD，不用本计划的历史基线覆盖当前文件。
- 同一漫画项目、同一书、同一正文页可有多种画风成果；图片、候选版本、主图、参考图、合集、上传回执及恢复状态都必须隔离。
- 漫画页及合集使用同一个已选画风身份；切换只查看，生成/合成/上传动作必须固定当时捕获的身份。
- 旧单画风按一项处理；原有图片ID、主图选择和上传回执不变。无法判断原画风的历史成果显示“原有画风”，不能冒充某个新画风。
- 第一段交付漫画多画风；文字插画本次保留其现有画风配置。其独立模式身份通过现有漫画生成能力承载，不直接增加现有后端不接受的 productionMode 枚举。
- 系统提示词正文只由后端 prompt 模块提供；客户端传预设选择，不信任客户端提交的系统正文。
- 原文完整保存；各项目独立截取、去敏、分页和生成。有效男女频与类型复用，不为四个项目重新分类四次。
- 不复制原项目的任务、成品、上传状态；不自动删除取消勾选的画风历史成果。
- 所有画风和项目继续受既有 Redis 全局容量限制。页数封顶按每种画风沿用既有配置。
- 先模拟供应商验证，再本地4001运行验收。真实付费生成、网络上传验收、推送、上线和更新 Mac 包按用户后续明确安排执行，不把模拟回执当成真实上传成功。
- 临时日志、数据库导出、截图和构建产物统一放 `_local-archive-20261028/multimode-multistyle-20261127/`，不进入 Git；只维护本设计和本计划两份文档。

## 代码边界及接口约定

现有漫画页以 owner/batch/book/video 查询一行，`comic_pages_mysql.go` 的唯一查询尚无画风范围。不能只改前端筛选，也不能把画风伪装成另一份书或另一套 video。

以下新增名称是本计划规定的接口，任务间不得自行更名：

```go
// backend/internal/batchfactoryv11/comic_styles.go
const MaxComicOutputStyles = 5
type ComicStyleOutput struct {
    PresetSnapshot
    GlobalReferenceImage ComicGlobalReferenceImage `json:"globalReferenceImage"`
}
// AIReasoningComicModule adds Styles []ComicStyleOutput `json:"styles,omitempty"`.
func NormalizeComicStyleOutputs(AIReasoningComicModule) ([]ComicStyleOutput, error)
func ResolveComicStyleSettings(SettingsPatch, string) (SettingsPatch, error)

// A scoped adapter is immutable; it never writes a shared "current style" field.
type ComicStyleScopedStore interface {
    ForComicStyle(string) ComicPageStore
}
// Add StyleID string `json:"styleId,omitempty"` to ComicPage,
// ComicCollection and BookAssetImage. ComicPageImage inherits its parent scope.

// backend/internal/batchfactoryv11/multimode_creation.go
type CreationMode string // video, comic, illustrated_text, text
type ModeProjectInput struct {
    Mode CreationMode `json:"mode"`
    Settings SettingsPatch `json:"settings"`
    PresetID string `json:"presetId,omitempty"`
    ExecutionMode string `json:"executionMode"`
}
type CreateMultiModeInput struct {
    RequestID string `json:"requestId"`
    Title string `json:"title"`
    Books []CreateBookInput `json:"books"`
    Projects []ModeProjectInput `json:"projects"`
}
type MultiModeCreationResult struct {
    GroupID string `json:"groupId"`
    Batches []Batch `json:"batches"`
}
type MultiModeCreationStore interface {
    CreateMultiModeBatches(context.Context, string, CreateMultiModeInput) (MultiModeCreationResult, error)
}
```

`ResolveComicStyleSettings` 返回深拷贝，不改变原配置；指定画风及其参考图覆盖到单次生成使用的 `Comic.Style/GlobalReferenceImage`，保留共用的提示词预设和约束。空的旧未配置设置仍可读取，执行预检明确要求至少一种有效画风。

接口使用 `styleId`：GET放查询参数，修改/生成请求放JSON字段；URL和body同时指定且不一致时返回400。服务端按当前成员、批次、书籍与预设目录校验，不能依赖名称或客户端正文。无参数的旧接口继续访问旧单画风范围，不返回混合画风列表。

## 第一段：同一项目内漫画多画风

### Task 1: 统一1～5种画风配置与预设读写

**Files**
- Create: `backend/internal/batchfactoryv11/comic_styles.go`, `comic_styles_test.go`.
- Modify: `backend/internal/batchfactoryv11/director_contract.go`, `settings.go`; `backend/internal/httpapi/batch_factory_prompt_config.go`.
- Test: `backend/internal/httpapi/batch_factory_comic_prompt_config_test.go`.
- Modify: `frontend/src/user/pages/shuihuo/batchFactoryComicStyle.js`, `batchFactoryComicStyle.test.js`.

**Interfaces:** Produces `ComicStyleOutput`, `NormalizeComicStyleOutputs`, `ResolveComicStyleSettings` above, plus frontend `normalizeComicOutputStyles(comic)` and `selectComicOutputStyle(comic, preset)` using metadata-only selections.

- [x] Write failing tests for legacy one-style read, five accepted, sixth rejected, duplicate IDs rejected, unknown/wrong-slot preset rejected, and no caller mutation. Include these boundary checks:

```go
func TestComicStylesRejectSix(t *testing.T) {
    module := AIReasoningComicModule{}
    for i := 0; i < 6; i++ {
        module.Styles = append(module.Styles, ComicStyleOutput{PresetSnapshot: PresetSnapshot{ID: fmt.Sprintf("style-%d", i)}})
    }
    if _, err := NormalizeComicStyleOutputs(module); err == nil { t.Fatal("six styles accepted") }
}
```

```js
assert.throws(() => normalizeComicOutputStyles({ styles: Array.from({ length: 6 }, (_, i) => ({ presetId: `s${i}` })) }));
assert.equal(normalizeComicOutputStyles({ style: { presetId: 'old' } })[0].presetId, 'old');
```

- [x] Run from backend: `go test ./internal/batchfactoryv11 ./internal/httpapi -run 'TestComicStyles|Test.*ComicPromptConfig' -count=1`; first new tests must fail on the absent normalization. Frontend: `node --test src/user/pages/shuihuo/batchFactoryComicStyle.test.js`.
- [x] Implement deep-copy normalization, preserve selected order, validate 1～5 for effective production, hydrate each style and style reference through the backend catalog. When both legacy `style` and `styles` exist, `styles` owns the list; preserve the old style only as legacy compatibility metadata, not an extra sixth output.
- [x] Extend sparse settings merge/preset import-export so changing styles does not discard comic prompt preset, constraints or another mode's settings. Test a six-style saved preset is rejected without partially changing current form values.
- [x] Run the focused tests green and commit only these files: `feat: define bounded comic output styles`.

### Task 2: 按画风保存页面、资产主图和合集，保护原有数据

**Files**
- Create: `backend/internal/storage/comic_style_output_schema.go`, `comic_style_output_schema_test.go`.
- Modify: `backend/internal/storage/local_executor_schema.go` (`AppMigrations` registration).
- Create: `backend/internal/batchfactoryv11/comic_style_store.go`, `comic_style_store_test.go`, `comic_style_mysql_integration_test.go`.
- Modify: `backend/internal/batchfactoryv11/types.go`, `comic_pages_mysql.go`, `comic_pages_memory.go`, `comic_assets_service.go`; asset-image MySQL/memory helpers called by the latter.

**Interfaces:** Consumes Task1 styles; produces immutable `ForComicStyle(styleID) ComicPageStore` on both stores. Asset-image queries/writes also carry an explicit style ID. Base source segments/videos retain their original IDs and scope.

- [x] Write the isolation test against memory and real isolated MySQL: same book/video under styles A/B creates distinct page IDs, both retain their own primary image; listing A never includes B; switching A's candidate does not touch B. Reusing another member's page/image/collection fails ownership checks.
- [x] Add an upgrade fixture with old page/image/collection IDs and a published receipt. After migration all IDs, primary flags and receipts must remain unchanged. Add a test for a late optimistic write conflict in A while B continues to save successfully.
- [x] Run new tests RED before changing schema. Provision a new isolated MySQL test database; never run migration fixtures against daily4001 or production data.
- [x] Register additive migration `8800501`, verified unused on 2026-11-27. Before implementation recheck latest registered migrations and choose a fresh unused number if another chat has occupied it; do not modify an applied migration checksum.
- [x] Add indexed `style_id` scope to pages, collections and asset images; replace the old page uniqueness on owner/batch/book/video with uniqueness including style. Locate the actual old index by its columns in `information_schema`, not an assumed name. Record an immutable legacy style binding per book before the first multi-style operation. Keep its original physical empty scope and resolve the original display style to it; moving old rows would break in-flight provider saves that already captured the empty scope. Unknown history stays in that legacy scope and is never copied to all selected styles.
- [x] Implement scoped adapters through private helpers with explicit style arguments. Preserve existing retry/backoff, ownership checks, optimistic revisions and stale-generation lease checks. Collection item validation must verify page style as well as ownership and book.
- [x] Execute MySQL tests with two concurrent same-video writes in different styles, process restart/readback and migration rerun. Run `go test ./internal/storage ./internal/batchfactoryv11 -run 'Test.*ComicStyle|Test.*ComicPage' -count=1`. Commit `feat: persist isolated comic style outputs`.

### Task 3: 贯通画风到出图、参考图、缓存、下载接口

**Files**
- Create: `backend/internal/httpapi/batch_factory_comic_style_scope.go`, `batch_factory_comic_style_scope_test.go`.
- Modify: `backend/internal/httpapi/batch_factory_comic_pages.go`, `batch_factory_comic_reference.go`, `batch_factory_comic_direct.go`, `batch_factory_v12_aliases.go`.
- Modify: `backend/internal/batchfactoryv11/comic_image_contract.go`, `comic_assets_service.go`.
- Modify: `frontend/src/shared/api/batchFactoryV12.js`.
- Test: `backend/internal/httpapi/batch_factory_comic_pages_test.go`; add `frontend/src/shared/api/batchFactoryV12.comicStyles.test.js`.

**Interfaces:** Consumes scoped stores and `ResolveComicStyleSettings`; API adapters carry `styleId` through list/generate/upload/primary/reference/collection/download requests. Every handler captures scope before beginning provider work and passes that same scope through completion.

- [x] Use fake image providers to record final prompts, reference images and request IDs for A/B. Assert shared story text is equal, each receives only its own style description/reference, and returned images persist to the right page. Add rejection cases for another book's style, conflicting query/body IDs and a manual cross-style image selection.
- [x] Assert identical neutral prompts under two styles produce different image-cache/request identities. Existing page/image ID-based content and thumbnail routes must check parent ownership/style instead of using the UI's current selection.
- [x] Run `go test ./internal/httpapi -run 'Test.*ComicStyleScope|Test.*ComicPage' -count=1` RED, then implement the resolver and scope propagation. Preserve late-result safety and errors; do not recompile a style-specific prompt as another style's source.
- [x] Verify both V12 aliases and older paths preserve query parameters; GET with omitted style remains backward compatible, while unscoped collection creation in a multi-style project requires an explicit selection rather than mixing output.
- [x] Add frontend API tests checking `encodeURIComponent(styleId)`, `view=lite` coexists with the style query, JSON generation requests include the ID, and download requests retain the same scope. Run `node --test src/shared/api/batchFactoryV12.comicStyles.test.js` green.
- [x] Commit `feat: carry comic style scope through generation and media APIs`.

### Task 4: 自动生产按画风恢复、合成和发布

**Files**
- Create: `backend/internal/httpapi/batch_factory_automation_comic_styles.go`, `batch_factory_automation_comic_styles_test.go`.
- Modify: `backend/internal/httpapi/batch_factory_automation.go`, `batch_factory_automation_preflight.go`, `batch_factory_n8_comic_publish.go`.
- Modify: `backend/internal/batchfactoryv11/comic_collection_selection.go`.
- Extend Task2's style output records with task snapshot/progress/retry/collection identity, registered before its migration is applied. Test files remain under the related Go packages.

**Interfaces:** A frozen run contains ordered style IDs and config snapshots. Existing book-level comic stages aggregate style runs; a failed A run never clears B's generated pages/collection/receipt. Collection ID remains the upload idempotency identity.

- [ ] Fake workflow: three styles × two pages; A succeeds and uploads, B fails on its second image, C succeeds and uploads. Retry must generate exactly B's missing image and submit exactly B's collection. Assert prompt extraction ran once and no successful image/receipt IDs changed.
- [ ] Fake two workers and restart recovery: completed style runs remain completed, provider-result save retries do not resubmit the model call, and upload retries do not regenerate images. Incomplete result-to-disk recovery retains existing uncertain-call protections.
- [x] Run `go test ./internal/httpapi -run 'Test.*ComicStyleAutomation|TestN8ComicPublish' -count=1` RED, then implement style-stage orchestration as a separate helper file. Every provider call still acquires existing global capacity; no per-style bypass or multiplied book concurrency.
- [x] Validate each collection contains only its style's page IDs/main images in source-page order; enforce existing per-collection size constraints. AIGC names add a stable style label only where multiple outputs require distinction, and preserve suffixes within the existing64-character limit. Never use display name as deduplication identity.
- [x] Aggregate current and total page counts across styles separately. Report 2/3 uploaded as partial, retain per-style errors, and ensure old single-style automation fixtures stay green.
- [x] Commit `feat: resume and publish comic styles independently`.

### Task 5: 漫画页与合集中的画风切换，以及1～5种选择界面

**Files**
- Modify: `frontend/src/user/pages/shuihuo/BatchFactoryComicStylePicker.jsx`, `BatchFactoryUnifiedSettingsModal.jsx`, `BatchFactoryBookSettingsModal.jsx`, `BatchFactoryCreateModal.jsx`.
- Create: `frontend/src/user/pages/shuihuo/ComicStyleOutputSelector.jsx`, `comicStyleMediaState.js`, `comicStyleMediaState.test.js`, `ComicStyleOutputSelector.browser.test.js`.
- Modify: `frontend/src/user/pages/shuihuo/BatchFactoryNovelList.jsx` at `ComicMediaPanel`, `InlineComicLibrary` and their action callers; extract style-specific helpers into `comicStyleMediaState.js` rather than adding another controller to the large JSX file.
- Modify: `frontend/src/user/pages/shuihuo-production.css` using existing `--sh-*` tokens only; test `comicMediaWorkbench.source.test.js` and the new browser fixture.

**Interfaces:** `ComicStyleOutputSelector({styles,value,onChange,disabled})`. State helper exports `comicStyleMediaKey(batchId,bookId,styleId)` and `isCurrentComicStyleResponse(capturedKey,currentKey)`. Generation callbacks receive a captured `styleId`, not a mutable selection reference.

- [x] Write pure-state and browser tests. In a fake API deferred promise test, select A, switch to B, resolve B then A; B's images/counts and action target must remain B. The selection action itself makes no generate/collection/publish calls.

```js
assert.notEqual(comicStyleMediaKey('b','k','A'), comicStyleMediaKey('b','k','B'));
assert.equal(isCurrentComicStyleResponse('b/k/A','b/k/B'), false);
```

- [x] Confirm tests RED with `node --test src/user/pages/shuihuo/comicStyleMediaState.test.js src/user/pages/shuihuo/ComicStyleOutputSelector.browser.test.js`.
- [x] Add multi-select behavior to the current style picker, show“已选3/5种画风”, keep preset previews and per-style reference associations. Sixth selection is disabled with visible explanation; unselecting or exchanging one remains possible. Saving/canceling cannot silently drop other configured styles.
- [x] Put the output selector in the existing comic media toolbar. Comic pages and collections share selection; retain source page position during switching. Empty/unrendered B pages show their actual state rather than A's images. Library page/collection caches, polling and pending writes include style in their key.
- [x] Wire generate, retry, reference selection, primary image, download, collection and upload to captured scope. Style-specific busy state must not disable unrelated completed styles. Unified/book/new-batch/preset settings use identical1～5 validation.
- [x] Browser verify 1021×901 and a wide viewport; use the existing Antd dark theme and spacing. Check one project card still represents all five styles. Run tests green, frontend build, commit `feat: browse up to five comic styles inside one project`.

## 第二段：新建批量创建多种独立模式

### Task 6: 一次来源读取与分类，创建独立项目并可靠分发

**Files**
- Create: `backend/internal/batchfactoryv11/multimode_creation.go`, `multimode_creation_test.go`, `multimode_creation_mysql.go`, `multimode_creation_memory.go`, `multimode_creation_mysql_test.go`.
- Create: `backend/internal/storage/batch_multimode_schema.go`, `batch_multimode_schema_test.go`; register in `backend/internal/storage/local_executor_schema.go`.
- Create: `backend/internal/httpapi/batch_factory_multimode_create.go`, `batch_factory_multimode_create_test.go`, `batch_factory_multimode_source.go`, `batch_factory_multimode_source_test.go`; wire through `backend/internal/httpapi/router.go`.
- Integrate: `backend/internal/batchfactoryv11/mysql_store.go` (`createBatchTx`), `memory_store.go`, `types.go`; `backend/internal/httpapi/batch_factory_v12_fetch.go`, `batch_factory_v12_single_fetch.go` and existing shared `novelfetchworkshop` classification service.

**Interfaces:** Uses the creation types above. New owner-authenticated endpoint `POST /api/batch-factory/v12/multi-mode-batches` accepts `CreateMultiModeInput`, returns `MultiModeCreationResult`. Existing single-batch endpoints retain their contracts.

Durable schema `8800502` (currently unused; recheck before application): creation group `(owner,request_id)` unique plus input digest, child project `(group,mode)` unique, shared source job `(group,source_key)` unique, and target book mappings with delivery status/revision. MySQL stores state/results; existing Redis task dispatch and short locks coordinate claims. Do not build an independent unbounded polling loop.

- [x] Tests: same request and digest submitted twice returns the same four batch IDs; same request ID with changed input is409; empty/five/repeated/unknown modes are400; another member cannot read or resume the group.
- [x] A source fake counts fetch/classification calls for two input books across four modes. Assert two fetches and two classification operations, four project copies, and no copied jobs/media/receipts. Existing supplied valid metadata skips AI. Duplicate book entries may share source work but keep the user's book rows and order.
- [x] Failure/restart tests: fail source delivery after two targets, restart and resume; delivered targets are unchanged and only two remain. Editing gender/type/text in a delivered target must not change another target. A manual change in a still-pending target wins over a late shared result; use expected revision/field provenance rather than overwriting the edit.
- [x] Run `go test ./internal/batchfactoryv11 ./internal/httpapi -run 'Test.*MultiMode' -count=1` RED before writing store/handler implementation.
- [x] Create group, four independently configured batches and mappings in one database transaction using `createBatchTx`. Validate all selected mode presets before creating any rows; return all children on retry using the unique request identity and digest.
- [x] Persist original source once, preserve full `originalRaw`, publish the source result to each target only once. Do not mark a source failed because one child has a mode-specific processing error. Each child applies its own capture, sanitization, segmentation and execution mode only after delivery.
- [x] Map `illustrated_text` at creation to existing comic production + `batch-comic-page-illustrated-text` preset and an explicit server-owned output-kind identity. Preset loading, project card labels, refresh recovery and publishing must retain that identity; adding this choice must not produce“生产模式无效”. Other three map to their existing production modes.
- [x] Verify MySQL uniqueness, redelivery and independent editing in a real isolated DB, plus memory-store equivalents. Run focused tests green; commit `feat: create independent mode projects from one book intake`.

### Task 7: 新建批量的多模式选择与各自预设

**Files**
- Create: `frontend/src/user/pages/shuihuo/BatchFactoryMultiModeCreationFields.jsx`, `batchFactoryMultiModeCreation.js`, `batchFactoryMultiModeCreation.test.js`, `BatchFactoryMultiModeCreation.browser.test.js`.
- Modify: `frontend/src/user/pages/shuihuo/BatchFactoryCreateModal.jsx`, `batchFactoryProductionMode.js`; `frontend/src/shared/api/batchFactoryV12.js`.
- Modify: project-card type-label handling in `frontend/src/user/pages/shuihuo/ProjectsView.jsx` and existing `batchFactoryProjects.js` helper; both paths must be rechecked in current HEAD before editing because this page has concurrent owners.
- Test: existing `BatchFactoryCreateModal.source.test.js`, `batchFactoryProductionMode.test.js` and new browser fixture.

**Interfaces:** `buildMultiModeCreationRequest({requestId,title,books,modes,settingsByMode})` creates the defined server request. `createMultiModeBatches(payload)` is the frontend API function. Mode-specific drafts and request identity survive retries; ordinary `batchFactoryProductionMode` continues to return the existing execution enum, while output kind is preserved separately.

- [x] Write a browser test selecting four modes, choosing four distinct presets and three comic styles, switching the editor between modes, then submitting. Assert each mode retains its settings and only one group-create request is made; clicking submit again or retrying a lost response reuses the request ID.
- [x] Write tests that a video preset cannot overwrite comic settings, a six-style comic preset cannot partially load, text capture/font/background are retained, and a single selected mode preserves existing creation behavior.
- [x] Run `node --test src/user/pages/shuihuo/batchFactoryMultiModeCreation.test.js src/user/pages/shuihuo/BatchFactoryMultiModeCreation.browser.test.js` RED.
- [x] Render the four mode choices only in new-batch UI. Each selected mode gets its own preset and unified-settings editor; an inline summary shows mode/preset/execution/style count without inventing generated output counts. Do not add a multi-mode switch to existing projects.
- [x] Persist the server response as the project group, refresh the normal project list and show all created projects. Partial source import appears as actual per-project progress, not a second group creation. Render video/comic/文字插画/text labels using shared theme styles.
- [x] Test four cards after refresh; editing one child's classification does not change another card/project. Run focused tests and frontend build green; commit `feat: configure four project modes during batch creation`.

## Task 8: 整体本地验收、同一版本证据与交付

**Files**
- Update this plan with evidence/commit IDs and the accepted design's status only.
- Evidence and screenshots: `_local-archive-20261028/multimode-multistyle-20261127/`.
- Existing build/runtime entry points: `scripts/start-local-go-review.ps1`, `scripts/deploy-go-embedded.ps1` (do not pass deployment flags without release authorization).

- [ ] Run focused Go tests from backend for storage, batchfactoryv11, httpapi and novelfetchworkshop. Run new frontend tests plus existing comic mode, polling, prompt isolation, collection publishing, preset persistence, classification and create flow regressions. Repeat only failures or checks affected by new fixes.
- [ ] Use synthetic source and provider fakes: one book/three styles, one partial page failure, restart recovery, three separate mock collection uploads; verify final image IDs and mock receipts per style. Separately create four projects from two source books, inspect exact source fetch/classification counts and compare independent edits.
- [ ] Build React and the Go embedded binary. Recheck actual4001 PID/path and current active jobs before replacing the local process; wait for active/uncertain provider work to drain. Never terminate an unrelated build or a production process by remembered PID.
- [ ] Verify `/health`, `/api/runtime-build-info` and embedded frontend entry match the built commit. Verify an authenticated session: existing single-style projects show original images; new projects show selected style pages/collections; no UI operation targets a different style/member.
- [ ] Record screenshots at1021×901 and desktop width for multi-style picker, comic-page selector, collection selector and four-mode new-batch editor. Visually compare shared spacing/colors with video and text settings.
- [ ] Actual network-upload acceptance remains distinct from fake tests. When authorized, submit a user-approved bounded fixture and verify remote receipt/listing for each output; do not report queued/generating or mocked receipts as successful real publication.
- [ ] Record checks, source SHA, migration numbers and user-visible results in this plan, retain rollback binary/database backup, and clean only owned temporary artifacts after validating paths. Preserve generated media and original backups.
- [ ] Deliver local4001 result and exact remaining release steps. Push, production deployment and Mac-package regeneration are separate follow-on actions; do not silently modify the already-delivered Mac ZIP.

## 计划自检与执行方式

- Config/limit/preset coverage: Task1 + Task5.
- Persistent page/image/reference/collection isolation and old data: Task2 + Task3.
- Independent retries, upload deduplication, queues and accounting scope: Task3 + Task4.
- Same-project viewing and late-response protection: Task5.
- Four independent projects, one classification and no later sync: Task6 + Task7.
- Local runtime identity, UI acceptance and actual-vs-mock upload evidence: Task8.

默认可在当前聊天顺序执行，每项完成后核对代码差异、测试和提交。若用户明确选择子任务代理，可将各自无文件冲突的测试/界面工作委派，但数据模型、迁移、接口与重启必须由主执行者串联核对；不因并行而跳过依赖或扩大上线权限。

## 执行记录

### 2026-11-27 本地交付记录

- 本地4001运行代码版本：`00286f89`；`/health` 返回200，`/api/runtime-build-info` 的运行SHA与清单SHA一致，前端入口为 `/assets/user-dpuRv_Tj.js`。独立保存的实际运行程序在本任务归档目录 `runtime-binaries/qiantie-local-00286f89.exe`。本机共享检出目录仍有其他聊天未提交的修改；本任务采用单独暂存，仅提交自己的改动，没有清理或代提交他人文件。
- 实现提交：`91baba44` 配置、`f5e563f6` 持久化、`64a1c5d0` 画风执行与接口、`37706d7c` 多模式及媒体界面、`0c95c032` 表单对齐、`696c8a0b` 保持合集选项、`00286f89` 服务端预设模式检查。直接维护 `v88`，未建分支或工作树，未推送或上线。
- 数据库迁移：8800501（画风范围及旧成果绑定）、8800502（多模式创建组、来源任务、独立项目和分发记录）。真实MySQL8.4测试通过，并验证重复迁移、并发创建、恢复后复用、原图片ID和主图保持。切换前已导出并解压校验 `before-multimode-verified.sql.gz`，约120MB原始SQL；未删原始备份。
- 最新针对性验证：画风/多模式HTTP测试14项、存储对应单元测试11项、相关N8漫画及画风HTTP测试24项、来源分类和文本处理测试47项通过；前端最终接口/配置测试38项、浏览器与延迟响应测试3项通过。React与Go embed构建通过。
- 扩展Go检查曾得到3398项通过、2项失败。失败为 `TestCustomCopyLegacyTextRequiresAuthoritativeReselection` 和 `TestFailedProcessingRetriesOnlyVerifiedRawAndKeepsCaptureSize`；将本任务修改的CreateBatch函数替换回HEAD基线后仍同样失败，原因是现有手动全文可信判定与旧断言不同。本任务未回退该既有行为，不能将扩展检查报告为全部通过。基线对照日志在归档目录 `baseline-source-tests.jsonl`。
- 模拟执行：三画风×两页，B的第二页首次失败，A/C继续完成；重试仅补B缺页，每画风只产生一次模拟发布，原成功图片ID不变。另有任务状态序列化再加载的恢复测试。模拟发布不是网络上传。
- 实际4001接口：同一创建请求重复提交返回同一组四个项目，完整原文已分别保存；视频12行、漫画分析3000字保留到实际读取的子书元数据；人工修改一个子项目男女频，其他项目内容不变。重启后六张手动测试图、三份独立合集仍存在。
- 已在登录浏览器检查新建四模式行、三画风选择、漫画页及合集切换。合集切换画风后保持当前栏目，1021×901与1440×1000窗口均无表单横向溢出；较短窗口沿用弹窗纵向滚动。表单使用现有主题变量；700px以下配置行改成两列。实际下载ZIP只有所选B画风两张图片，未混入A/C。测试图片明确印有 `LOCAL TEST`，不能作为真实模型出图效果。
- 运行验收摘要保存在归档目录 `runtime-acceptance.json`。保留名为“本地验收-多模式-1127”和“本地验收-三画风-1127”的独立测试项目，未操作原有书籍。
- 回退入口：归档目录 `rollback-stable-manifest.json` 指向原稳定 `4a276374` 程序；可用本任务 `switch-local-runtime.py` 切换，脚本先检查活动任务再停止已确认的4001进程。新增表为附加结构，回退程序不需要删除新表。数据库备份只在确实需要恢复数据时使用，不能盲目覆盖日常数据。
- 发布边界：本轮真实付费模型调用为0，网络上传为0；未更新Mac包。下一步若安排上线，应先授权一份有上限的真实出图/上传验收，逐画风核对网络回执，再从维护分支推送并走 `scripts/deploy-go-embedded.ps1`。未勾选的双工作进程综合模拟及真实供应商上传验收仍明确保留，不以局部测试替代。

- 任务1完成：Go画风配置/解析/系统目录冻结测试通过；前端画风相关18项测试通过。旧预览测试读取了已退役V11转发文件，已改为检查实际V12接口文件。该条记录为任务1当时状态；现已由后续任务开放生产与查看入口。

- 任务2完成：真实MySQL8.4空表结构夹具验证画风页面/资产图/合集隔离、并发写回、重复图片保存与迁移重复启动；原有图片ID及主图保留。迁移8800501。旧画风使用不可变关联映射到原物理范围，避免破坏在途请求。完整batchfactoryv11/storage单元测试已在当前共享源码上通过；临时依赖覆盖仅用于费用模块并发修改期间，最终单元测试不再使用覆盖。

### 2026-11-27 创建等待修复

- 原因：新建弹窗先等待缺失原文，创建多模式接口又同步完成整组读取/分类才返回；列表刷新也挡住自动启动。改为先落库并进入项目，自动模式交给现有逐书任务读取和生产；普通多模式在进入后调用现有共享原文恢复接口，不自动生成或发布。未新增临时后台循环。
- 单书任务只准备自己的共享源，慢书或失败书不再挡住其他书；现有共享源占用标记继续防止跨模式重复读取。
- 验证：Go HTTP相关22项、共享源相关6项、前端27项通过。真实Antd创建弹窗使用相同故障夹具做旧新对照：旧版等待不可用原文服务，新版不发前置原文请求并进入项目。新增实际页面创建处理函数测试，验证不等待列表刷新及手动模式原文有后续处理。只读复查指出的普通创建漏接原文已修正并复核。
- 证据保存在本机 _local-archive-20261028/batch-creation-latency-20261127。尚在执行的本地自动任务不能因更新被强行结束；以实际运行版本核验结果记录交付，不能用构建结果冒充4001已更新。本轮未推送、未上线、未调用真实付费模型。
