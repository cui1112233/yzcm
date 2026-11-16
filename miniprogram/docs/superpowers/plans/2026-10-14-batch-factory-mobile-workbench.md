# Batch Factory Mobile Workbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将微信小程序批量工厂改造成与真实公网 Batch/Book 链路一致的移动生产工作台：个人作品首页、导入/新建、作品生产区、当前小说工作台，以及可验证的阶段状态和能力门禁。

**Architecture:** 保留 Taro 原生页面和现有 V11 服务层，将 `src/pages/batch/index.tsx` 拆成有限责任的页面区块组件；所有权威列表、状态、媒体和操作继续来自服务端，不在小程序本地保存生产结果。页面用显式的 `works -> create -> project -> book` 状态机管理入口和返回，当前书的每个请求都带 `batchId`、`bookId`。

**Tech Stack:** Taro 4.2.1、React 18、TypeScript、Vitest、微信小程序原生编译（`npm run build:weapp`）。

## Global Constraints

- 一个个人作品对应一个服务端 `Batch`；小说列表只属于已打开的作品。
- 单本生产单位是 `Book`；视频任务、合成任务和发布回读不得被压缩成单一“完成”。
- 书城目录、封面、状态、能力和错误只使用服务端返回值；禁止硬编码平台 ID、伪造封面或伪造成功。
- 中文界面不得出现 `Director`、`VIDEO`、revision 等内部字段；使用“分镜生产”“视频任务”“版本”等中文文案。
- 能力键使用服务端定义的 `production.submit`、`director.run`、`merge.run`、`publish.121`；缺失或拒绝时禁用操作并展示原因。
- 每个读写区必须区分加载、真实内容、真实空结果、不可用、失败、重试六种状态。
- 不引入 Cookie、localStorage、浏览器业务状态或 Web React 组件；密钥和平台凭据只留服务端。
- 不修改现有无关脏文件；每个任务单独测试并提交。

---

### Task 1: 固化 V11 服务契约与移动端领域模型

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Test: `src/services/batchFactoryV11.test.ts`
- Create: `src/services/batchFactoryModels.ts`
- Test: `src/services/batchFactoryModels.test.ts`

**Interfaces:**
- Consumes: 现有 `BatchFactoryV11Api.request`、`/api/batch-factory/v11/batches`、批次详情、状态、能力、自动化和 Intake 接口。
- Produces: `BatchFactoryViewState`、`BatchFactoryBookView`、`normalizeBatchFactoryBook`、`deriveBookStageSummary`、`statusTone`、`loadBatchFactoryProject`、`loadBookWorkspace` 等供页面组件使用的纯数据接口。

- [ ] **Step 1: Write failing model tests**

