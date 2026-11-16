# Batch Factory Content Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user save and read back each Batch Factory book's logical-line range and future source-capture limit from the mini-program.

**Architecture:** Keep the saved source metadata as the authority. A small pure content-window module derives defaults, validates inputs, and produces display-only logical-line previews. The existing Batch Factory service sends a revision-guarded metadata update and validates both the write receipt and a fresh batch readback before the workbench refreshes.

**Tech Stack:** Taro React, TypeScript, Vitest, Batch Factory V12 HTTP API.

## Global Constraints

- `contentRangeLines` accepts an integer from `1` through `500`, default `5`.
- `contentCaptureCharacters` accepts an integer from `100` through `100000`, default `4000`.
- `contentRangeLines` controls the current book's first-N-non-empty-lines AI window; no browser code becomes a production source-of-truth.
- `contentCaptureCharacters` affects only future source capture/import and never destructively edits saved source text.
- Save through `PUT /api/batch-factory/v12/batches/{batchId}/books/{bookId}/metadata` with `expectedRevision`, then read back `GET /api/batch-factory/v12/batches/{batchId}`.
- Preserve unrelated `sourceMetadata` fields exactly; do not add local storage or a new endpoint.
- This shared working tree is dirty. Do not reset, clean, stage, or commit unrelated files.

---

### Task 1: Content-window pure helpers

**Files:**
- Create: `src/pages/batch/contentWindow.ts`
- Create: `src/pages/batch/contentWindow.test.ts`

**Interfaces:**
- Produces `readContentWindow(metadata?: Record<string, unknown>): { contentRangeLines: number; contentCaptureCharacters: number }`.
- Produces `validateContentWindow(input): { ok: true; value } | { ok: false; error: string }`.
- Produces `previewContentWindow(sourceText: string, lines: number): { availableLines: number; selectedLines: string[] }`.

- [ ] **Step 1: Write failing helper tests**

```ts
import { expect, it } from "vitest";
import { previewContentWindow, readContentWindow, validateContentWindow } from "./contentWindow";

it("uses safe defaults and only counts non-empty logical lines", () => {
  expect(readContentWindow({})).toEqual({ contentRangeLines: 5, contentCaptureCharacters: 4000 });
  expect(previewContentWindow("一\n\n 二 \n三\n四", 2)).toEqual({ availableLines: 3, selectedLines: ["一", "二"] });
});

it("rejects fractional or out-of-range window values", () => {
  expect(validateContentWindow({ contentRangeLines: "0", contentCaptureCharacters: "4000" })).toEqual({ ok: false, error: "内容范围需为 1 至 500 的整数" });
  expect(validateContentWindow({ contentRangeLines: "5", contentCaptureCharacters: "99" })).toEqual({ ok: false, error: "获取上限需为 100 至 100000 的整数" });
});
```

- [ ] **Step 2: Verify red**

Run: `npm test -- src/pages/batch/contentWindow.test.ts`

Expected: FAIL because `contentWindow.ts` and its exported functions do not exist.

- [ ] **Step 3: Implement the minimal pure module**

```ts
export function readContentWindow(metadata: Record<string, unknown> = {}) {
  const lines = Number(metadata.contentRangeLines);
  const characters = Number(metadata.contentCaptureCharacters);
  return {
    contentRangeLines: Number.isInteger(lines) && lines >= 1 && lines <= 500 ? lines : 5,
    contentCaptureCharacters: Number.isInteger(characters) && characters >= 100 && characters <= 100000 ? characters : 4000,
  };
}

export function previewContentWindow(sourceText: string, lines: number) {
  const logical = String(sourceText || "").split(/\r?\n/).map(line => line.trim()).filter(Boolean);
  return { availableLines: logical.length, selectedLines: logical.slice(0, lines) };
}
```

Implement `validateContentWindow` by parsing both numeric strings once, rejecting non-integers before returning the normalized value.

- [ ] **Step 4: Verify green**

Run: `npm test -- src/pages/batch/contentWindow.test.ts`

