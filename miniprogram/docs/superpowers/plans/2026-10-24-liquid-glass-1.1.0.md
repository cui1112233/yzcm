# Mini-program Liquid Glass 1.1.0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a locally verifiable `1.1.0` mini-program build with smooth edge-focused liquid refraction on public pages and an immutable build identity, while preserving login animation and business behavior.

**Architecture:** Keep the recovered `1.0.11` PNG wallpapers as both the visible background and the refraction sample source. `ThemedPage` owns one `DynamicGlassCanvas` per liquid public page; the canvas reuses the latest valid frame, coalesces scroll updates through the existing scheduler, and limits work to four visible surfaces at 1.25 backing scale. A release-build wrapper injects Git identity into Taro constants and writes a matching `dist/release-manifest.json`.

**Tech Stack:** Taro 4.2.1, React 18, TypeScript 5.7, WeChat Canvas 2D, Vitest 3, Node.js build scripts.

## Global Constraints

- Version is exactly `1.1.0`; material code is exactly `liquid-smooth-a`.
- `1.0.11` remains a reference baseline and is not overwritten or renamed.
- Liquid material applies only to workspace, tasks, functions, profile, and profile-detail public pages.
- Login animation, wheel assets, account behavior, business pages, APIs, and routes must not change.
- Public refraction samples `src/assets/glass/wallpaper-light.png` and `wallpaper-dark.png`, matching `scaleToFill` visible geometry.
- Dynamic refraction processes at most four visible surfaces at backing scale `1.25`.
- Reduced transparency disables dynamic refraction and keeps readable static surfaces.
- No upload, experience-version replacement, publication, or Git tag creation before visual acceptance.

---

### Task 1: Immutable 1.1.0 build identity

**Files:**
- Create: `scripts/release-identity.mjs`
- Create: `scripts/release-identity.test.ts`
- Modify: `src/config/release.ts`
- Modify: `src/components/LoginScene.tsx`
- Modify: `src/pages/profile/index.tsx`
- Modify: `config/index.ts`
- Modify: `package.json`

**Interfaces:**
- Produces: `prepareReleaseIdentity({ cwd, version, material }) -> ReleaseIdentity` for tests and build use.
- Produces: compile constants `__MINIPROGRAM_BUILD_SHA__` and `__MINIPROGRAM_BUILD_DIRTY__`.
- Produces: `MINIPROGRAM_VERSION`, `MINIPROGRAM_BUILD_SHORT`, `MINIPROGRAM_MATERIAL`, and `MINIPROGRAM_VERSION_LABEL`.
- Produces: `dist/release-manifest.json` with `version`, `material`, `commit`, `shortCommit`, `dirty`, `builtAt`, and both wallpaper SHA-256 values.

- [ ] **Step 1: Write failing release identity tests**

Test a temporary Git repository and assert that `prepareReleaseIdentity` returns the full SHA, 7-character short SHA, dirty state, and the exact version/material. Assert that a non-Git directory throws instead of returning a fake identity.

```ts
expect(identity).toMatchObject({
  version: '1.1.0',
  material: 'liquid-smooth-a',
  shortCommit: expect.stringMatching(/^[0-9a-f]{7}$/),
  dirty: false,
});
expect(() => prepareReleaseIdentity({ cwd: nonGit, version: '1.1.0', material: 'liquid-smooth-a' })).toThrow(/Git/);
```

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `npx vitest run scripts/release-identity.test.ts`

Expected: FAIL because `scripts/release-identity.mjs` does not exist.

- [ ] **Step 3: Implement release preparation and manifest writing**

Use `execFileSync('git', ['rev-parse', 'HEAD'])` and `git status --porcelain`; never invoke a shell string. Pass the identity through child-process environment variables and `defineConstants`, then write the manifest after `dist` exists. Hash the exact PNG wallpaper files with `createHash('sha256')`.

Update the production build script to:

```json
"build:weapp": "node scripts/release-identity.mjs build production"
```

