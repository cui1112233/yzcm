# Batch Factory Prompt Button Editing Design

## Goal

Make Batch Factory prompt editing discoverable without keeping long textareas open. Both asset prompts and storyboard video prompts use the same browse/edit/save interaction, while preserving the existing server-owned write and readback contracts.

## Scope

- Applies only to `BookWorkbenchView` in the Batch Factory mini-program.
- Covers asset prompts and the active storyboard video prompt.
- Does not alter prompt text, prompt APIs, revision checks, production tasks, or server-side permissions.

## Interaction

### Browse state

- Show the returned prompt as readable text.
- Show two compact action buttons below it: `编辑提示词` and `复制`.
- The edit action is visible only when the relevant save capability and record identity are available.

### Edit state

- Tapping `编辑提示词` replaces the static text with the existing textarea.
- Show `取消` and a visually primary `保存提示词` action.
- Disable saving while a request is pending or the trimmed text is empty.
- Retain the draft and render a safe failure message if saving fails.

### Completion

- On successful server response and existing caller readback, return to browse state.
- The caller continues to refresh the book workspace, so the displayed prompt comes from the confirmed server value rather than a local-only optimistic mutation.

## Technical Boundaries

- Reuse the existing `saveAssetPrompt` and `saveVideoPrompt` callbacks.
- Keep `expectedRevision` and prompt non-empty validation in the current service layer.
- Do not add a new endpoint, local persistence, or custom dialog.
- Reuse current mini-program button, textarea, busy, and warning styles; only add focused button styling if needed for visual hierarchy.

## Verification

- Add render/interaction tests for both prompt types entering edit mode, canceling, saving, and retaining safe errors.
- Keep service tests proving the PATCH/PUT payload and revision readback contracts.
- Run targeted Batch Factory tests, the full test suite, TypeScript validation, and `build:weapp`.
