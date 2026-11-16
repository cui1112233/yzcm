# Batch Prompt Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a mini-program user edit an asset prompt and one storyboard video prompt, with server-side persistence and readback.

**Architecture:** Add typed V12 client helpers around the existing asset PATCH and video override PUT contracts. The workbench owns temporary edit drafts and delegates persistence to its page container, which refreshes the server workspace after a successful receipt.

**Tech Stack:** Taro React, TypeScript, Vitest, Batch Factory V12 HTTP API.

## Global Constraints

- All production prompt content remains server-owned; no business prompt is persisted in local storage.
- Save receipts must be followed by a workspace reload.
- Asset updates include its `expectedRevision`; video updates include the selected video revision.
- The worktree may be dirty; stage or commit only files introduced by this change.

---

### Task 1: Typed V12 prompt persistence helpers

**Files:**
- Modify: `src/services/batchFactoryV11.ts`
- Modify: `src/services/batchFactoryV11.test.ts`

**Interfaces:**
- Produces: `saveBatchFactoryAssetPrompt(api, batchId, bookId, asset)`.
- Produces: `saveBatchFactoryVideoPrompt(api, batchId, bookId, videoId, prompt, expectedRevision)`.

- [ ] **Step 1: Write failing service tests**

```ts
await saveBatchFactoryAssetPrompt(api, "batch 1", "book/1", { id: "asset?1", name: "林晚", prompt: "新资产提示词", revision: 2 });
expect(api.request).toHaveBeenCalledWith(
  "/api/batch-factory/v12/batches/batch%201/books/book%2F1/assets/asset%3F1",
  { method: "PATCH", data: { name: "林晚", prompt: "新资产提示词", expectedRevision: 2 } }
);
```

- [ ] **Step 2: Run service test to verify it fails**

Run: `npm test -- src/services/batchFactoryV11.test.ts`
Expected: FAIL because the prompt persistence helper does not exist.

- [ ] **Step 3: Implement minimal helpers and receipt validation**

```ts
const saved = object(await api.request(path, { method: "PATCH", data }));
if (String(saved.asset?.prompt || "") !== data.prompt) throw new Error("资产提示词保存后读回不一致");
```

- [ ] **Step 4: Run service test to verify it passes**

Run: `npm test -- src/services/batchFactoryV11.test.ts`
Expected: PASS.

### Task 2: Workbench editing interaction

**Files:**
- Modify: `src/pages/batch/BookWorkbenchView.tsx`
- Modify: `src/pages/batch/BookWorkbenchView.interaction.test.tsx`

**Interfaces:**
- Consumes: `BookWorkbenchActions.saveAssetPrompt` and `BookWorkbenchActions.saveVideoPrompt`.
- Produces: edit/cancel/save controls for an asset and selected video.

- [ ] **Step 1: Write failing interaction tests**

```ts
click("编辑当前提示词");
expect(nodes(render()).some(node => node.type === "Textarea")).toBe(true);
await click("保存当前提示词");
expect(actions.saveVideoPrompt).toHaveBeenCalledWith("第一个镜头");
```

- [ ] **Step 2: Run interaction test to verify it fails**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx`
Expected: FAIL because editing controls are absent.

- [ ] **Step 3: Implement scoped drafts and save state**

```tsx
<Textarea value={draft} onInput={event => setDraft(event.detail.value)} />
<Button disabled={!draft.trim() || saving} onClick={() => void actions.saveVideoPrompt(draft.trim())}>保存当前提示词</Button>
```

- [ ] **Step 4: Run interaction test to verify it passes**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx`
Expected: PASS.

### Task 3: Page-container persistence and reload

**Files:**
- Modify: `src/pages/batch/index.tsx`
- Test: `src/pages/batch/BookWorkbenchView.interaction.test.tsx`

**Interfaces:**
- Consumes: service persistence helpers and active `batch.id` / selected book ID.
- Produces: action callbacks that refresh `visibleWorkspace` only after confirmed saves.

- [ ] **Step 1: Add page-level failing test or narrowly test the callback seam**

```ts
await actions.saveAssetPrompt(asset, "服务端确认提示词");
expect(refreshWorkspace).toHaveBeenCalledOnce();
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx`
Expected: FAIL because no save action is wired to the service.

- [ ] **Step 3: Wire save actions through `runAction` and refresh**

```ts
saveVideoPrompt: prompt => void runAction("book-video-prompt", async () => {
  await saveBatchFactoryVideoPrompt(api, batch.id, bookId, videoId, prompt, revision);
  setBookRefresh(value => value + 1);
}, "视频提示词已由服务端保存")
```

- [ ] **Step 4: Run focused tests and static checks**

Run: `npm test -- src/pages/batch/BookWorkbenchView.interaction.test.tsx && npx tsc --noEmit`
Expected: PASS.

### Task 4: Build and device acceptance

**Files:**
- Generated: `dist/` through the project build only.

- [ ] **Step 1: Run full regression suite**

Run: `npm test`
Expected: PASS.

- [ ] **Step 2: Build the WeChat package**

Run: `npm run build:weapp`
Expected: exit 0 and refreshed `dist/`.

- [ ] **Step 3: Verify in WeChat Developer Tools**

Open one actual batch book, edit one asset and one current storyboard prompt, save each, reload the book, and confirm the returned server value remains visible.