The build wrapper must inject the SHA through child-process environment variables consumed by `config/index.ts`; it must not generate a tracked source file containing the SHA because a commit cannot contain its own final hash. It then runs Taro, runs `scripts/sanitize-weapp-wxss.mjs`, and writes the manifest. It must preserve child exit codes.

- [ ] **Step 4: Display an auditable version label**

Render `MINIPROGRAM_VERSION_LABEL` as `v1.1.0 · abc1234` on the login page and profile diagnostic row. Add tests that reject a label containing only `1.1.0`.

- [ ] **Step 5: Run focused tests and build**

Run: `npx vitest run scripts/release-identity.test.ts src/pages/profile/index.test.tsx src/components/LoginScene.test.tsx`

Run: `npm run build:weapp`

Expected: tests pass; `dist/release-manifest.json` exists and its version, material and SHA match the generated source.

- [ ] **Step 6: Commit Task 1**

```bash
git add scripts/release-identity.mjs scripts/release-identity.test.ts src/config/release.ts src/components/LoginScene.tsx src/pages/profile/index.tsx config/index.ts package.json
git commit -m "build: identify miniprogram 1.1.0 artifacts"
```

### Task 2: Mount one dynamic optical layer on public pages

**Files:**
- Modify: `src/theme.tsx`
- Modify: `src/ui/LiquidGlassMaterial.tsx`
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`
- Modify: `src/theme.test.ts`
- Test: `src/ui/liquidGlassPages.test.ts`

**Interfaces:**
- Consumes: `liquidEnabled(className)` and `liquidPageKey(className)`.
- Produces: one mounted `DynamicGlassCanvas({ theme, reduced, pageKey })` inside every liquid page root.
- Produces: PNG wallpaper sampling with the same `scaleToFill` crop as `LiquidGlassMaterial`.

- [ ] **Step 1: Write failing mounting and source-parity tests**

Assert that `ThemedPage` mounts `DynamicGlassCanvas` only when `liquidEnabled` is true, passes `liquidPageKey(className)`, and omits it when reduced transparency is enabled. Update the canvas source test to require PNG imports and reject WebP imports.

```ts
expect(themeSource).toContain('<DynamicGlassCanvas theme={baseTheme} reduced={reducedGlass} pageKey={liquidPageKey(className)} />');
expect(canvasSource).toContain("wallpaper-dark.png");
expect(canvasSource).not.toContain("wallpaper-dark.webp");
```

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `npx vitest run src/theme.test.ts src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlassPages.test.ts`

Expected: FAIL because the dynamic canvas is not mounted and still imports WebP wallpapers.

- [ ] **Step 3: Implement the single-owner material stack**

Import `DynamicGlassCanvas` and `liquidPageKey` in `theme.tsx`. Render the PNG wallpaper first, dynamic canvas second, and page content last. Do not mount the canvas in reduced mode. Repeat the same ownership rules inside the theme-reveal preview so the target theme does not flash to a non-liquid frame.

- [ ] **Step 4: Use the visible PNG wallpaper as the sample source**

Change `DynamicGlassCanvas` imports to the recovered PNG files. Remove `aspectFillSourceRect` from this path: `scaleToFill` stretches the complete image to the viewport, so the offscreen canvas must call `drawImage(wallpaperImage, 0, 0, sourceWidth, sourceHeight, 0, 0, pixelWidth, pixelHeight)`. Add a geometry assertion that the complete source is mapped to the complete target without cropping or a second generated background.

- [ ] **Step 5: Run focused tests**

Run: `npx vitest run src/theme.test.ts src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlassPages.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add src/theme.tsx src/ui/LiquidGlassMaterial.tsx src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx src/theme.test.ts src/ui/liquidGlassPages.test.ts
git commit -m "feat: mount public liquid refraction layer"
```

### Task 3: Smooth four-surface frame scheduling

**Files:**
- Modify: `src/ui/glassRefraction.ts`
- Modify: `src/ui/glassRefraction.test.ts`
- Modify: `src/ui/DynamicGlassCanvas.tsx`
- Modify: `src/ui/DynamicGlassCanvas.test.tsx`
- Modify: `src/ui/glassScrollBridge.ts`
- Modify: `src/ui/glassScrollBridge.test.ts`

**Interfaces:**
- Produces: `glassBackingScale(pixelRatio) -> number` capped at `1.25`.
- Produces: `visibleRefractionRects(rects, viewportHeight, limit = 4)`.
- Produces: a latest-value scheduler that never clears the current frame until a replacement frame is ready.

- [ ] **Step 1: Write failing performance-contract tests**

Assert exact backing scale and surface limits:

```ts
expect(glassBackingScale(2)).toBe(1.25);
expect(visibleRefractionRects(twelveRects, 700)).toHaveLength(4);
```

Add a scheduler test that sends scroll positions `20`, `40`, and `80` before one animation callback and expects a single draw of `80`. Add a lifecycle test that stops a queued frame and proves the callback cannot draw afterward.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `npx vitest run src/ui/glassRefraction.test.ts src/ui/glassScrollBridge.test.ts src/ui/DynamicGlassCanvas.test.tsx`

Expected: FAIL on the old `1.5` backing scale and six/eight-surface limits.

- [ ] **Step 3: Implement the bounded scheduler contract**

Cap backing scale at `1.25`, make four the default visibility limit, and call it without a larger override from `DynamicGlassCanvas`. Preserve the current canvas pixels while a new frame is queued; only clear immediately before `putImageData` for the completed replacement frame.

- [ ] **Step 4: Guard lifecycle and late frames**

Tie every draw to the existing generation token. Page hide, resize, theme change, layout invalidation and unmount must cancel the queued frame. A callback from an older generation must exit without clearing the canvas.

- [ ] **Step 5: Run focused tests**

Run: `npx vitest run src/ui/glassRefraction.test.ts src/ui/glassScrollBridge.test.ts src/ui/DynamicGlassCanvas.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

