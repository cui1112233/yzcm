# Create Batch Content Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Place content capture and AI line-window defaults in the New Batch flow and persist those defaults for every imported book before any automation starts.

**Architecture:** The create form owns two validated defaults: a character ceiling for the source-fetch request and a first-N-non-empty-lines window for each imported book. `importBatchBooks` fetches with the selected ceiling, creates the batch, reloads its created books, writes each book's metadata with revision-guarded receipts, then performs one batch readback before applying configuration or starting automation.

**Tech Stack:** Taro React, TypeScript, Vitest, Batch Factory V12 HTTP API.

## Global Constraints

- Capture limit is an integer from `100` through `100000`, default `4000`.
- AI line window is an integer from `1` through `500`, default `5`.
- The values apply to every book in this one import; later per-book changes remain available in the source workbench.
- A source fetch receives `maxTxt`; it must not silently truncate an already saved source.
- The metadata route uses each created book's current revision; all per-book receipts and one final batch readback must match.
- Do not start immediate or scheduled automation until every selected-book metadata write has been confirmed.
- This checkout contains unrelated changes. Do not reset, clean, stage, or commit unrelated files.

---

### Task 1: Import-level default persistence

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Modify: `src/pages/batch/clientFlows.test.ts`

**Interfaces:**
- Extend `importBatchBooks` input with `contentRangeLines?: number` and `contentCaptureCharacters?: number`.
- Produce `saveBatchFactoryBookContentWindowDefaults(api, batch, values): Promise<BatchFactoryBatch>` to avoid reloading the entire batch after every one of 50 writes.
- `importBatchBooks` sends `{ platform, bookIds, maxTxt }` to `/fetch-originals`.
- It returns after every created book confirms the same metadata values, or returns a created batch plus a warning and does not launch automation.

- [x] **Step 1: Write failing service-flow tests**

```ts
test("new batch fetches with the selected capture ceiling and confirms each book window before automation", async () => {
  const calls: Array<{ path: string; data?: unknown }> = [];
  const api = { request: async (path: string, options?: any) => {
    calls.push({ path, data: options?.data });
    if (path.endsWith("fetch-originals")) return { results: [{ bookId: "1", status: "ok", data: "正文" }] };
    if (path.endsWith("manual")) return { intakeId: "i" };
    if (path.endsWith("intakes/i/batches")) return { batch: { id: "b", books: [] } };
    if (path.endsWith("batches/b")) return { batch: { id: "b", books: [{ id: "book-1", revision: 2, sourceMetadata: {} }] } };
    if (path.endsWith("books/book-1/metadata")) return { book: { id: "book-1", revision: 3, sourceMetadata: { contentRangeLines: 8, contentCaptureCharacters: 8000 } } };
    if (path.endsWith("automation/start")) return { automation: { batchId: "b", state: "running" } };
    return { automation: { batchId: "b", state: "running" } };
  } };
  await importBatchBooks(api, { title: "作品", groups: [{ platformId: "p", platformName: "平台", inputText: "1" }], contentRangeLines: 8, contentCaptureCharacters: 8000, execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2 });
  expect(calls[0]).toMatchObject({ path: expect.stringMatching(/fetch-originals$/), data: { platform: "p", bookIds: ["1"], maxTxt: 8000 } });
  expect(calls.find(call => call.path.endsWith("books/book-1/metadata"))?.data).toEqual({ metadata: { contentRangeLines: 8, contentCaptureCharacters: 8000 }, expectedRevision: 2 });
});
```

- [x] **Step 2: Verify red**

Run: `npm test -- src/pages/batch/clientFlows.test.ts`

Expected: FAIL because the fetch does not receive `maxTxt` and the flow does not write per-book metadata.

- [x] **Step 3: Implement the guarded import flow**

```ts
const requested = { contentRangeLines, contentCaptureCharacters };
const fetched = await fetchBatchFactoryOriginals(api, { platform: group.platformId, bookIds: ids, maxTxt: contentCaptureCharacters });
const fresh = normalizeBatch(unwrapBatchPayload(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batch.id)}`)));
batch = await saveBatchFactoryBookContentWindowDefaults(api, fresh, requested);
```

The helper sends one metadata write per book, validates every newer receipt, then reads `GET /batches/{batchId}` once and confirms every book's persisted values and revision. Run this after batch creation and before `saveBatchConfigVersion` or `startBatchExecution`. On an empty or malformed created-book list, return a warning that says defaults could not be confirmed and do not start automation.

- [x] **Step 4: Verify green**

Run: `npm test -- src/pages/batch/clientFlows.test.ts src/services/batchFactoryV11.test.ts`

Expected: PASS, including a test that metadata failure prevents `/automation/start`.

### Task 2: New Batch configuration controls

**Files:**
- Modify: `src/pages/batch/index.tsx`
- Modify: `src/pages/batch/index.test.ts`
- Modify: `src/pages/batch/mobile.scss`

**Interfaces:**
- New Batch `02 统一配置` renders numeric inputs labelled `原文获取上限` and `AI 内容范围`.
- `importBooks` passes parsed values to `importBatchBooks`.
- The import confirmation summary names both values and the selected-book count.

- [x] **Step 1: Write failing UI-source assertions**

```ts
test("new batch configuration exposes source capture and AI line defaults", () => {
  expect(source).toContain('aria-label="原文获取上限"');
  expect(source).toContain('aria-label="AI 内容范围"');
  expect(source).toContain("contentCaptureCharacters");
  expect(source).toContain("contentRangeLines");
});
```

- [x] **Step 2: Verify red**

Run: `npm test -- src/pages/batch/index.test.ts`

Expected: FAIL because the creation screen has only the global configuration template selector.

- [x] **Step 3: Implement inputs and validation**

```tsx
<View className="mp-batch-import-window">
  <Text className="mp-field-label">原文获取上限</Text>
  <Input aria-label="原文获取上限" type="number" value={contentCaptureCharacters} onInput={event => setContentCaptureCharacters(event.detail.value)} />
  <Text className="mp-field-label">AI 内容范围</Text>
  <Input aria-label="AI 内容范围" type="number" value={contentRangeLines} onInput={event => setContentRangeLines(event.detail.value)} />
</View>
```

Use the existing `validateContentWindow` helper. Disable confirmation for invalid values and display its exact error. Pass the normalized values to `importBatchBooks`; show `每本原文最多 {characters} 字，AI 默认使用前 {lines} 个非空行` in the summary.

- [x] **Step 4: Verify green**

Run: `npm test -- src/pages/batch/index.test.ts src/pages/batch/clientFlows.test.ts && npx tsc --noEmit`

Expected: PASS and TypeScript exits `0`.

### Task 3: Build and real-session boundary

**Files:**
- Generated: `dist/` from `npm run build:weapp`

- [x] **Step 1: Run regression checks**

Run: `npm test && npx tsc --noEmit && git diff --check`

Expected: all tests pass, no TypeScript errors, and no whitespace errors.

- [x] **Step 2: Build the WeChat package**

Run: `npm run build:weapp`

Expected: exit code `0` and current timestamps on `dist/pages/batch/index.js` and `dist/pages/batch/index.wxss`.

- [ ] **Step 3: Authenticated acceptance**

In WeChat Developer Tools, open `新建批量`, set `8000` and `8`, import one book, open its `原文`, and confirm `当前 AI 使用前 8 个非空行`. Start automation only after the create flow reports success. If the metadata route rejects the write, report the server receipt error and do not claim the defaults were stored.
