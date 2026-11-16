# Dynamic Background Refraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace repeated lens PNG overlays on the five public mini-program pages with one event-driven Canvas 2D renderer that samples the actual page wallpaper and bends it only at visible glass edges.

**Architecture:** A pure optics module owns wallpaper painting, visible-rectangle selection, and edge displacement. `DynamicGlassCanvas` owns one visible Canvas plus one offscreen source per page and exposes an event bridge for page scroll, internal `ScrollView` scroll, and layout invalidation; `ThemedPage` mounts it only for the existing liquid public-page scope. Native WXML content remains above the pointer-transparent Canvas, while unsupported devices use clean translucent CSS with no repeated texture.

**Tech Stack:** Taro 4.2.1, React 18, TypeScript 5.7, WeChat Canvas 2D, Vitest 3.2.

## Global Constraints

- Enable dynamic refraction only for workspace, tasks, functions, profile, profile-detail, and their public navigation.
- Do not change login, login animation, wheel animation, or business inner pages.
- Keep the current WebView renderer; do not introduce WebGL or Skyline.
- Use one visible Canvas and one offscreen Canvas per page, with pixel ratio capped at `1.5`.
- Process at most eight visible glass rectangles per frame.
- Never call React state updates, selector queries, image export, file writes, or network requests in the frame loop.
- Keep refraction visible at rest and synchronized during scroll; never wait for scroll settlement before drawing.
- If Canvas is unavailable or too slow, use clean translucent fill plus a thin optical rim; never show `lens-light.png` or `lens-dark.png`.
- Do not claim completion without current `dist`, WeChat Developer Tools inspection of all five pages, and at least one real-device check.
- Do not upload or publish a new mini-program version without separate user authorization.

---

## File Structure

- `src/ui/glassRefraction.ts`: pure wallpaper, geometry, visibility, and edge-sampling functions.
- `src/ui/glassRefraction.test.ts`: deterministic unit tests for geometry, coordinate continuity, limits, and drawing calls.
- `src/ui/DynamicGlassCanvas.tsx`: native Canvas initialization, cached selector measurement, frame coalescing, lifecycle, and fallback notification.
- `src/ui/DynamicGlassCanvas.test.tsx`: component contract and lifecycle tests with mocked Taro Canvas/query APIs.
- `src/ui/glassScrollBridge.ts`: page-local event names and helpers for internal scroll/layout invalidation without React frame updates.
- `src/ui/glassScrollBridge.test.ts`: event payload and latest-scroll tests.
- `src/theme.tsx`: mounts one renderer for the existing liquid public-page scope.
- `src/pages/profile/index.tsx`: forwards internal `ScrollView` position and content-layout changes.
- `src/ui/liquidGlass.scss`: Canvas stacking and clean fallback; removes fixed lens pseudo-elements.
- `src/ui/TextureRefraction.tsx`, `src/ui/textureRefraction.scss`: remove fixed texture implementation after consumers migrate.
- Existing page tests and liquid-glass tests: assert scope isolation and absence of static lens assets.

---

### Task 1: Pure Refraction Geometry and Painter

**Files:**
- Create: `src/ui/glassRefraction.ts`
- Create: `src/ui/glassRefraction.test.ts`
- Modify: `src/pages/glass-lab/refraction2d.ts`
- Test: `src/pages/glass-lab/refraction2d.test.ts`

**Interfaces:**
- Consumes: Canvas 2D contexts and rectangles in CSS pixels.
- Produces: `GlassSurfaceRect`, `RefractionTheme`, `visibleRefractionRects(rects, viewportHeight, limit?)`, `paintGlassWallpaper(ctx, width, height, theme, offsetY?)`, and `drawBackgroundRefraction(ctx, source, rect, options)`.

- [ ] **Step 1: Write failing geometry and painter tests**

