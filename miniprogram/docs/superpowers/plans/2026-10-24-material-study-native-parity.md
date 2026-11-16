# Material Study Native Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the public mini-program liquid background and optics with the approved `material-study.html` implementation while preserving the workspace hero and ordinary business pages.

**Architecture:** Generate light and dark WebP wallpaper assets from the exact Material Study canvas constants, then use those same assets in both `LiquidGlassMaterial` and `DynamicGlassCanvas`. Keep refraction bounded to visible card rims, retain the previous canvas frame while a new theme source loads, and use the already-restored complete wallpaper in the circular theme preview.

**Tech Stack:** Taro 4.2.1, React 18, TypeScript, WeChat Canvas 2D, Sharp, Vitest, SCSS.

## Global Constraints

- Preserve `home-art-glass.png` and `home-art-glass-dark.png`; no liquid selector may hide `.home-hero-art`.
- Public pages use liquid material; screenplay, fetch, panel, Shuihuo, and Batch Factory workbenches remain ordinary.
- Keep backing scale capped at 1.5 and process no more than 6 visible refraction surfaces.
- Keep “降低透明度” under Profile > 服务.
- Do not upload or publish a mini-program version.
- The worktree contains unrelated changes; stage or commit no implementation files automatically.

---

### Task 1: Exact Material Study Wallpaper Assets

**Files:**
- Modify: `scripts/prepare-glass-wallpaper.mjs`
- Create: `scripts/prepare-glass-wallpaper.test.ts`
- Create: `src/assets/glass/wallpaper-light.webp`
- Create: `src/assets/glass/wallpaper-dark.webp`

**Interfaces:**
- Produces: `materialStudySvg(dark: boolean): string` and two 780 x 1500 WebP assets.
- Consumes: Exact gradient, four ellipse, rotation, offset, and radial glow constants from `previews/liquid-glass/material-study.js`.

- [ ] **Step 1: Write the failing generator contract test**

Assert that the exported SVG contains the three exact gradient stops, four exact curve colors per theme, four ellipses, `rotate(-21.772396)`, and a 780 x 1500 view box.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `npm test -- --run scripts/prepare-glass-wallpaper.test.ts`

Expected: FAIL because the current generator uses 390 x 844, three approximate curves, and no exported generator.

- [ ] **Step 3: Implement the exact generator and write WebP assets**

Export `materialStudySvg`; guard the file-writing entry point so tests can import it without changing assets. Generate lossless or visually equivalent high-quality WebP files from the exact SVG.

- [ ] **Step 4: Generate and inspect assets**

Run: `node scripts/prepare-glass-wallpaper.mjs`

Run: `file src/assets/glass/wallpaper-light.webp src/assets/glass/wallpaper-dark.webp`

Expected: both report 780 x 1500 WebP images.

- [ ] **Step 5: Run the focused test and verify GREEN**

Run: `npm test -- --run scripts/prepare-glass-wallpaper.test.ts`

Expected: PASS.

### Task 2: Use One Background Source and Restore Reference Optics

**Files:**
- Modify: `src/ui/LiquidGlassMaterial.tsx`
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`
- Modify: `src/ui/liquidGlass.scss`
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/ui/liquidGlassPages.test.ts`

**Interfaces:**
- Consumes: `wallpaper-light.webp` and `wallpaper-dark.webp` from Task 1.
- Produces: the same displayed and sampled wallpaper with primary optics radius/rim/bend `30/13/12.5` and secondary `24/8/6`.

- [ ] **Step 1: Write failing same-source and visual-token tests**

Assert that both material components import WebP assets, that the deprecated PNG imports are absent, that the main-card CSS uses reference edge/highlight values, and that no liquid workspace rule hides `.home-hero-art`.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlass.test.ts src/ui/liquidGlassPages.test.ts`

Expected: FAIL on PNG imports and reduced edge/highlight values.

- [ ] **Step 3: Switch both renderers to WebP and align CSS optics**

Update both imports together. Restore the Material Study primary shadow, outer sheen, inner edge, and press scale without changing page routing or business content.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlass.test.ts src/ui/liquidGlassPages.test.ts src/ui/glassRefraction.test.ts`

Expected: PASS.

### Task 3: Preserve Refraction During Theme Changes

**Files:**
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`
- Verify: `src/theme.tsx`
- Verify: `src/ui/themeReveal.scss`
- Verify: `src/ui/themeReveal.test.ts`

**Interfaces:**
- Consumes: the same-source WebP assets and circular reveal state.
- Produces: old canvas pixels remain visible until the new wallpaper image loads and the replacement frame is drawn synchronously.

- [ ] **Step 1: Write a failing frame-retention test**

Assert that visible canvas width/height reset and clear operations occur only after `wallpaperImage.onload`, not before the replacement pixels exist.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/themeReveal.test.ts src/theme.test.ts`

Expected: FAIL because the current initializer clears the visible canvas before image loading completes.

- [ ] **Step 3: Stage new pixels before swapping the visible frame**

Keep the previous renderer and visible bitmap while the offscreen source loads. Inside `onload`, resize only when necessary, assign the new renderer, create the scheduler, and immediately draw the first new frame.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/themeReveal.test.ts src/theme.test.ts`

Expected: PASS.

### Task 4: Build and WeChat Visual Acceptance

**Files:**
- Verify: `dist/**`
- Verify: source files changed in Tasks 1-3

**Interfaces:**
- Consumes: completed background, optics, and transition work.
- Produces: a rebuilt local `dist` accepted in WeChat Developer Tools.

- [ ] **Step 1: Run all automated tests**

Run: `npm test -- --run`

Expected: all test files pass with zero failures.

- [ ] **Step 2: Build the WeChat output**

Run: `npm run build:weapp`

Expected: exit 0; only the existing asset/performance warnings are acceptable.

- [ ] **Step 3: Inspect exact changes**

Run: `git diff --check`

Run: `git diff -- scripts/prepare-glass-wallpaper.mjs scripts/prepare-glass-wallpaper.test.ts src/ui/LiquidGlassMaterial.tsx src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlass.scss src/ui/liquidGlass.test.ts src/ui/liquidGlassPages.test.ts src/theme.tsx src/ui/themeReveal.scss src/ui/themeReveal.test.ts src/components/WorkspaceHomeHeader.scss`

Expected: no whitespace errors and no unrelated edits introduced by this task.

- [ ] **Step 4: Verify in WeChat Developer Tools**

Confirm workspace hero visibility, the Material Study curves, card refraction, scrolling, clickability, light-to-dark and dark-to-light transition, and the ordinary material boundary on at least one business workbench.

- [ ] **Step 5: Record actual evidence**

Report test totals, build result, inspected pages, and any residual simulator-only warning. Do not claim visual completion if either theme direction or the business-page boundary was not observed.
