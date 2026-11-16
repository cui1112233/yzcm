# Reference Liquid Glass Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reproduce the approved `material-study.html` glass card language on the mini-program public pages without applying noisy refractive outlines to business workbenches.

**Architecture:** Keep the wallpaper in an ordinary bottom image layer and use one transparent viewport Canvas only for card-local optical pixels. Replace scanline edge copying with a rounded signed-distance sampler, and bind glass through explicit `liquid-glass--primary`, `liquid-glass--secondary`, and `liquid-glass--list` roles instead of global component selectors.

**Tech Stack:** Taro 4, React, TypeScript, SCSS/WXSS, WeChat Canvas 2D, Vitest, WeChat Developer Tools.

## Global Constraints

- The exact visual reference is the “现在 / 清透中心 · 弧面折射” card at `http://127.0.0.1:61977/material-study.html`.
- Public outer pages are workspace, tasks, functions, profile, and profile detail.
- Script, fetch, batch factory, Shuihuo, and other business workbenches retain ordinary high-readability material.
- The visible Canvas stays transparent outside marked glass regions and never redraws the full wallpaper.
- Reduced-transparency mode disables realtime refraction and uses high-contrast solid surfaces.
- Build or unit-test success is not visual acceptance; WeChat Developer Tools screenshots are required.
- Real-device smoothness remains pending until tested on a device.

---

### Task 1: Rounded optical sampling kernel

**Files:**
- Modify: `src/ui/glassRefraction.ts`
- Modify: `src/ui/glassRefraction.test.ts`

**Interfaces:**
- Produces: `roundedRectSample(x, y, width, height, radius, rim): { inside: boolean; depth: number; normalX: number; normalY: number; bend: number; highlight: number }`.
- Produces: `drawBackgroundRefraction(ctx, source, rect, options): void` with `radius`, `rim`, `bend`, `reduced`, and `role` options.

- [ ] **Step 1: Write failing optical-kernel tests**

Add assertions showing that the center has zero bend, a left-edge sample bends inward by approximately 25px, a rounded-corner sample has a diagonal normal, and a point outside the rounded rectangle is excluded:

```ts
expect(roundedRectSample(160, 50, 320, 100, 30, 26).bend).toBe(0);
expect(roundedRectSample(3, 50, 320, 100, 30, 26).bend).toBeGreaterThan(20);
expect(Math.abs(roundedRectSample(8, 8, 320, 100, 30, 26).normalX)).toBeGreaterThan(.3);
expect(roundedRectSample(0, 0, 320, 100, 30, 26).inside).toBe(false);
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `npm test -- --run src/ui/glassRefraction.test.ts`

Expected: FAIL because `roundedRectSample` and the wider optical options do not exist.

- [ ] **Step 3: Implement the signed-distance sampler and pixel compositor**

Use the reference formula with a 20-26px rim and quadratic displacement:

```ts
const depth = -signedDistance;
const bend = depth >= 0 && depth < rim
  ? Math.pow(1 - depth / rim, 2) * maxBend
  : 0;
```

Render the refracted source into a clipped temporary image buffer for each card. Keep the center at the same wallpaper coordinate, add only a narrow edge highlight, and omit the unconditional `1px` white stroke.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `npm test -- --run src/ui/glassRefraction.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit the optical kernel**

```bash
git add src/ui/glassRefraction.ts src/ui/glassRefraction.test.ts
git commit -m "feat: match reference glass refraction kernel"
```

### Task 2: Explicit material roles and selectors