Expected: PASS with the default, preview, and validation cases.

### Task 2: Revision-guarded V12 metadata persistence

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Modify: `src/services/batchFactoryV11.test.ts`

**Interfaces:**
- Extends `BatchFactoryBook` with `sourceMetadata?: Record<string, unknown>`.
- Produces `saveBatchFactoryBookContentWindow(api, batchId, book, values): Promise<BatchFactoryBook>`.
- Consumes `values: { contentRangeLines: number; contentCaptureCharacters: number }` from Task 1.

- [ ] **Step 1: Write failing persistence tests**

```ts
const api = { request: vi.fn()
  .mockResolvedValueOnce({ book: { id: "book/1", revision: 8, sourceMetadata: { keep: "yes", contentRangeLines: 8, contentCaptureCharacters: 8000 } } })
  .mockResolvedValueOnce({ batch: { id: "batch 1", books: [{ id: "book/1", revision: 8, sourceMetadata: { keep: "yes", contentRangeLines: 8, contentCaptureCharacters: 8000 } }] } }) };

await expect(saveBatchFactoryBookContentWindow(api, "batch 1", { id: "book/1", revision: 7, sourceMetadata: { keep: "yes" } }, { contentRangeLines: 8, contentCaptureCharacters: 8000 })).resolves.toMatchObject({ revision: 8 });
expect(api.request).toHaveBeenNthCalledWith(1, "/api/batch-factory/v12/batches/batch%201/books/book%2F1/metadata", {
  method: "PUT", data: { metadata: { keep: "yes", contentRangeLines: 8, contentCaptureCharacters: 8000 }, expectedRevision: 7 },
});
expect(api.request).toHaveBeenNthCalledWith(2, "/api/batch-factory/v12/batches/batch%201");
```

Add a second test that rejects a same-revision receipt and a readback with a different value.

- [ ] **Step 2: Verify red**

Run: `npm test -- src/services/batchFactoryV11.test.ts`

Expected: FAIL because `saveBatchFactoryBookContentWindow` is not exported.

- [ ] **Step 3: Implement the minimal service helper**

