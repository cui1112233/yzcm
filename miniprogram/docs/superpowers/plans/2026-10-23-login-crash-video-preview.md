# Clean Impact Login Intro Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a clean standalone MP4 that plays the supplied crash video unchanged, hides the landing cut with a short impact flash, and settles into the supplied transparent character-plus-login-frame artwork.

**Architecture:** Keep the original MP4 opaque through the riding, crash, and fall, so no white-background keying is required. At the measured final landing, render a brief white flash, small shake, and fading stars; while the flash is near peak opacity, replace the video with the clean RGBA final card and animate one damped squash/rebound. Export a muted 540 x 960 H.264 preview and review frames without changing the mini-program.

**Tech Stack:** Swift 6.3, Swift Package Manager, AVFoundation, CoreGraphics, ImageIO, zero-dependency executable checks, macOS `mdls`.

## Global Constraints

- Do not modify `src/components/LoginScene.tsx`, `src/components/LoginScene.scss`, app configuration, release version, or `dist`.
- Use `/Users/ming/Library/Containers/com.tencent.xinWeChat/Data/Documents/xwechat_files/wxid_gy86gz9hwwco12_d637/msg/video/2026-10/264421a9b1a22e77e953bebdc25d2a41_raw.mp4` unchanged before the landing transition.
- Use `/var/folders/6d/qr8qf0nn5zg4z2x9cbvvcblh0000gn/T/codex-clipboard-5ab00a25-6ed7-4451-8d28-8444da0f66f4.png` as the clean final-card source and copy it into the preview artifact directory before rendering.
- Do not key, segment, or extract a transparent foreground from the source video.
- Do not create a sprite sheet or frame-sequence animation for the mini-program.
- Output is 540 x 960, H.264, 24fps, no audio, approximately 5.5 seconds.
- Keep generated artifacts under `docs/previews/login-crash-intro/`; do not place the MP4 in the app package or upload it.
- User visual approval of the independent preview is the terminal acceptance boundary.

## File Map

- Modify `tools/login-intro-preview/Sources/LoginIntroPreviewCore/Timeline.swift`: replace keying/layout fields with impact-transition and final-card fields.
- Modify `tools/login-intro-preview/Sources/LoginIntroPreviewCore/PreviewRenderer.swift`: remove connected-background keying and render opaque-video-to-clean-card transition.
- Modify `tools/login-intro-preview/Sources/LoginIntroPreview/CLI.swift`: rename `--frame` to `--final-card` and verify transition review times.
- Modify `tools/login-intro-preview/Tests/LoginIntroPreviewChecks/main.swift`: validate revised ordering and impact animation math.
- Modify `docs/previews/login-crash-intro/timeline.json`: store measured transition and final-card layout.
- Create `docs/previews/login-crash-intro/assets/final-card.png`: stable copy of the supplied transparent artwork.
- Generate `docs/previews/login-crash-intro/login-crash-preview.mp4`, review frames, and `verification.json`.

---

### Task 1: Revised Timeline And Impact Motion Contract

**Files:**
- Modify: `tools/login-intro-preview/Sources/LoginIntroPreviewCore/Timeline.swift`
- Modify: `tools/login-intro-preview/Tests/LoginIntroPreviewChecks/main.swift`
- Modify: `docs/previews/login-crash-intro/timeline.json`

**Interfaces:**
- Produces: `IntroTimeline.validate(sourceDuration:)`, `flashOpacity(at:)`, and `cardTransform(at:)`.
- `IntroTimeline` fields: `crash`, `transitionStart`, `flashPeak`, `cardStart`, `settled`, `end`, `cardWidth`, `cardY`.

- [ ] **Step 1: Change executable checks to the revised contract**

Checks must require `transitionStart < flashPeak`, `cardStart` within 0.08 seconds of `flashPeak`, `cardStart < settled < end`, and `transitionStart < sourceDuration`. They must also assert flash opacity is `0` before transition, at least `0.95` at `flashPeak`, and returns to `0` after 0.18 seconds; final card transform must converge to scale `1` and zero offset.

- [ ] **Step 2: Run checks and confirm the old timeline does not compile**

Run: `swift run --package-path tools/login-intro-preview LoginIntroPreviewChecks`

Expected: FAIL because the revised fields and motion functions do not exist.

- [ ] **Step 3: Implement the revised timeline and deterministic animation math**

Use a triangular eased flash centered on `flashPeak`. Use damped sine/cosine functions for a maximum 4% vertical squash and 8px vertical offset; clamp both to final values at `settled`.