**Files:**
- Modify: `src/ui/liquidGlass.ts`
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/ui/liquidGlass.scss`

**Interfaces:**
- Produces: `glassSelectors` containing only `.liquid-glass--primary` and `.liquid-glass--secondary`.
- Produces: `.liquid-glass--primary`, `.liquid-glass--secondary`, and `.liquid-glass--list` material classes.
- Consumes: Task 1 optical roles through DOM class selection.

- [ ] **Step 1: Write failing selector and style-contract tests**

Assert that `.mp-card` and `.tools-entry` are absent from `glassSelectors`, and that the SCSS contains the three explicit roles while the old blanket selector block is absent:

```ts
expect(glassSelectors).toEqual(['.liquid-glass--primary', '.liquid-glass--secondary']);
expect(source).toContain('.liquid-glass--primary');
expect(source).toContain('.liquid-glass--list');
expect(source).not.toMatch(/\.mp-card,\.tools-entry/);
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `npm test -- --run src/ui/liquidGlass.test.ts`

Expected: FAIL because the selector registry still targets generic page components.

- [ ] **Step 3: Implement the three material roles**

Primary material uses a transparent center, reference-like `28-30px` radius, soft outer shadow, top/left inner highlight, bottom/right inner shade, and a localized copy halo. Secondary material lowers highlight/refraction intensity. List material applies one glass container around rows and uses separators instead of per-row rims.

Delete only the liquid-theme blanket overrides for generic cards. Preserve semantic status colors, form reading surfaces, reduced-transparency mode, and navigation behavior.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run: `npm test -- --run src/ui/liquidGlass.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit material roles**

```bash
git add src/ui/liquidGlass.ts src/ui/liquidGlass.test.ts src/ui/liquidGlass.scss
git commit -m "feat: scope liquid glass to material roles"
```

### Task 3: Canvas role-aware drawing and restrained wallpaper

**Files:**
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`
- Modify: `src/ui/glassWallpaper.ts`
- Modify: `src/ui/LiquidGlassMaterial.tsx`
- Modify: `src/ui/LiquidGlassMaterial.test.tsx`

**Interfaces:**
- Consumes: Task 1 `drawBackgroundRefraction` and Task 2 role selectors.
- Produces: one transparent Canvas that distinguishes primary and secondary card intensity.
- Produces: wallpaper geometry with a quiet upper field and the bright arc concentrated in the lower-right region.

- [ ] **Step 1: Write failing renderer contracts**

Assert that the renderer asks for `dataset.glassRole`, limits visible optical surfaces to six, passes `rim: 26` and `bend: 25` for primary cards, and never calls `drawImage(source, 0, 0, width, height)` on the visible Canvas. Assert that the wallpaper arc center is below the vertical midpoint.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/LiquidGlassMaterial.test.tsx`

Expected: FAIL because rectangles do not carry roles and the wallpaper is dominated by a viewport-sized arc.

- [ ] **Step 3: Implement role-aware measurements and drawing**

Read each marked node's `dataset.glassRole`, store it with the document rectangle, and draw primary/secondary options separately. Keep the viewport Canvas transparent between frames. Move wallpaper ellipses down and right, reduce bright-band width and opacity, and keep the upper 35% visually quiet.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `npm test -- --run src/ui/DynamicGlassCanvas.test.tsx src/ui/LiquidGlassMaterial.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit the renderer and wallpaper**

```bash
git add src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx src/ui/glassWallpaper.ts src/ui/LiquidGlassMaterial.tsx src/ui/LiquidGlassMaterial.test.tsx
git commit -m "feat: render restrained role-aware glass"
```

### Task 4: Apply material roles to public pages

**Files:**
- Modify: `src/pages/functions/index.tsx`
- Modify: `src/pages/functions/functions-glass.scss`
- Modify: `src/pages/functions/functions.interaction.test.tsx`
- Modify: `src/pages/tasks/index.tsx`
- Modify: `src/pages/tasks/index.test.tsx`
- Modify: `src/pages/workspace/index.tsx`
- Modify: `src/components/WorkspaceHomeHeader.tsx`
- Modify: `src/components/WorkspaceHomeHeader.scss`
- Modify: `src/components/WorkspaceHomeHeader.test.tsx`
- Modify: `src/pages/profile/index.tsx`
- Modify: `src/pages/profile/index.test.tsx`
- Modify: `src/pages/profile-detail/index.tsx`
- Modify: `src/pages/profile-detail/index.test.tsx`