```ts
export async function saveBatchFactoryBookContentWindow(api, batchId, book, values) {
  const expected = expectedRevision(book.revision, "内容范围");
  const metadata = { ...object(book.sourceMetadata), ...values };
  const saved = object(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(book.id)}/metadata`, {
    method: "PUT", data: { metadata, expectedRevision: expected },
  }));
  const receipt = object(saved.book);
  // Require matching id, a newer revision, and both persisted values here.
  const reloaded = normalizeBatch(unwrapBatchPayload(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}`)));
  // Return only the matching reloaded book after checking its revision and values.
}
```

Do not swallow `404`, `409`, or malformed-receipt errors. They must reach the workbench and keep its draft visible.

- [ ] **Step 4: Verify green**

Run: `npm test -- src/services/batchFactoryV11.test.ts`

Expected: PASS, including request path encoding and receipt/readback rejection tests.

### Task 3: Source-tab range controls and server-owned refresh

**Files:**
- Modify: `src/pages/batch/BookWorkbenchView.tsx`
- Modify: `src/pages/batch/BookWorkbenchView.interaction.test.tsx`
- Modify: `src/pages/batch/mobile.scss`
- Modify: `src/pages/batch/index.tsx`

**Interfaces:**
- Extends `BookWorkbenchActions` with `saveContentWindow(values: { contentRangeLines: number; contentCaptureCharacters: number }): Promise<void>`.
- Consumes Task 1 helpers and Task 2 service function.
- Produces a source-tab `AI 使用内容` editor, preview, and failure state.

- [ ] **Step 1: Write failing interaction tests**

```ts
test("source tab previews logical lines and saves only the selected book window", async () => {
  const actions = { ...baseActions, saveContentWindow: vi.fn().mockResolvedValue(undefined) };
  click("原文");
  expect(text(render())).toContain("AI 使用内容");
  expect(text(render())).toContain("当前 AI 使用前 5 个非空行");
  input("内容范围", "8");
  input("获取上限", "8000");
  await click("保存范围");
  expect(actions.saveContentWindow).toHaveBeenCalledWith({ contentRangeLines: 8, contentCaptureCharacters: 8000 });
});

test("invalid source window stays local and never invokes save", () => {
  click("原文");
  input("内容范围", "0");
  expect(text(render())).toContain("内容范围需为 1 至 500 的整数");
  expect(actions.saveContentWindow).not.toHaveBeenCalled();
});
```

Adapt the local Taro test tree helper to locate `Input` nodes by their `aria-label`; do not test CSS class names.

- [ ] **Step 2: Verify red**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx`

Expected: FAIL because the source tab has no `AI 使用内容` inputs or save callback.

- [ ] **Step 3: Implement the minimal workbench UI**

```tsx
const savedWindow = readContentWindow(workspace.book?.sourceMetadata);
const [windowDraft, setWindowDraft] = useState(savedWindow);
const validation = validateContentWindow(windowDraft);
const preview = previewContentWindow(sourceText, validation.ok ? validation.value.contentRangeLines : savedWindow.contentRangeLines);

<Section title="原文">
  <View className="mp-batch-content-window">
    <Text className="mp-row-title">AI 使用内容</Text>
    <Input aria-label="内容范围" type="number" value={String(windowDraft.contentRangeLines)} onInput={...} />
    <Input aria-label="获取上限" type="number" value={String(windowDraft.contentCaptureCharacters)} onInput={...} />
    <Button disabled={!validation.ok || unchanged || windowSaving} onClick={saveWindow}>保存范围</Button>
    <Text>当前 AI 使用前 {preview.selectedLines.length} 个非空行，共 {preview.selectedLines.length} / {preview.availableLines} 行。</Text>
  </View>
</Section>
```

`saveWindow` sets a busy state, clears the prior error, awaits `actions.saveContentWindow(validation.value)`, and only clears the draft error after successful completion. Use an effect keyed by `workspace.book?.revision` to replace the draft with confirmed server values after refresh. Render `获取上限仅用于下次重新获取原文` beside the capture field.

Style only the new `.mp-batch-content-window`, its input row, summary, and read-only preview. Preserve the business-page material treatment and existing full-original controls.

- [ ] **Step 4: Wire the parent action**

```tsx
saveContentWindow: async values => savePrompt(
  "book-content-window",
  () => saveBatchFactoryBookContentWindow(batchFactoryV11Api, batch.id, visibleWorkspace.book!, values),
  "内容范围已由服务端保存",
),
```

Use the currently loaded selected book, not an ID from a stale closure. Preserve `savePrompt`'s existing refresh behavior.

- [ ] **Step 5: Verify green**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx src/services/batchFactoryV11.test.ts && npx tsc --noEmit`

Expected: PASS. The interaction test must prove invalid input makes no save call and the selected-book callback receives both values.

### Task 4: Regression build and authenticated acceptance boundary

**Files:**
- Generated: `dist/` through the project build only.

- [ ] **Step 1: Run full static and behavioral regression**

Run: `npm test && npx tsc --noEmit && git diff --check`

Expected: all tests pass, TypeScript exits zero, and no whitespace errors are added.

- [ ] **Step 2: Build the WeChat package**

Run: `npm run build:weapp`

Expected: exit code `0`, then verify `dist/pages/batch/index.js` and `dist/pages/batch/index.wxss` have current timestamps.

- [ ] **Step 3: Verify the real server path in WeChat Developer Tools**

1. Open one authenticated Batch Factory book and switch to `原文`.
2. Change the range to `6` and capture ceiling to `8000`, then tap `保存范围`.
3. Leave and reopen the book; confirm the same values and six-line preview are returned.
4. Start the book's director stage and inspect its provider-facing source or server stage receipt to confirm it contains exactly the confirmed first six non-empty lines.
5. If the metadata route returns unavailable or 404, preserve the visible failure state and report the V12 server-contract gap; do not add a local-only fallback.