```ts
test('keeps the same wallpaper coordinate as scroll changes', () => {
  expect(sampleOrigin({ left: 20, top: 260, width: 300, height: 100 }, 120))
    .toEqual({ x: 20, y: 140 });
});

test('limits work to the first eight visible surfaces', () => {
  const rects = Array.from({ length: 12 }, (_, i) => ({ left: 10, top: i * 50, width: 300, height: 44 }));
  expect(visibleRefractionRects(rects, 700)).toHaveLength(8);
});

test('samples displaced scanlines only inside the optical rim', () => {
  const ctx = fakeContext();
  drawBackgroundRefraction(ctx, {}, { left: 20, top: 100, width: 300, height: 96 }, { radius: 28, rim: 10, reduced: false });
  expect(ctx.drawImage).toHaveBeenCalled();
  expect(ctx.clip).toHaveBeenCalledOnce();
});
```

- [ ] **Step 2: Run the focused tests and verify the missing-module failure**

Run: `npx vitest run src/ui/glassRefraction.test.ts src/pages/glass-lab/refraction2d.test.ts`

Expected: FAIL because `glassRefraction.ts` and its exports do not exist.

- [ ] **Step 3: Implement the pure optics module**

```ts
export type GlassSurfaceRect = { left: number; top: number; width: number; height: number };
export type RefractionTheme = 'light' | 'dark';

export function sampleOrigin(rect: GlassSurfaceRect, scrollTop: number) {
  return { x: rect.left, y: rect.top - scrollTop };
}

export function visibleRefractionRects(rects: GlassSurfaceRect[], viewportHeight: number, limit = 8) {
  return rects.filter(rect => rect.width >= 70 && rect.height >= 42 && rect.top < viewportHeight && rect.top + rect.height > 0).slice(0, limit);
}
```

Implement wallpaper gradients and large ribbons with the same geometry used by `material-study.js`. Implement the rounded-rectangle signed-distance edge band with a 6–12px rim, outward-to-inward quadratic displacement, subtle magnification, and a thin highlight. The center must sample the same underlying wallpaper coordinate and must never contain WXML text.

- [ ] **Step 4: Make the glass-lab wrapper delegate to the shared painter**

Keep `roundedRect` and `drawLens` exports temporarily for the laboratory test, but delegate their work to `drawBackgroundRefraction` so the proof page and production renderer cannot drift.

- [ ] **Step 5: Run the focused tests**

Run: `npx vitest run src/ui/glassRefraction.test.ts src/pages/glass-lab/refraction2d.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit the pure painter**

```bash
git add src/ui/glassRefraction.ts src/ui/glassRefraction.test.ts src/pages/glass-lab/refraction2d.ts src/pages/glass-lab/refraction2d.test.ts
git commit -m "feat: add shared background refraction painter"
```

---

### Task 2: Event-Driven Single-Canvas Renderer

**Files:**
- Create: `src/ui/glassScrollBridge.ts`
- Create: `src/ui/glassScrollBridge.test.ts`
- Create: `src/ui/DynamicGlassCanvas.tsx`
- Create: `src/ui/DynamicGlassCanvas.test.tsx`
- Modify: `src/ui/liquidGlass.ts`

**Interfaces:**
- Consumes: `{ theme: RefractionTheme; reduced: boolean; pageKey: string }`, `glassSelectors`, page scroll, internal-scroll events, and layout-invalidation events.
- Produces: `<DynamicGlassCanvas />`, `publishGlassScroll(pageKey, scrollTop)`, `publishGlassLayout(pageKey)`, `GLASS_INTERNAL_SCROLL`, and `GLASS_LAYOUT_CHANGE`.

- [ ] **Step 1: Write failing bridge and renderer contract tests**

```ts
test('coalesces repeated scroll notifications into one frame using the latest value', () => {
  publishGlassScroll('profile', 10);
  publishGlassScroll('profile', 42);
  expect(latestGlassScroll('profile')).toBe(42);
});