```ts
it("keeps batch, book and task status separate", () => {
  const view = normalizeBatchFactoryBook({
    id: "book-1",
    title: "第一本",
    production: { status: "running", taskId: "task-1" },
    merge: { status: "queued" },
    publish: { status: "unavailable", reason: "没有上传回执" },
  });
  expect(view.production.status).toBe("running");
  expect(view.merge.status).toBe("queued");
  expect(view.publish.reason).toBe("没有上传回执");
});

it("does not infer completion from an unknown status", () => {
  expect(statusTone("mystery-status")).toEqual({ tone: "unknown", label: "状态未返回" });
});
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `npm test -- --run src/services/batchFactoryModels.test.ts`

Expected: FAIL because the new model functions are not defined.

- [ ] **Step 3: Implement normalization without inventing server fields**

Add typed stage summaries for `source`, `assets`, `storyboard`, `video`, `merge`, and `publish`. Preserve unknown payloads as `unavailable`/`状态未返回`; do not map an absent field to success. Keep IDs and provider task IDs as strings, and expose `coverUrl` only when the server returns an actual media URL.

Extend `BatchFactoryV11State` with `lastLoadedAt`, `readState`, and optional `readError`, and add a `loadBatchFactoryProject(api, batchId)` helper that requests the list, selected detail, batch status, merge status, capabilities, and automation status while retaining which request failed.

- [ ] **Step 4: Add request-contract tests**

```ts
it("loads only the confirmed v11 project routes", async () => {
  const api = { request: vi.fn()
    .mockResolvedValueOnce({ batches: [{ id: "batch-1", title: "作品" }] })
    .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-1" }] } })
    .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
    .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
    .mockResolvedValueOnce({ capabilities: { "production.submit": { available: false, reason: "provider offline" } } })
    .mockResolvedValueOnce({ automation: null }) };
  await loadBatchFactoryProject(api, "batch-1");
  expect(api.request.mock.calls.map(([path]) => path)).toEqual([
    "/api/batch-factory/v11/batches",
    "/api/batch-factory/v11/batches/batch-1",
    "/api/batch-factory/v11/batches/batch-1/status",
    "/api/batch-factory/v11/batches/batch-1/merge-status",
    "/api/batch-factory/v11/capabilities",
    "/api/batch-factory/v11/batches/batch-1/automation",
  ]);
});
```

- [ ] **Step 5: Run service tests and commit**

Run: `npm test -- --run src/services/batchFactoryModels.test.ts src/services/batchFactoryV11.test.ts`

Expected: PASS.

Commit: `git add src/services/batchFactoryV11.ts src/services/batchFactoryV11.test.ts src/services/batchFactoryModels.ts src/services/batchFactoryModels.test.ts && git commit -m "feat: model batch factory project state"`

### Task 2: Make the page a real works/project/book state machine

**Files:**
- Modify: `src/pages/batch/index.tsx`
- Create: `src/pages/batch/batchFactoryPageState.ts`
- Test: `src/pages/batch/batchFactoryPageState.test.ts`

**Interfaces:**
- Consumes: Task 1 view models and existing Intake/automation functions.
- Produces: `BatchFactoryPageMode = "works" | "create" | "project" | "book"`, `BatchFactoryPageState`, `openProject`, `openBook`, `backToWorks`, and a single refresh path that never changes the current book after a stale response.

- [ ] **Step 1: Write failing state-transition tests**

```ts
it("opens a batch from the personal works screen", () => {
  const next = openProject({ mode: "works", selectedBatchId: "" }, "batch-7");
  expect(next).toEqual({ mode: "project", selectedBatchId: "batch-7", selectedBookId: "" });
});

