# Login Wheel Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a lightweight wheel that repeatedly rolls behind the real login card until the user leaves the login page.

**Architecture:** Extract one transparent wheel asset from the existing login parts artwork. Render it in a pointer-inert layer below `nl-card-assembly`, and let CSS own translation, rotation, idle time, pause, and reduced-motion behavior without React timers.

**Tech Stack:** Taro React, SCSS/WXSS animation, PNG assets, Vitest, WeChat Developer Tools.

## Global Constraints

- The wheel appears only in `ready`, never over the entrance video.
- The wheel stays behind the login card and never intercepts pointer input.
- Use the existing illustrated wheel; do not introduce a mismatched generated asset.
- Pause on inactive pages and hide for reduced motion.
- Do not modify the login video or authentication behavior.
- Do not stage unrelated dirty-worktree files.

---

### Task 1: Lock the wheel contract with a failing test

**Files:**
- Modify: `src/components/LoginScene.test.tsx`

**Interfaces:**
- Consumes: `LoginScene(props)`
- Produces: assertions for `.nl-wheel-track`, `.nl-wheel`, PNG source, and pointer-inert markup

- [ ] Add a test that expects the wheel layer and PNG image.
- [ ] Run `npx vitest run src/components/LoginScene.test.tsx` and confirm it fails because the layer is absent.

### Task 2: Add the wheel asset and background layer

**Files:**
- Create: `src/assets/login/wheel.png`
- Modify: `src/components/LoginScene.tsx`
- Modify: `src/components/LoginScene.scss`

**Interfaces:**
- Consumes: `wheel.png`
- Produces: `.nl-wheel-track` at `z-index: 2` and `.nl-wheel` animated only under `.nl-ready`

- [ ] Crop the wheel from `parts.png` into a transparent square and visually inspect it.
- [ ] Import `wheel.png` and render it in a dedicated track before `.nl-card-assembly`.
- [ ] Add the `7s` translate-and-rotate animation, paused/reduced behavior, and `pointer-events: none`.
- [ ] Run the focused test and confirm it passes.

### Task 3: Build and visually verify

**Files:**
- Verify: `src/components/LoginScene.tsx`
- Verify: `src/components/LoginScene.scss`
- Verify: `dist/`

**Interfaces:**
- Consumes: built login page
- Produces: visual evidence that the wheel enters left, disappears behind the card, exits right, and does not block inputs

- [ ] Run `npm test`, `npm run build:weapp`, and `git diff --check`.
- [ ] Recompile `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist` in WeChat Developer Tools.
- [ ] Observe one complete loop and click the form controls while the wheel is moving.
- [ ] Commit only the spec, plan, wheel asset, component, stylesheet, and test.