test('mounts exactly one visible canvas and no exported image', () => {
  const tree = DynamicGlassCanvas({ theme: 'light', reduced: false, pageKey: 'workspace' }) as any;
  expect(collect(tree, 'Canvas')).toHaveLength(1);
  expect(collect(tree, 'Image')).toHaveLength(0);
});
```

Mock `createSelectorQuery`, `createOffscreenCanvas`, `requestAnimationFrame`, `cancelAnimationFrame`, `usePageScroll`, `useDidShow`, and `useDidHide`. Assert that multiple scroll callbacks before a frame produce one scheduled frame, selector measurement is not repeated per scroll event, hide cancels the frame, and a missing Canvas node activates fallback without throwing.

- [ ] **Step 2: Run tests and verify failure**

Run: `npx vitest run src/ui/glassScrollBridge.test.ts src/ui/DynamicGlassCanvas.test.tsx`

Expected: FAIL because the bridge and renderer do not exist.

- [ ] **Step 3: Implement latest-value scroll and layout bridges**

```ts
export const GLASS_INTERNAL_SCROLL = 'yzcm:glass-internal-scroll';
export const GLASS_LAYOUT_CHANGE = 'yzcm:glass-layout-change';

export function publishGlassScroll(pageKey: string, scrollTop: number) {
  Taro.eventCenter?.trigger(GLASS_INTERNAL_SCROLL, { pageKey, scrollTop });
}

export function publishGlassLayout(pageKey: string) {
  Taro.eventCenter?.trigger(GLASS_LAYOUT_CHANGE, { pageKey });
}
```

The bridge must reject non-finite and negative scroll values by normalizing them to `0`.

- [ ] **Step 4: Implement `DynamicGlassCanvas` initialization and measurement**

Render one `<Canvas type="2d" id={`dynamic-glass-${pageKey}`} className="dynamic-glass-canvas" />`. On the next Taro tick, acquire its node and size, cap DPR at `1.5`, create one offscreen 2D Canvas, paint the wallpaper, and measure `glassSelectors` once with `selectAll(...).boundingClientRect()`.

Store the node, contexts, measured document rectangles, latest scroll, visibility, and scheduled frame in refs. Do not store per-frame data in React state. The only React state allowed is one boolean fallback flag changed during initialization or performance protection.

- [ ] **Step 5: Implement request coalescing and direct drawing**

```ts
const request = () => {
  if (!active.current || frame.current !== undefined || fallback.current) return;
  frame.current = node.requestAnimationFrame(() => {
    frame.current = undefined;
    drawLatestFrame();
  });
};
```

`drawLatestFrame()` copies the wallpaper once, converts cached document rectangles with the latest page/internal scroll, filters to eight visible surfaces, and calls `drawBackgroundRefraction`. It must not query nodes, export a PNG, call `setState`, or enqueue missed frames.

- [ ] **Step 6: Implement lifecycle, resize, theme, and fallback**

`useDidHide` cancels the pending frame. `useDidShow` marks active and requests a frame. Component cleanup cancels frames, unregisters event handlers, and invalidates late selector callbacks. Window resize and `GLASS_LAYOUT_CHANGE` schedule a remeasure outside the draw frame. Canvas/offscreen/context failure sets fallback once and leaves all WXML interactive.

- [ ] **Step 7: Run focused tests**

Run: `npx vitest run src/ui/glassScrollBridge.test.ts src/ui/DynamicGlassCanvas.test.tsx src/ui/glassRefraction.test.ts`

Expected: PASS.

- [ ] **Step 8: Commit renderer and bridge**

```bash
git add src/ui/glassScrollBridge.ts src/ui/glassScrollBridge.test.ts src/ui/DynamicGlassCanvas.tsx src/ui/DynamicGlassCanvas.test.tsx src/ui/liquidGlass.ts
git commit -m "feat: render public glass with one canvas"
```

---

### Task 3: Mount Renderer and Bridge Profile Scroll

**Files:**
- Modify: `src/theme.tsx`
- Modify: `src/pages/profile/index.tsx`
- Modify: `src/pages/workspace/index.tsx`
- Modify: `src/pages/tasks/index.tsx`
- Modify: `src/pages/functions/index.tsx`
- Modify: `src/pages/profile-detail/index.tsx`
- Modify: corresponding existing page tests

**Interfaces:**
- Consumes: `<DynamicGlassCanvas theme reduced pageKey />`, `publishGlassScroll`, and `publishGlassLayout`.
- Produces: one renderer per mounted liquid public page and internal profile scroll updates.

- [ ] **Step 1: Write failing scope and integration tests**

Assert that `ThemedPage` mounts one `DynamicGlassCanvas` when `liquidEnabled(className)` is true and none for login or module-detail classes. Assert that profile `ScrollView` has `onScroll`, publishes `event.detail.scrollTop`, uses a stable throttle interval, and publishes layout invalidation after account data changes.

- [ ] **Step 2: Run focused integration tests**

Run: `npx vitest run src/theme.test.ts src/pages/profile/index.test.tsx src/pages/primary-navigation.test.ts`

Expected: FAIL because the renderer and profile bridge are not wired.

- [ ] **Step 3: Mount one renderer in `ThemedPage`**

Derive a stable page key from the existing public-page class (`workspace`, `tasks`, `functions`, `profile`, `profile-detail`). Mount `DynamicGlassCanvas` next to `LiquidGlassMaterial`, behind all WXML content. Keep `LiquidGlassMaterial` as the CSS fallback/background owner until Canvas initialization succeeds; do not mount a renderer in the theme-reveal preview clone.

- [ ] **Step 4: Wire internal `ScrollView` scroll**

Add `onScroll={event => publishGlassScroll('profile', event.detail.scrollTop)}` and `scrollWithAnimation={false}` to `.mp-profile-scroll`. Publish `publishGlassLayout('profile')` after the profile load stage or section count changes so asynchronous content receives fresh rectangles.

- [ ] **Step 5: Remove per-card static overlay calls from public pages**

Remove `PageTextureRefraction` imports and JSX from workspace, tasks, and functions. Profile and profile-detail already rely on CSS pseudo-elements; those are removed in Task 4. Do not change card content, handlers, navigation, or request logic.

- [ ] **Step 6: Run integration tests**

Run: `npx vitest run src/theme.test.ts src/pages/profile/index.test.tsx src/pages/primary-navigation.test.ts src/pages/functions/functions.interaction.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit public-page integration**

