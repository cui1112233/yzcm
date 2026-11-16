# Batch Factory Content Window Design

## Goal

Restore the confirmed per-book content-window controls in the mini-program. A user can set how many non-empty source lines are used by the current book's AI workflow and the character ceiling used the next time source is captured. The mini-program saves both values to the server and renders a confirmed readback; it does not silently slice, persist, or treat a local preview as authoritative content.

## Scope

- Applies to the `原文` tab in the Batch Factory single-book workbench.
- Adds two per-book fields backed by `book.sourceMetadata`:
  - `contentRangeLines`: `1..500`, default `5`; the active AI content window is the first N non-empty logical lines.
  - `contentCaptureCharacters`: `100..100000`, default `4000`; the maximum source size used by the next source capture/import.
- Shows a read-only preview of the active line window and the number of available non-empty lines.
- Saves through `PUT /api/batch-factory/v12/batches/{batchId}/books/{bookId}/metadata` with the current book revision, then reloads the workspace to prove the stored metadata matches the receipt.

## Interaction

### Browse state

Above the full original text, show a compact `AI 使用内容` section:

- `内容范围`: numeric field followed by `行`.
- `获取上限`: numeric field followed by `字`.
- A single primary `保存范围` button, disabled for invalid or unchanged values.
- A confirmed summary such as `当前 AI 使用前 5 个非空行，共 5 / 18 行。`.
- An `截取预览` block containing only the selected logical lines. The existing full original remains available below it.

### Save and failure behavior

- The client validates whole-number limits before any request.
- Save sends a complete metadata object that preserves unrelated keys, with `expectedRevision` from the selected book.
- A success receipt must return the current book with a strictly newer revision and matching two metadata fields. The page then refreshes the workspace and uses the reloaded values.
- On 409 or a malformed receipt, keep the draft visible, show a safe error, and do not claim the range is saved.

## Semantics

- `contentRangeLines` changes the current book's AI working window. The established Batch Factory director/production implementations consume this server-owned field.
- `contentCaptureCharacters` controls future source capture/import only. It does not destructively shorten an already stored full original. The UI must say this explicitly so a user does not mistake the setting for a retroactive source rewrite.
- Each book owns its values. Changing one book never changes other books in the batch.

## Architecture

1. Extend the mini-program book type so `sourceMetadata` is available from the workbench response.
2. Add small pure helpers for metadata defaults, input validation, and previewing the first N non-empty lines.
3. Add `saveBatchFactoryBookContentWindow` in the existing Batch Factory service. It owns request encoding, revision validation, receipt validation, and batch readback.
4. Pass the save action from `BatchPage` into `BookWorkbenchView`; the existing page refreshes its server workspace after a confirmed save.
5. Keep existing director, production, prompt, asset, and full-original flows unchanged.

## Tests and Verification

- Unit-test defaults, validation boundaries, and logical-line preview behavior.
- Unit-test the metadata request and rejection of an unchanged/invalid receipt.
- Interaction-test entering values, preview update, rejected invalid input, successful save callback, and failure preservation.
- Run targeted tests, TypeScript validation, the full suite, `build:weapp`, and inspect refreshed `dist/pages/batch/*` timestamps.
- In the authenticated WeChat Developer Tools session, save a range for one actual book, reopen it, confirm readback, then run the book's director stage and verify the provider-facing source window is limited to the confirmed logical lines.

## Non-goals

- No browser persistence of prompt or source content.
- No bulk apply-to-all-books action in this slice.
- No destructive edit of a captured full original when changing the character capture ceiling.
- No guessed endpoint or fallback when the V12 metadata route is absent; report that server contract gap instead.