```bash
git add src/ui/glassRefraction.ts src/ui/glassRefraction.test.ts src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx src/ui/glassScrollBridge.ts src/ui/glassScrollBridge.test.ts
git commit -m "perf: keep liquid refraction smooth while scrolling"
```

### Task 4: Clean Apple-like surface styling and reduced transparency

**Files:**
- Modify: `src/ui/liquidGlass.scss`
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/pages/functions/functions-glass.scss`
- Modify: `src/pages/functions/glassOptics.ts`
- Modify: `src/pages/functions/glassOptics.test.ts`
- Test: `src/pages/profile/profile-palette.test.tsx`

**Interfaces:**
- Produces: shared primary, secondary and list surface tokens with transparent centers and edge-focused highlights.
- Produces: reduced mode with static opaque surfaces and no dynamic canvas.

- [ ] **Step 1: Write failing material tests**

Assert that shared styles define separate light/dark center fill, inner rim, edge highlight and short shadow variables. Reject full-card white masks, large blur filters and page-specific duplicated edge pseudo-elements. Assert reduced mode hides `.dynamic-glass-layer` and uses an opaque surface token.

- [ ] **Step 2: Run focused style tests and confirm failure**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/pages/functions/glassOptics.test.ts src/pages/profile/profile-palette.test.tsx`

Expected: FAIL until the new token contract is present.

- [ ] **Step 3: Implement restrained primary, secondary and list surfaces**

Use theme-specific variables for center tint, one-pixel inner edge, directional highlight and short shadow. Keep the center alpha below the edge alpha. Remove duplicate feature-page edge layers so only the shared material owns the glass boundary.

- [ ] **Step 4: Preserve readable text and reduced mode**

Keep text colors opaque enough for both themes. In `.liquid-reduced`, hide dynamic optics, replace transparent surfaces with the theme solid surface, remove displacement-related transitions and retain borders that distinguish controls.