- [ ] **Step 4: Record the measured timing and layout**

Write: `crash=2.10`, `transitionStart=4.30`, `flashPeak=4.42`, `cardStart=4.40`, `settled=4.76`, `end=5.50`, `cardWidth=520`, `cardY=245`.

- [ ] **Step 5: Run checks and commit the contract**

Run: `swift run --package-path tools/login-intro-preview LoginIntroPreviewChecks`

Expected: PASS.

Commit only the timeline, checks, JSON, and copied `final-card.png` with message `test: define clean impact transition`.

### Task 2: Opaque Video To Clean Card Renderer

**Files:**
- Modify: `tools/login-intro-preview/Sources/LoginIntroPreviewCore/PreviewRenderer.swift`
- Modify: `tools/login-intro-preview/Sources/LoginIntroPreview/CLI.swift`

**Interfaces:**
- Consumes: source MP4, transparent final-card PNG, revised `IntroTimeline`, output path.
- Produces: muted 540 x 960 H.264 MP4 and `RenderReceipt`.

- [ ] **Step 1: Remove the white-key path**

Delete `connectedBackgroundRemoved`. Until `cardStart`, draw the original video frame opaquely at full canvas size. Never blend a keyed video foreground over the final card.

- [ ] **Step 2: Add the impact transition**

From `transitionStart` to `cardStart`, apply at most 3px deterministic horizontal/vertical shake to the opaque video. At and after `cardStart`, draw a pure-white background and the clean RGBA final card centered at `cardY`, using `cardTransform(at:)`. Draw four small four-point stars around the card, fading to zero by `settled`. Draw the white flash last so its peak fully hides the source-to-card cut.

- [ ] **Step 3: Preserve recoverable output replacement**

Keep the existing `.previous.mp4` move-before-render behavior and restoration on failure.

- [ ] **Step 4: Update CLI arguments**

The render command accepts `--final-card <png>` instead of `--frame <png>`. The verify command extracts frames at `transitionStart - 0.08`, `flashPeak`, `cardStart + 0.08`, `settled`, and `end - 0.08`.

- [ ] **Step 5: Build, run checks, and commit renderer code**

Run:

```bash
swift build --package-path tools/login-intro-preview
swift run --package-path tools/login-intro-preview LoginIntroPreviewChecks
```

Expected: build and checks PASS. Commit renderer and CLI changes with message `feat: render clean impact login preview`.

### Task 3: Render And Visually Verify The Preview

**Files:**
- Generate: `docs/previews/login-crash-intro/login-crash-preview.mp4`
- Generate: `docs/previews/login-crash-intro/preview-frames/*.png`
- Generate: `docs/previews/login-crash-intro/verification.json`

- [ ] **Step 1: Render the actual preview**

```bash
swift run --package-path tools/login-intro-preview LoginIntroPreview render \
  --source '/Users/ming/Library/Containers/com.tencent.xinWeChat/Data/Documents/xwechat_files/wxid_gy86gz9hwwco12_d637/msg/video/2026-10/264421a9b1a22e77e953bebdc25d2a41_raw.mp4' \
  --final-card docs/previews/login-crash-intro/assets/final-card.png \
  --timeline docs/previews/login-crash-intro/timeline.json \
  --output docs/previews/login-crash-intro/login-crash-preview.mp4
```

- [ ] **Step 2: Verify structure and extract five review frames**

Run the existing `verify` command. Require 540 x 960, H.264, 24fps, no audio, duration within 0.10 seconds of 5.50 seconds, and five present review PNGs.

- [ ] **Step 3: Inspect visual acceptance points**

Confirm the pre-impact video is pixel-clean and unchanged, the flash fully covers the cut, no keyed halo or double image exists, the final card remains fully inside the canvas, stars disappear by `settled`, and the final 0.7 seconds are stable.

- [ ] **Step 4: Record immutable artifact identity**

Run `shasum -a 256` for the MP4 and receipt and record byte size. Commit only the final MP4, five review frames, and receipt with message `preview: render clean crash login intro`.

- [ ] **Step 5: Present the independent preview**

Open the MP4 and representative frames in Codex. Report path, duration, dimensions, size, and SHA-256. Stop for visual approval; do not build or upload the mini-program.

## Completion Boundary

This plan completes only when the independent MP4 is structurally verified, visually inspected, and shown to the user. Mini-program integration remains a separate approved step.
