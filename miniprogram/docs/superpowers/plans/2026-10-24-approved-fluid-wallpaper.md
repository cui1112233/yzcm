# Approved Fluid Wallpaper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the generated screensaver-like public-page wallpaper with the user-approved blue fluid image while keeping the visible wallpaper and Canvas refraction pixel-aligned in light and dark themes.

**Architecture:** Store the approved upload as a checked-in master asset, derive fixed light and dark runtime PNGs from that master, and share one pure aspect-fill geometry calculation between the visible Taro image contract and the offscreen Canvas draw. The visible image uses `aspectFill`; the Canvas uses the matching source rectangle rather than stretching the bitmap.

**Tech Stack:** Taro 4, React, TypeScript, Vitest, WeChat Canvas 2D, Swift/AppKit asset preparation.

## Global Constraints

- The approved uploaded image is the only composition source; do not redraw its shape with gradients or ellipses.
- Light theme preserves the source colors; dark theme only lowers brightness without moving the light seam.
- The visible wallpaper and offscreen refraction source must use the same asset, aspect-fill rule, center position, and viewport coordinates.
- Do not alter login visuals, business workbench pages, glass roles, or the rounded-surface refraction kernel.
- Build `dist` with `npm run build:weapp`; do not upload or publish.
- Developer Tools visual inspection is required; build and tests alone are insufficient.

---

### Task 1: Reproducible Approved Wallpaper Assets

**Files:**
- Create: `src/assets/glass/wallpaper-master.png`
- Modify: `scripts/render-liquid-glass-assets.swift`
- Modify: `src/assets/glass/wallpaper-light.png`
- Modify: `src/assets/glass/wallpaper-dark.png`
- Test: `src/ui/LiquidGlassMaterial.test.tsx`

**Interfaces:**
- Consumes: user-approved `658x1449` PNG.
- Produces: one immutable master plus `390x844` light and dark runtime PNGs with identical composition.

- [ ] **Step 1: Write the failing asset contract test**

Require `wallpaper-master.png`, assert the runtime wallpapers exist, assert all three PNG headers report the intended dimensions, and assert the light and dark files differ while both retain non-empty pixel payloads.

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `npm test -- --run src/ui/LiquidGlassMaterial.test.tsx`

Expected: FAIL because `wallpaper-master.png` does not exist and the generator still redraws the obsolete arc wallpaper.

- [ ] **Step 3: Store and derive the approved assets**

Copy the approved upload to `wallpaper-master.png`. Change the Swift tool to accept `<master-image> <output-directory>`, render the light image by centered aspect-fill crop, render the dark image from the same crop with a uniform dark overlay, and retain the existing lens generation only for legacy consumers. Remove the gradient/ellipse wallpaper renderer.

- [ ] **Step 4: Run the generator and focused test**

Run:

```bash
swift scripts/render-liquid-glass-assets.swift src/assets/glass/wallpaper-master.png src/assets/glass
npm test -- --run src/ui/LiquidGlassMaterial.test.tsx
```

Expected: PASS; light and dark are `390x844`, composition-aligned, and not byte-identical.

- [ ] **Step 5: Commit only the wallpaper asset unit**

```bash
git add scripts/render-liquid-glass-assets.swift src/assets/glass/wallpaper-master.png src/assets/glass/wallpaper-light.png src/assets/glass/wallpaper-dark.png src/ui/LiquidGlassMaterial.test.tsx
git commit -m "feat: adopt approved fluid wallpaper"
```

### Task 2: Shared Aspect-Fill Geometry

**Files:**
- Create: `src/ui/wallpaperGeometry.ts`
- Create: `src/ui/wallpaperGeometry.test.ts`
- Modify: `src/ui/LiquidGlassMaterial.tsx`
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`

**Interfaces:**
- Produces: `aspectFillSourceRect(sourceWidth, sourceHeight, targetWidth, targetHeight): { sx: number; sy: number; sw: number; sh: number }`.
- Consumes: the image's natural pixel dimensions and the current viewport dimensions.

- [ ] **Step 1: Write failing geometry and integration tests**

Test portrait-to-portrait, landscape target, invalid dimensions, and centered crop cases. Require `LiquidGlassMaterial` to use `mode="aspectFill"`. Require `DynamicGlassCanvas` to call `aspectFillSourceRect` and the nine-argument `drawImage` overload.

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
npm test -- --run src/ui/wallpaperGeometry.test.ts src/ui/LiquidGlassMaterial.test.tsx src/ui/DynamicGlassCanvas.test.tsx
```

Expected: FAIL because the helper does not exist and both render paths currently stretch the image.

- [ ] **Step 3: Implement the minimal centered aspect-fill path**

Calculate the source crop from aspect ratios, return a full-source rectangle when the ratios match, and return a zero rectangle for invalid dimensions. Render the visible image with Taro `aspectFill`; in `onload`, draw the same centered crop into the offscreen Canvas before reading pixels.

- [ ] **Step 4: Run focused and full tests**

Run:

```bash
npm test -- --run src/ui/wallpaperGeometry.test.ts src/ui/LiquidGlassMaterial.test.tsx src/ui/DynamicGlassCanvas.test.tsx src/ui/glassRefraction.test.ts
npm test -- --run
```

Expected: all relevant and full-suite tests pass, excluding only already-documented unrelated failures if the checkout baseline has changed.

- [ ] **Step 5: Commit the geometry unit**

```bash
git add src/ui/wallpaperGeometry.ts src/ui/wallpaperGeometry.test.ts src/ui/LiquidGlassMaterial.tsx src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx
git commit -m "fix: align wallpaper and refraction crops"
```

### Task 3: Build And Visual Acceptance

**Files:**
- Verify: `dist/assets/glass/wallpaper-light.png`
- Verify: `dist/assets/glass/wallpaper-dark.png`
- Verify: `dist/pages/**`

**Interfaces:**
- Consumes: completed asset and rendering tasks.
- Produces: a locally built `dist` and visual evidence from WeChat Developer Tools.

- [ ] **Step 1: Build the production mini-program**

Run: `npm run build:weapp`

Expected: build completes and WXSS sanitizer completes.

- [ ] **Step 2: Verify source-to-dist asset identity and diff hygiene**

Run:

```bash
shasum -a 256 src/assets/glass/wallpaper-light.png dist/assets/glass/wallpaper-light.png
shasum -a 256 src/assets/glass/wallpaper-dark.png dist/assets/glass/wallpaper-dark.png
git diff --check
```

Expected: each source asset matches its corresponding `dist` asset; no whitespace errors.

- [ ] **Step 3: Recompile and inspect in WeChat Developer Tools**

Inspect workspace, tasks, functions, profile, and profile-detail in both themes. Confirm the old arc wallpaper is absent, the approved warm seam remains at the same page position inside and outside glass edges, text is readable, controls remain clickable, and scrolling does not introduce crop jumps.

- [ ] **Step 4: Record the remaining boundary**

Report Developer Tools acceptance separately from real-device performance. Do not claim real-device smoothness or publication until those are actually tested and authorized.