- [ ] **Step 5: Run focused tests**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/pages/functions/glassOptics.test.ts src/pages/profile/profile-palette.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add src/ui/liquidGlass.scss src/ui/liquidGlass.test.ts src/pages/functions/functions-glass.scss src/pages/functions/glassOptics.ts src/pages/functions/glassOptics.test.ts src/pages/profile/profile-palette.test.tsx
git commit -m "style: refine public liquid glass surfaces"
```

### Task 5: Theme transition and regression verification

**Files:**
- Modify: `src/theme.tsx`
- Modify: `src/ui/themeReveal.scss`
- Modify: `src/ui/themeReveal.test.ts`
- Modify: `src/components/WorkspaceHomeHeader.tsx`
- Test: `src/components/LoginScene.test.tsx`
- Test: `src/components/loginEntryMotion.test.ts`

**Interfaces:**
- Consumes: the single-owner material stack from Task 2.
- Produces: a two-way circular theme reveal whose preview uses the requested target theme and retains the old frame until target material is ready.

- [ ] **Step 1: Write failing transition tests**

Assert that preview theme is derived from `reveal.next`, not hard-coded to dark. Verify light-to-dark and dark-to-light clip radii, first-frame readiness, and fallback completion. Add source/hash guards proving `LoginScene`, login motion and wheel assets were not altered by Tasks 1-4 except the version label line.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `npx vitest run src/ui/themeReveal.test.ts src/theme.test.ts src/components/LoginScene.test.tsx src/components/loginEntryMotion.test.ts`

Expected: the bidirectional preview test fails while `previewValue.theme` is hard-coded to dark.

- [ ] **Step 3: Implement target-theme preview**

Set the preview provider, page class, variables, wallpaper and dynamic layer from `reveal.next`. Keep the old base theme unchanged until transition completion. Ensure cleanup removes timers and queued optical work.

- [ ] **Step 4: Run focused tests**

Run: `npx vitest run src/ui/themeReveal.test.ts src/theme.test.ts src/components/LoginScene.test.tsx src/components/loginEntryMotion.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit Task 5**

```bash
git add src/theme.tsx src/ui/themeReveal.scss src/ui/themeReveal.test.ts src/components/WorkspaceHomeHeader.tsx src/components/LoginScene.test.tsx src/components/loginEntryMotion.test.ts
git commit -m "fix: keep liquid theme transitions continuous"
```

### Task 6: Full build and WeChat visual acceptance

**Files:**
- Verify: `dist/`
- Verify: `dist/release-manifest.json`
- Verify: `src/assets/login/`
- Verify: `src/assets/glass/`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: a locally accepted, upload-ready `1.1.0` artifact. Upload remains a separate user-confirmed operation.

- [ ] **Step 1: Run full automated verification**

Run:

```bash
npm test -- --run
npx tsc --noEmit
npm run build:weapp
git diff --check
```

Expected: all tests and build pass. If TypeScript still reports a pre-existing login component typing problem, record the exact errors and do not alter animation behavior merely to silence it.

- [ ] **Step 2: Verify artifact identity**

Compare `dist/release-manifest.json`, compiled release constants, current Git SHA and PNG wallpaper hashes. Confirm the login page bundle contains `v1.1.0 · <short SHA>` and public bundles mount exactly one dynamic canvas.

- [ ] **Step 3: Compile in the correct WeChat project**

Confirm the developer-tool project URL contains:

```text
projectpath=/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist
appid=wxd72c311f3711eda9
```

Click compile and verify the simulator loads the newly built `dist`.

- [ ] **Step 4: Inspect both themes on all public pages**

Check workspace, tasks, functions, profile and profile-detail in light and dark themes. Scroll continuously, stop at several offsets, switch tabs while scrolling, press cards, and verify that refraction remains visible without delayed popping, text overlap or blocked clicks.

- [ ] **Step 5: Verify boundaries**

Confirm the workspace hero remains complete, reduced transparency disables dynamic optics, business pages use ordinary material, and the login opening animation plus wheel remain unchanged.

- [ ] **Step 6: Stop before upload**

Report screenshots and verification evidence. Ask for explicit approval immediately before uploading or replacing the experience version. Create `miniprogram-v1.1.0` only after implementation commits are final and visual acceptance has passed.