```bash
git add src/theme.tsx src/pages/profile/index.tsx src/pages/workspace/index.tsx src/pages/tasks/index.tsx src/pages/functions/index.tsx src/pages/profile-detail/index.tsx src/theme.test.ts src/pages/profile/index.test.tsx src/pages/primary-navigation.test.ts src/pages/functions/functions.interaction.test.tsx
git commit -m "feat: connect public pages to dynamic refraction"
```

---

### Task 4: Remove Fake Lens Texture and Add Clean Fallback

**Files:**
- Modify: `src/ui/liquidGlass.scss`
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/pages/profile/profile-palette.test.tsx`
- Delete: `src/ui/TextureRefraction.tsx`
- Delete: `src/ui/TextureRefraction.test.tsx`
- Delete: `src/ui/textureRefraction.scss`
- Modify: `src/pages/glass-lab/index.tsx`
- Verify unused: `src/assets/glass/lens-light.png`, `src/assets/glass/lens-dark.png`

**Interfaces:**
- Consumes: `.dynamic-glass-canvas` and `.dynamic-glass-fallback` from the renderer.
- Produces: pointer-transparent Canvas stacking, readable WXML content, and texture-free fallback.

- [ ] **Step 1: Replace tests that expect lens assets with anti-regression tests**

```ts
test('public glass styles never reference repeated lens PNGs', () => {
  const source = readFileSync(resolve(__dirname, 'liquidGlass.scss'), 'utf8');
  expect(source).not.toContain('lens-light.png');
  expect(source).not.toContain('lens-dark.png');
});
```

Assert the Canvas is fixed, fills the viewport, has `pointer-events:none`, and stays behind content. Assert fallback keeps `var(--liquid-fill)`, a thin border, and no background image.

- [ ] **Step 2: Run style tests and verify failure**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/pages/profile/profile-palette.test.tsx src/ui/wxssCompatibility.test.ts`