it("returns from a book to the project and then to personal works", () => {
  const project = backToProject({ mode: "book", selectedBatchId: "batch-7", selectedBookId: "book-2" });
  expect(project.mode).toBe("project");
  expect(backToWorks(project).mode).toBe("works");
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npm test -- --run src/pages/batch/batchFactoryPageState.test.ts`

Expected: FAIL because the state helpers do not exist.

- [ ] **Step 3: Implement the state machine and request guards**

Use a reducer-style pure state transition. Every async load captures the requested `batchId` and `bookId`; on completion it updates state only when those IDs still match. Refreshing a project must not silently switch to the first batch. The cold-start page mode is `works`, not `project`.

- [ ] **Step 4: Refactor `BatchPage` to render mode-specific sections**

Move the current always-open batch detail into mode branches. `works` loads only the account-visible batch list. `create` opens the existing import/new-batch form. `project` shows the selected work and book rail/list. `book` shows the selected book workspace. Keep `BrandHeader showBack` and make back behavior explicit: book -> project, project -> works, works -> previous Tab/function entry.

- [ ] **Step 5: Run focused tests and commit**

Run: `npm test -- --run src/pages/batch/batchFactoryPageState.test.ts src/services/batchFactoryV11.test.ts`

Expected: PASS.

Commit: `git add src/pages/batch/index.tsx src/pages/batch/batchFactoryPageState.ts src/pages/batch/batchFactoryPageState.test.ts && git commit -m "feat: add batch factory navigation state"`

### Task 3: Build the personal works library and truthful create flow

**Files:**
- Create: `src/pages/batch/PersonalWorksView.tsx`
- Create: `src/pages/batch/BatchCreateSheet.tsx`
- Test: `src/pages/batch/PersonalWorksView.test.tsx`
- Test: `src/pages/batch/BatchCreateSheet.test.tsx`
- Modify: `src/services/batchFactoryV11.ts`
- Modify: `src/app.scss`

**Interfaces:**
- Consumes: `BatchFactoryViewState`, server-provided cover media, `createBatchFactoryBatch`, `createManualIntake`, `createBatchFromIntake`, `fetchBatchFactoryOriginals`, platform/preset/capability reads.
- Produces: callbacks `onOpen(batchId)`, `onCreate()`, `onRetry()`, and create results that distinguish `created`, `started`, and `scheduled`.

- [ ] **Step 1: Write UI tests for empty/loading/error and real works**

```tsx
it("shows only the empty works action when no batches are returned", () => {
  render(<PersonalWorksView state={{ readState: "empty", batches: [] }} onCreate={vi.fn()} onOpen={vi.fn()} onRetry={vi.fn()} />);
  expect(screen.getByText("还没有批量作品")).toBeTruthy();
  expect(screen.getByText("新建批量")).toBeTruthy();
});

it("opens a work without rendering its books on the personal works page", () => {
  render(<PersonalWorksView state={{ readState: "ready", batches: [{ id: "b1", title: "作品", books: [{ id: "book-1" }] }] }} onCreate={vi.fn()} onOpen={onOpen} onRetry={vi.fn()} />);
  fireEvent.click(screen.getByText("作品"));
  expect(onOpen).toHaveBeenCalledWith("b1");
});
```

- [ ] **Step 2: Run the tests and verify the new components fail**

Run: `npm test -- --run src/pages/batch/PersonalWorksView.test.tsx src/pages/batch/BatchCreateSheet.test.tsx`

Expected: FAIL because the components are not defined.

- [ ] **Step 3: Implement the two-column cover shelf**

Use a compact two-column list with real cover URL or deterministic text cover. Show title, book count, server updated time, and a truthful aggregate label. Keep actions icon-plus-text and limit card radius to 8px. Add explicit loading, empty, unavailable, error, and retry render paths.

- [ ] **Step 4: Replace hardcoded platform defaults in the create sheet**

Load the server book-city directory before enabling submission. Do not initialize `"2"`/`"番茄付费"`; if the directory is unavailable, display the reason and block import. Preserve multi-platform groups, content range, preset, run mode, concurrency, and Beijing schedule. “仅导入” must never call automation start; “立即执行” and “定时执行” must require a real preset and return the server automation status.

- [ ] **Step 5: Run tests and commit**

Run: `npm test -- --run src/pages/batch/PersonalWorksView.test.tsx src/pages/batch/BatchCreateSheet.test.tsx src/services/batchFactoryV11.test.ts`

Expected: PASS.

Commit: `git add src/pages/batch/PersonalWorksView.tsx src/pages/batch/PersonalWorksView.test.tsx src/pages/batch/BatchCreateSheet.tsx src/pages/batch/BatchCreateSheet.test.tsx src/services/batchFactoryV11.ts src/app.scss && git commit -m "feat: add personal batch works library"`

### Task 4: Add the opened-work production area and book rail

**Files:**
- Create: `src/pages/batch/BatchProjectView.tsx`
- Create: `src/pages/batch/BookRail.tsx`
- Create: `src/pages/batch/NovelListView.tsx`
- Test: `src/pages/batch/BatchProjectView.test.tsx`
- Modify: `src/app.scss`

**Interfaces:**
- Consumes: selected `BatchFactoryBatch`, normalized book summaries, automation status/actions, `onBack`, `onSelectBook`, `onRetry`, and batch action capability results.
- Produces: one work-level view with “返回个人作品”, horizontal book rail, novel-list subview, unified settings/automation/batch-action entry points, and explicit current-book selection.

- [ ] **Step 1: Write failing tests for hierarchy and selection**

```tsx
it("keeps novels inside the opened work", () => {
  render(<BatchProjectView batch={batch} books={books} onBack={onBack} onSelectBook={onSelectBook} />);
  expect(screen.getByText("返回个人作品")).toBeTruthy();
  expect(screen.getByText("小说列表")).toBeTruthy();
  expect(screen.queryByText("个人作品")).toBeNull();
});

it("selects one book without creating a card per video task", () => {
  render(<BatchProjectView batch={batch} books={[{ ...book, videoTasks: [{ id: "v1" }, { id: "v2" }] }]} onBack={onBack} onSelectBook={onSelectBook} />);
  expect(screen.getAllByText("第一本")).toHaveLength(1);
  fireEvent.click(screen.getByText("第一本"));
  expect(onSelectBook).toHaveBeenCalledWith("book-1");
});
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `npm test -- --run src/pages/batch/BatchProjectView.test.tsx`

Expected: FAIL because the production-area components are not defined.

- [ ] **Step 3: Implement the horizontal book rail and list**

The rail shows title, status tone, and short state only. The list exposes serial number, title, Book ID, book city, style, gender, source, and truthful status. Batch actions must state whether they apply to current book, selected books, or all books before calling the existing server action.

- [ ] **Step 4: Connect automation controls to server state**

Render scheduled/running/paused/needs-attention states with counts and per-book messages. Pause, continue, retry-failed-items, and cancel use the existing V11 functions and refresh from the server after each action. Do not turn a successful button click into a completed label.

- [ ] **Step 5: Run tests and commit**

Run: `npm test -- --run src/pages/batch/BatchProjectView.test.tsx src/pages/batch/batchFactoryPageState.test.ts`

Expected: PASS.

Commit: `git add src/pages/batch/BatchProjectView.tsx src/pages/batch/BookRail.tsx src/pages/batch/NovelListView.tsx src/pages/batch/BatchProjectView.test.tsx src/app.scss && git commit -m "feat: add batch project book workbench"`

### Task 5: Add the current-book workbench with server-owned stage boundaries

**Files:**
- Create: `src/pages/batch/BookWorkbenchView.tsx`
- Create: `src/pages/batch/BookStageSection.tsx`
- Test: `src/pages/batch/BookWorkbenchView.test.tsx`
- Modify: `src/services/batchFactoryV11.ts`
- Modify: `src/app.scss`

**Interfaces:**
- Consumes: `batchId`, `bookId`, `loadBookWorkspace`, capabilities, source/assets/stages/merge/publish payloads where V11 confirms them, `runBookDirector`, `runProduction`, `runMerge`, and automation refresh.
- Produces: four compact work zones: “原文与生产内容”“人物与场景”“分镜与视频”“合成与发布”; every action returns a server-read state.

- [ ] **Step 1: Write failing tests for stage isolation and gates**

```tsx
it("sends the selected batch and book IDs for book actions", async () => {
  render(<BookWorkbenchView batchId="batch-1" bookId="book-2" workspace={workspace} actions={actions} />);
  fireEvent.click(screen.getByText("生成当前书分镜"));
  await waitFor(() => expect(actions.runDirector).toHaveBeenCalledWith("batch-1", "book-2"));
});

it("blocks publish without receipt and readback", () => {
  render(<BookWorkbenchView batchId="batch-1" bookId="book-2" workspace={{ publish: { status: "blocked", reason: "缺少平台读回" } }} actions={actions} />);
  expect(screen.getByText("缺少平台读回")).toBeTruthy();
  expect(screen.getByText("发布")).toHaveAttribute("disabled");
});
```

- [ ] **Step 2: Run the test and verify failure**

Run: `npm test -- --run src/pages/batch/BookWorkbenchView.test.tsx`

Expected: FAIL because the workbench is not defined.

- [ ] **Step 3: Implement the four zones with explicit states**

The source zone displays server content/range and marks dependent results “需更新” after a saved source change. Assets expose only confirmed categories and show image generation as unavailable when the capability is absent. Storyboard/video rows show task ID, provider status, duration, failure reason, and single-task retry. Merge shows progress and protected media only when returned.

- [ ] **Step 4: Add service helpers only for confirmed V11 routes**

Use V11 source/metadata/override/assets/stages/merge/publish route shapes listed in the migration inventory. For any route whose handler is not confirmed, return a typed `unavailable` state and render the reason; do not create an optimistic client-side fallback or copy the unverified single-book merge-status wrapper.

- [ ] **Step 5: Fix capability key usage and user-facing Chinese copy**

Replace any `production.run` check with `production.submit`. Keep internal compatibility values confined to the service layer. Button text must use “生成当前书视频”“提交合成”“发布到平台” only when the matching server capability and prerequisite receipt state are present.

- [ ] **Step 6: Run tests and commit**

Run: `npm test -- --run src/pages/batch/BookWorkbenchView.test.tsx src/services/batchFactoryV11.test.ts`

Expected: PASS.

Commit: `git add src/pages/batch/BookWorkbenchView.tsx src/pages/batch/BookStageSection.tsx src/pages/batch/BookWorkbenchView.test.tsx src/services/batchFactoryV11.ts src/pages/batch/index.tsx src/app.scss && git commit -m "feat: add current book production workspace"`

### Task 6: Wire the full page and protect navigation/visual states

**Files:**
- Modify: `src/pages/batch/index.tsx`
- Modify: `src/components/BrandHeader.tsx`
- Modify: `src/app.scss`
- Test: `src/components/BrandHeader.test.ts`
- Test: `src/pages/batch/BatchPage.integration.test.tsx`

**Interfaces:**
- Consumes: all components from Tasks 2–5.
- Produces: a usable cold-start route, stacked back navigation, safe retry behavior, and responsive mobile layout.

- [ ] **Step 1: Write failing integration tests**

```tsx
it("starts at personal works and reaches a book without losing the parent work", async () => {
  render(<BatchPage />);
  await screen.findByText("我的作品");
  fireEvent.click(screen.getByText("作品 A"));
  await screen.findByText("小说列表");
  fireEvent.click(screen.getByText("第一本"));
  expect(await screen.findByText("原文与生产内容")).toBeTruthy();
  fireEvent.click(screen.getByText("返回作品"));
  expect(screen.getByText("小说列表")).toBeTruthy();
});
```

- [ ] **Step 2: Implement back fallback and stale-load cleanup**

If a detail page has no navigation stack, `BrandHeader` must fall back to the batch Tab/function entry instead of discarding a failed `navigateBack()`. Clear polling timers and pending refresh guards on unmount or book switch.

- [ ] **Step 3: Apply mobile layout constraints**

Use a fixed, stable horizontal rail height, safe-area padding, 8px maximum regular radius, no nested card wall, and text wrapping/truncation that keeps controls visible on narrow screens. Keep the approved light cover-library and active-book workbench direction; do not reintroduce dark gradients or fake media.

- [ ] **Step 4: Run integration tests and commit**

Run: `npm test -- --run src/pages/batch/BatchPage.integration.test.tsx src/components/BrandHeader.test.ts`

Expected: PASS.

Commit: `git add src/pages/batch/index.tsx src/components/BrandHeader.tsx src/app.scss src/components/BrandHeader.test.ts src/pages/batch/BatchPage.integration.test.tsx && git commit -m "feat: wire batch factory mobile flow"`

### Task 7: Verify the mini-program build and real-service acceptance boundary

**Files:**
- Modify: `docs/superpowers/handoffs/2026-10-14-batch-factory-mobile-workbench-handoff.md`
- Test: all batch factory unit/integration tests

**Interfaces:**
- Consumes: completed page, service, component and navigation changes.
- Produces: evidence that source tests, generated `dist/`, WeChat Developer Tools compile, authenticated server reads, and capability/readback behavior are distinct acceptance checks.

- [ ] **Step 1: Run focused and full tests**

Run: `npm test -- --run src/services/batchFactoryV11.test.ts src/services/batchFactoryModels.test.ts src/pages/batch src/components/BrandHeader.test.ts`

Expected: PASS for all scoped tests; unrelated existing failures must be listed separately rather than hidden.

- [ ] **Step 2: Build the WeChat bundle**

Run: `npm run build:weapp`

Expected: exit code 0 and regenerated `dist/` containing the new batch page assets. Do not claim simulator acceptance from this command alone.

- [ ] **Step 3: Compile in WeChat Developer Tools**

Open the project configured by `project.config.json`, compile the generated `dist/`, and verify both empty and populated authenticated states. Check works -> project -> book -> project -> works navigation, one failed video retry, unavailable publish, and light/dark theme contrast.

- [ ] **Step 4: Verify real service evidence**

With a real authenticated account, record the exact batch list/detail/status responses and the selected `batchId`/`bookId` for each mutation. Confirm that a publish label appears only after upload receipt and platform readback. A build, HTTP 200, or clickable button is not completion evidence.

- [ ] **Step 5: Write the handoff and commit**

Record tested routes, served build identity, simulator result, known unavailable capabilities, and any provider/readback blockers in the handoff file. Commit only the handoff and final implementation files.

Commit: `git add docs/superpowers/handoffs/2026-10-14-batch-factory-mobile-workbench-handoff.md && git commit -m "docs: record batch factory mobile verification"`
