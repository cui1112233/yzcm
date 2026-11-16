# Batch Factory Mobile Media Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with checkpoints.

**Goal:** 将已确认的四层移动端设计落成可用的小程序链路，重点让单本工作区读取并展示真实原文、分镜提示词、资产图片/提示词、视频、合成状态和受公网确认单保护的提交流程。

**Architecture:** 复用现有 `BatchPage` 的 works/project/book 状态机和公网 Batch Factory V11 路由。服务层负责规范化批次、资产图片、生产任务、合成结果和外部发布意图；页面层只根据服务端数据渲染状态和原生 `Image`/`Video`，不生成业务假数据。媒体 URL 需要通过现有 Bearer 会话下载到小程序本地临时文件后再交给原生组件。

**Tech Stack:** Taro 4.2.1、React 18、TypeScript、Vitest、微信小程序原生 `Image`/`Video`/`FileSystemManager`。

## Global Constraints

- 任何作品、小说、提示词、资产、任务状态和视频均必须来自服务端；前端不得用样例数据补足空结果。
- 图片和视频不把 Bearer token 拼到 URL，不写入日志或浏览器存储。
- 不把 HTTP 200、任务提交成功或 provider task 存在当作视频完成；必须区分真实 `mediaUrl` 和可播放/读回。
- 复用现有作品/批次/单本页面状态机，不新建第二套批次业务流程。
- 微信开发者工具编译 `dist/`；源码、测试和构建不等于微信模拟器验收。
- 公网微信登录路由和合法域名仍是上线前置条件；若未打通，只报告为真实登录待验收。

---

### Task 1: Extend the workspace contract and prove the real request graph

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Test: `src/services/batchFactoryV11.test.ts`

**Interfaces:**
- Consumes: `BatchFactoryV11Api.request` and existing V11 batch/stage/asset/status routes.
- Produces: typed `BatchFactoryAsset`, `BatchFactoryAssetImage`, `BatchFactoryProductionTask`, `BatchFactoryBookWorkspace.production`, `BatchFactoryBookWorkspace.assetImages`, `BatchFactoryBookWorkspace.sourceText` and workspace loader requests for assets/images/status.

- [ ] **Step 1: Write failing tests for wrapped payloads and request paths**

Add a fixture where the batch detail contains `sourceText`, assets returns `{ data: { assets: [...] } }`, each asset image endpoint returns `{ images: [{ id, url, prompt }] }`, and status contains two books with different tasks. Assert the selected book receives only its own tasks and that the call list includes:

```ts
"/api/batch-factory/v11/batches/batch-1",
"/api/batch-factory/v11/batches/batch-1/books/book-2/stages",
"/api/batch-factory/v11/batches/batch-1/books/book-2/assets",
"/api/batch-factory/v11/batches/batch-1/status",
"/api/batch-factory/v11/batches/batch-1/books/book-2/assets/asset-1/images",
```

Add assertions that an empty `mediaUrl` does not produce a playable media item and a returned `mediaUrl` is preserved verbatim.

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `npm test -- --run src/services/batchFactoryV11.test.ts`

Expected: FAIL because the current workspace loader does not request status or asset images and does not expose production/source fields.

- [ ] **Step 3: Add normalization types and minimal loader behavior**

Define explicit types for asset metadata, image metadata and production tasks. Normalize `data`, `assets`, `images` and `jobs` wrappers. In `loadBatchFactoryBookWorkspace`, request detail, stages, assets and status; after assets resolve, request each asset’s `/images` endpoint. Filter production jobs by `job.bookId`, task `bookId`, or selected book ID and retain only non-empty `mediaUrl`, `videoId`, `status`, prompt and error fields. Keep `readState: "empty"` when no selected book exists and `readState: "error"` for request failures.

- [ ] **Step 4: Run the focused tests and verify they pass**

Run: `npm test -- --run src/services/batchFactoryV11.test.ts`

Expected: PASS, including the previous wrapped batch and fallback-book tests.

- [ ] **Step 5: Commit the service contract**

```bash
git add src/services/batchFactoryV11.ts src/services/batchFactoryV11.test.ts
git commit -m "feat: load real book media and production status"
```

### Task 2: Add authenticated media caching without leaking tokens

**Files:**
- Create: `src/services/mediaCache.ts`
- Test: `src/services/mediaCache.test.ts`
- Modify: `src/services/api.ts` only if a response-type helper is required

**Interfaces:**
- Consumes: a relative or absolute media URL and the existing `apiRequest` authentication path.
- Produces: `loadAuthenticatedMedia(url, extension): Promise<{ path: string; fromCache: boolean }>`; callers receive a local file path suitable for Taro `Image` or `Video`.

- [ ] **Step 1: Write failing tests for safe media handling**

Mock the request and file-system APIs. Assert that relative paths are requested with `responseType: "arraybuffer"`, bytes are written below `Taro.env.USER_DATA_PATH`, repeated URLs use the cache, and a URL containing a token/query is not logged or rewritten with an Authorization value.

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `npm test -- --run src/services/mediaCache.test.ts`

Expected: FAIL because the cache module does not exist.

- [ ] **Step 3: Implement the smallest cache**

Normalize URL keys, hash them with a deterministic non-cryptographic filename-safe key, request bytes through the authenticated API path, write atomically to `Taro.env.USER_DATA_PATH`, and return the local path. Keep cache failures as explicit errors so the UI can show “媒体读取失败”，not an empty success state.

- [ ] **Step 4: Run the focused tests and verify they pass**

Run: `npm test -- --run src/services/mediaCache.test.ts src/services/api.test.ts`

Expected: PASS with no token value in test output.

- [ ] **Step 5: Commit the media boundary**