Expected: FAIL because current profile pseudo-elements still load lens PNGs.

- [ ] **Step 3: Implement stacking and fallback CSS**

Add `.dynamic-glass-layer` and `.dynamic-glass-canvas` as fixed, full-viewport, pointer-transparent layers. Give public content and navigation stable higher z-index values. Remove all `background-image:url(...lens-*.png)` pseudo-elements. In fallback/reduced mode keep only translucent fill, highlight border, inner dark line, and existing readable text colors.

- [ ] **Step 4: Delete fixed overlay components after proving no consumers remain**

Run: `rg -n "TextureRefraction|PageTextureRefraction|texture-refraction|lens-light|lens-dark" src --glob '!assets/glass/*.png'`

Expected before deletion: only glass-lab and tests remain. Update glass-lab to use the shared Canvas painter only, then delete the fixed overlay files. Delete `lens-light.png` and `lens-dark.png` only if a second `rg` proves no source, style, test, or build script consumer remains; otherwise leave the unused assets but prohibit runtime references.

- [ ] **Step 5: Run style and component tests**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/pages/profile/profile-palette.test.tsx src/ui/wxssCompatibility.test.ts src/pages/glass-lab/refraction2d.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit fake-texture removal**

```bash
git add -A src/ui src/pages/profile/profile-palette.test.tsx src/pages/glass-lab src/assets/glass scripts/render-liquid-glass-assets.swift
git commit -m "fix: remove repeated lens texture from public glass"
```

---

### Task 5: Regression, Build, and Visual Acceptance

**Files:**
- Modify only if a failing test identifies an in-scope defect.
- Build output: `dist/`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: tested source, current WeChat build, and recorded visual evidence without uploading a release.

- [ ] **Step 1: Run all automated tests**

Run: `npm test`

Expected: all tests pass with no unhandled rejection.

- [ ] **Step 2: Run TypeScript and production build**

Run: `npx tsc --noEmit`

Expected: exit 0.

Run: `npm run build:weapp`

Expected: exit 0; known CSS-order or asset-size warnings may remain, but there must be no Canvas/WXSS compilation error.

- [ ] **Step 3: Verify built artifacts contain dynamic renderer and no static lens consumer**

```bash
rg -n "dynamic-glass|requestAnimationFrame" dist/pages dist/common.js dist/common.wxss
! rg -n "lens-light|lens-dark|texture-refraction" dist/pages dist/common.js dist/common.wxss
```

Expected: dynamic renderer present; static lens references absent.

- [ ] **Step 4: Compile current `dist` in WeChat Developer Tools**

Open `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram` and compile. Verify the tool is reading `dist/` from `project.config.json`, clear stale compile cache if the built identity does not change, and capture console errors before any further code change.

- [ ] **Step 5: Inspect all five public pages while scrolling**

For workspace, tasks, functions, profile, and profile-detail verify: background bands remain continuous through cards; only edges bend; different cards do not repeat the same arc; refraction is present at rest and follows continuous scroll; clicks and navigation remain responsive; profile internal scroll, page switching, and logout do not freeze.

- [ ] **Step 6: Inspect exclusions and modes**

Verify login and business inner pages are unchanged. Verify light/dark themes and reduced-transparency mode. Simulate Canvas initialization failure in Developer Tools and confirm clean fallback with no repeated circle.

- [ ] **Step 7: Perform one real-device check**

Preview on one representative device and repeat continuous scroll, page switching, and profile logout. Record device/model/base-library version and whether fallback activated. If no real device is available, report this acceptance gate as pending and do not call the feature fixed.

- [ ] **Step 8: Commit only in-scope verification fixes**

```bash
git status --short
git diff --check
git add <only files changed to resolve verified in-scope failures>
git commit -m "test: verify dynamic mini program refraction"
```

Do not stage unrelated dirty files and do not upload an experience or production version.