**Interfaces:**
- Consumes: Task 2 material role class names.
- Produces: page markup with explicit `data-glass-role="primary|secondary"` markers.

- [ ] **Step 1: Write failing page-role and layout tests**

Assert that the script-generation entry is primary, function entry groups use secondary cards, the task list has one `liquid-glass--list` container, profile rows do not each use a glass role, and the workspace status card no longer combines a negative top margin with sticky positioning.

- [ ] **Step 2: Run focused page tests and verify RED**

Run: `npm test -- --run src/pages/functions/functions.interaction.test.tsx src/pages/tasks/index.test.tsx src/components/WorkspaceHomeHeader.test.tsx src/pages/profile/index.test.tsx src/pages/profile-detail/index.test.tsx`

Expected: FAIL because pages still depend on generic glass selectors and the workspace header overlaps.

- [ ] **Step 3: Apply roles and repair page hierarchy**

Functions: make “剧本生成” the exact first reference sample and share secondary material with other entries. Tasks: wrap the activity feed once and keep rows separated by spacing/dividers. Workspace: place the status card in normal flow below the hero with fixed reserved height; keep companion, time, and greeting in non-overlapping tracks. Profile: apply glass to account hero, usage, and grouped sections only. Profile detail: keep summary glass and stable form surfaces.

- [ ] **Step 4: Run focused page tests and verify GREEN**

Run: `npm test -- --run src/pages/functions/functions.interaction.test.tsx src/pages/tasks/index.test.tsx src/components/WorkspaceHomeHeader.test.tsx src/pages/profile/index.test.tsx src/pages/profile-detail/index.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit public-page material mapping**

```bash
git add src/pages/functions/index.tsx src/pages/functions/functions-glass.scss src/pages/functions/functions.interaction.test.tsx src/pages/tasks/index.tsx src/pages/tasks/index.test.tsx src/pages/workspace/index.tsx src/components/WorkspaceHomeHeader.tsx src/components/WorkspaceHomeHeader.scss src/components/WorkspaceHomeHeader.test.tsx src/pages/profile/index.tsx src/pages/profile/index.test.tsx src/pages/profile-detail/index.tsx src/pages/profile-detail/index.test.tsx
git commit -m "feat: apply reference glass to public pages"
```

### Task 5: Regression build and visual acceptance

**Files:**
- Modify only if a failing regression identifies a scoped defect in Task 1-4 files.

**Interfaces:**
- Consumes: completed public-page glass implementation.
- Produces: rebuilt `dist/` and visual evidence from WeChat Developer Tools.

- [ ] **Step 1: Run the full automated suite**

Run: `npm test -- --run`

Expected: all tests pass with no new unhandled errors.

- [ ] **Step 2: Run type and build verification**

Run: `npx tsc --noEmit`

Run: `npm run build:weapp`

Expected: both commands exit `0`; `dist/` contains the current bundle.

- [ ] **Step 3: Check patch hygiene**

Run: `git diff --check`

Expected: no whitespace errors.

- [ ] **Step 4: Inspect five public pages in WeChat Developer Tools**

Compile `dist/`, then capture workspace, tasks, functions, profile, and profile-detail. Verify the exact criteria from the design specification: reference-like function card, restrained background, no blanket white rims, no workspace overlap, readable text, working tab switches, and scroll alignment.

- [ ] **Step 5: Check reduced transparency**

Toggle “降低透明度” under profile services and revisit all public tabs. Verify realtime optical pixels disappear, solid contrast surfaces appear, and interaction remains intact.

- [ ] **Step 6: Record the remaining real-device boundary**

Report Developer Tools results separately from unverified real-device frame smoothness. Do not upload or publish unless separately authorized.

