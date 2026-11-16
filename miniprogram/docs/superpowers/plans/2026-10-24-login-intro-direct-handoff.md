# Login Intro Direct Handoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the non-interactive fake login-card shot from the entrance video and hand off directly to the real mini-program login form.

**Architecture:** Trim the approved MP4 before its synthetic card transition, publish the new content-addressed asset through the existing durable static route, then update `LoginScene` to request that asset. The existing `onEnded` transition remains the single owner of the handoff to the interactive form.

**Tech Stack:** Taro React, WeChat mini-program `<Video>`, Vitest, AVFoundation/`avconvert`, Nginx static assets.

## Global Constraints

- Preserve all footage before `4.30s`.
- No fake login card, fake fields, white-flash transition, or duplicate form.
- Keep the current real login form and login behavior unchanged.
- Do not stage or modify unrelated dirty-worktree files.
- Do not upload a mini-program release without separate user approval.

---

### Task 1: Lock the new media contract

**Files:**
- Modify: `src/components/LoginScene.test.tsx`
- Modify: `src/components/LoginScene.tsx`

**Interfaces:**
- Consumes: `loadLoginIntroMedia(url: string): Promise<string>`
- Produces: a new content-addressed `introVideo` URL and a `5s` safety timeout while preserving `onEnded -> ready`

- [ ] **Step 1: Write the failing test**

Change the expected URL to the new content-hashed MP4 and assert that the fallback timer cannot keep the fake scene visible longer than five seconds.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/components/LoginScene.test.tsx`

Expected: FAIL because `LoginScene` still references `login-intro-c69ddb98.mp4` and a `6500ms` timeout.

- [ ] **Step 3: Implement the minimal source update**

Replace only the media URL and safety timeout. Keep the existing `onEnded`, `onError`, local-file download, and real form markup.

- [ ] **Step 4: Run the focused test**

Run: `npx vitest run src/components/LoginScene.test.tsx src/services/loginIntroMedia.test.ts`

Expected: PASS.

### Task 2: Produce and publish the trimmed media

**Files:**
- Create: `docs/previews/login-crash-intro/login-crash-direct-handoff.mp4`
- Server create: `/opt/qiantie/v88/deploy/v88-public/static-assets/login-intro-<sha8>.mp4`

**Interfaces:**
- Consumes: `docs/previews/login-crash-intro/login-crash-preview.mp4`
- Produces: an H.264, `540x960`, approximately `4.30s`, silent MP4 served at `https://yzcm.site/assets/login-intro-<sha8>.mp4`

- [ ] **Step 1: Trim and re-encode**

Run `avconvert` with `PresetHighestQuality`, `--start 0`, `--duration 4.30`, fast-start enabled, and a fresh output path.

- [ ] **Step 2: Inspect the output and final frame**

Use the existing Swift `inspect` command and extracted frames. Confirm duration, H.264 dimensions, silence, and that the last frame contains the fallen characters but no login card.

- [ ] **Step 3: Publish by content hash**

Copy the asset into the persistent server static-assets directory, expose the exact `/assets/<filename>` route through the existing Nginx configuration, validate `nginx -t`, then reload Nginx.

- [ ] **Step 4: Verify public bytes**

Check HTTP `200`, Range `206`, `video/mp4`, byte length, and SHA-256 equality between local and public files.

### Task 3: Verify the mini-program integration

**Files:**
- Verify: `src/components/LoginScene.tsx`
- Verify: `dist/`

**Interfaces:**
- Consumes: the public content-addressed MP4 and existing request-to-local-file path
- Produces: a built mini-program whose entrance ends directly on the interactive login form

- [ ] **Step 1: Run all automated checks**

Run: `npm test`

Run: `npm run build:weapp`

Run: `git diff --check`

Expected: zero test failures, successful build, and no whitespace errors in scoped changes.

- [ ] **Step 2: Recompile and visually inspect in WeChat Developer Tools**

Confirm the loaded project is `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist`. Replay the login entrance and verify the fake card never appears, the real form appears immediately, and the inputs accept focus.

- [ ] **Step 3: Commit scoped files only**

Stage only the design, plan, trimmed video, `LoginScene.tsx`, and `LoginScene.test.tsx`. Do not stage `dist` or unrelated existing changes.