```bash
git add src/services/mediaCache.ts src/services/mediaCache.test.ts src/services/api.ts
git commit -m "feat: cache authenticated batch media locally"
```

### Task 3: Replace the placeholder single-book view with the real mobile workbench

**Files:**
- Modify: `src/pages/batch/BookWorkbenchView.tsx`
- Modify: `src/pages/batch/BookWorkbenchView.test.ts`
- Modify: `src/pages/batch/index.tsx`
- Modify: the batch page stylesheet imported by `src/app.tsx` or `src/app.scss` where the existing `mp-batch-*` rules live

**Interfaces:**
- Consumes: `BatchFactoryBookWorkspace`, capabilities, current busy state and action callbacks.
- Produces: tabs for workbench/source/storyboard/assets/tasks, real `Image`/`Video` elements only when media paths exist, prompt detail drawers, and explicit merge/submit action states.

- [ ] **Step 1: Write failing component/source tests**

Add source-level assertions that the view imports `Image` and `Video`, renders `sourceText`, asset prompt/image fields, production media only when `mediaUrl` is present, and labels the empty states as server-unreturned rather than using sample cards. Add a regression assertion that book mode does not also render the project toolbar/list.

- [ ] **Step 2: Run the page tests and verify they fail**

Run: `npm test -- --run src/pages/batch/BookWorkbenchView.test.ts src/pages/batch/index.test.ts`

Expected: FAIL because the current view only renders text summaries and `BatchPage` renders the project shell while `pageState.mode === "book"`.

- [ ] **Step 3: Implement the single-book workbench**

Use a local tab state. Render:

```tsx
<Video src={localVideoPath} controls />
<Image src={localAssetPath} mode="aspectFill" />
```

only after authenticated media caching succeeds. Show the original text in a collapsed summary with an explicit expand control, the selected storyboard prompt as read-only server text, asset image/prompt rows, filtered production tasks and merge status. Wire `runDirector`, `runProduction`, `runMerge` and a new publish-confirmation callback. Keep “提交” disabled until the service reports a successful merge output, required upload receipt and provider capability; show the exact blocking reason.

- [ ] **Step 4: Fix the page state boundary**

Change the project shell guard so the toolbar, import modal and batch automation section render only for `pageState.mode === "project"`; book mode renders only the back control and `BookWorkbenchView`. Pass the selected book’s provider/publish configuration through a typed callback rather than reading browser storage.

- [ ] **Step 5: Run focused tests and verify they pass**

Run: `npm test -- --run src/pages/batch/BookWorkbenchView.test.ts src/pages/batch/index.test.ts`

Expected: PASS, including no-media, failed-media, empty-assets, and book-mode shell tests.

- [ ] **Step 6: Commit the workbench UI**

```bash
git add src/pages/batch/BookWorkbenchView.tsx src/pages/batch/BookWorkbenchView.test.ts src/pages/batch/index.tsx src/app.scss
git commit -m "feat: build real single-book mobile workbench"
```

### Task 4: Add the real external submit confirmation flow

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Test: `src/services/batchFactoryV11.test.ts`
- Modify: `src/pages/batch/BookWorkbenchView.tsx`
- Modify: `src/pages/batch/index.tsx`

**Interfaces:**
- Consumes: provider ID, batch/book identity, merged media URL, production video IDs and configured publish settings.
- Produces: `createPublishIntent`, `confirmPublishIntent`, `submitPublishIntent`, and UI phases `idle -> confirmation-required -> submitting -> submitted/failed`.

- [ ] **Step 1: Write failing tests for the three-step publish contract**

Mock these exact requests and assert the payload never contains credentials:

```ts
POST /api/batch-factory/v11/publish/{provider}/intents
POST /api/batch-factory/v11/publish/{provider}/intents/{intentId}/confirm
POST /api/batch-factory/v11/publish/{provider}/intents/{intentId}/submit
```

Assert the UI cannot call confirm/submit before an intent exists and surfaces the returned reference/audit state.

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `npm test -- --run src/services/batchFactoryV11.test.ts src/pages/batch/BookWorkbenchView.test.ts`

Expected: FAIL because the mini-program service has no external publish methods or confirmation state.

- [ ] **Step 3: Implement service methods and UI confirmation**

Reuse the web contract: create an intent with batch/book IDs, publish settings, merged URL and selected production video IDs; require explicit confirmation before submit; show provider reference and audit-readback status. On 409/410/unavailable responses, keep the page in a recoverable state and do not show success.

- [ ] **Step 4: Run focused tests and commit**

Run: `npm test -- --run src/services/batchFactoryV11.test.ts src/pages/batch/BookWorkbenchView.test.ts`

```bash
git add src/services/batchFactoryV11.ts src/services/batchFactoryV11.test.ts src/pages/batch/BookWorkbenchView.tsx src/pages/batch/index.tsx
git commit -m "feat: require publish confirmation in mobile workbench"
```

### Task 5: Verify the full mini-program build and public-data boundary

**Files:**
- Modify: only tests or copy/style files required by failures from Tasks 1-4

- [ ] **Step 1: Run all mini-program tests**

Run: `npm test -- --run`

Expected: PASS with no snapshot or unrelated generated-file changes.

- [ ] **Step 2: Build the WeChat bundle**

Run: `npm run build:weapp`

Expected: Taro writes a fresh `dist/` bundle without TypeScript or template errors.

- [ ] **Step 3: Check the diff and served identity**

Run: `git diff --check` and inspect `dist/` only; do not add `.env.production`, `.superpowers/`, or unrelated pre-existing dirty files.

- [ ] **Step 4: Record the acceptance boundary**

In the final report, distinguish source/test/build results from the still-required WeChat Developer Tools checks: authenticated login, real asset image readback, real video playback/readback, merge completion, provider confirmation, and publish audit. Do not claim public release until those checks pass.

