# 小程序公共页液态背景与常驻折射 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将工作区、任务、功能、我的和个人资料页换成参考页的冰蓝背景，并让卡片在静止与滑动期间始终具有可见折射。

**Architecture:** 使用本地 PNG 提供稳定的浅色／深色冰蓝背景，以及卡片内部的透明折射纹理。`LiquidGlassMaterial` 继续只管理全屏背景；`TextureRefraction` 在每张卡片内渲染“折射纹理 + 边缘 rim”，不使用 Canvas 截图、异步读回或逐帧 React 更新。

**Tech Stack:** Taro 4.2.1, React, TypeScript, SCSS/WXSS, Vitest, Swift CoreGraphics/AppKit 本地资产生成。

## Global Constraints

- 液态材质只用于工作区、任务、功能、我的和个人资料页。
- 登录页、入场视频、登录轮子和业务内页不改。
- 折射层在滑动时必须持续可见，不得在停止后才突然显示。
- 不使用微信小程序不支持的 SVG `backdrop-filter` 或浏览器专用滤镜。
- 降低透明度时保留静态边缘折射，不保留频繁动画。
- 不修改数据请求、会话、导航和业务逻辑。

---

### Task 1: 生成冰蓝背景与折射纹理

**Files:**
- Create: `scripts/render-liquid-glass-assets.swift`
- Modify: `src/assets/glass/wallpaper-light.png`
- Modify: `src/assets/glass/wallpaper-dark.png`
- Create: `src/assets/glass/lens-light.png`
- Create: `src/assets/glass/lens-dark.png`
- Test: `src/ui/LiquidGlassMaterial.test.tsx`

**Interfaces:**
- Produces: `wallpaper-light.png` and `wallpaper-dark.png`, each `390x844`, opaque full-screen PNG.
- Produces: `lens-light.png` and `lens-dark.png`, each `240x160`, transparent PNG containing curved displaced light bands but no text or controls.

- [ ] **Step 1: Add the failing asset-contract test**

Extend `src/ui/LiquidGlassMaterial.test.tsx` so the component contract still requires exactly one wallpaper image and source inspection requires all four generated assets:

```ts
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

test('ships matching wallpaper and lens assets for both themes', () => {
  for (const name of ['wallpaper-light.png', 'wallpaper-dark.png', 'lens-light.png', 'lens-dark.png']) {
    expect(existsSync(resolve(__dirname, `../assets/glass/${name}`))).toBe(true);
  }
});
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `npx vitest run src/ui/LiquidGlassMaterial.test.tsx`

Expected: FAIL because `lens-light.png` and `lens-dark.png` do not exist.

- [ ] **Step 3: Add the deterministic asset generator**

Create `scripts/render-liquid-glass-assets.swift` using `AppKit`/`CoreGraphics`:

```swift
import AppKit

extension NSColor {
  convenience init(hex: Int, alpha: CGFloat = 1) {
    self.init(calibratedRed: CGFloat((hex >> 16) & 255) / 255,
              green: CGFloat((hex >> 8) & 255) / 255,
              blue: CGFloat(hex & 255) / 255,
              alpha: alpha)
  }
}

struct Palette {
  let top: NSColor, middle: NSColor, bottom: NSColor
  let ribbon: NSColor, shadow: NSColor, highlight: NSColor
}

func png(_ size: NSSize, output: URL, draw: () -> Void) throws {
  let image = NSImage(size: size)
  image.lockFocus(); draw(); image.unlockFocus()
  let rep = NSBitmapImageRep(data: image.tiffRepresentation!)!
  try rep.representation(using: .png, properties: [:])!.write(to: output)
}

func renderWallpaper(_ palette: Palette, to output: URL) throws {
  try png(NSSize(width: 390, height: 844), output: output) {
    NSGradient(colors: [palette.top, palette.middle, palette.bottom])!
      .draw(in: NSRect(x: 0, y: 0, width: 390, height: 844), angle: -52)
    let ribbon = NSBezierPath(ovalIn: NSRect(x: -250, y: -40, width: 650, height: 1040))
    ribbon.lineWidth = 92; palette.ribbon.setStroke(); ribbon.stroke()
    let shadow = NSBezierPath(ovalIn: NSRect(x: -224, y: -24, width: 650, height: 1040))
    shadow.lineWidth = 18; palette.shadow.setStroke(); shadow.stroke()
    let shine = NSBezierPath(ovalIn: NSRect(x: -278, y: -55, width: 650, height: 1040))
    shine.lineWidth = 20; palette.highlight.setStroke(); shine.stroke()
  }
}

func renderLens(_ palette: Palette, to output: URL) throws {
  try png(NSSize(width: 240, height: 160), output: output) {
    NSColor.clear.setFill(); NSRect(x: 0, y: 0, width: 240, height: 160).fill()
    let band = NSBezierPath(ovalIn: NSRect(x: -82, y: -102, width: 392, height: 344))
    band.lineWidth = 38; palette.ribbon.withAlphaComponent(0.42).setStroke(); band.stroke()
    let inner = NSBezierPath(ovalIn: NSRect(x: -69, y: -91, width: 367, height: 322))
    inner.lineWidth = 9; palette.highlight.withAlphaComponent(0.66).setStroke(); inner.stroke()
    let edge = NSBezierPath(ovalIn: NSRect(x: -93, y: -109, width: 416, height: 360))
    edge.lineWidth = 8; palette.shadow.withAlphaComponent(0.34).setStroke(); edge.stroke()
  }
}

let directory = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let light = Palette(top: .init(hex: 0xF5FAF8), middle: .init(hex: 0xD1EEF2), bottom: .init(hex: 0x80B7C2), ribbon: .init(hex: 0xE2F8F1), shadow: .init(hex: 0x286883), highlight: .white)
let dark = Palette(top: .init(hex: 0x193746), middle: .init(hex: 0x285668), bottom: .init(hex: 0x122F46), ribbon: .init(hex: 0xA5DEE4), shadow: .init(hex: 0x071F30), highlight: .init(hex: 0xE0FFFF))
try renderWallpaper(light, to: directory.appendingPathComponent("wallpaper-light.png"))
try renderWallpaper(dark, to: directory.appendingPathComponent("wallpaper-dark.png"))
try renderLens(light, to: directory.appendingPathComponent("lens-light.png"))
try renderLens(dark, to: directory.appendingPathComponent("lens-dark.png"))
```

The light palette follows `#f5faf8`, `#d1eef2`, `#80b7c2`, `#e2f8f1`, and `#286883`. The dark palette uses the same geometry with `#193746`, `#285668`, `#122f46`, and cyan-white highlights.

- [ ] **Step 4: Generate the four assets**

Run:

```bash
swift scripts/render-liquid-glass-assets.swift src/assets/glass
sips -g pixelWidth -g pixelHeight -g hasAlpha src/assets/glass/wallpaper-light.png src/assets/glass/wallpaper-dark.png src/assets/glass/lens-light.png src/assets/glass/lens-dark.png
```

Expected: wallpapers are `390x844`; lenses are `240x160`; lens files have alpha.

- [ ] **Step 5: Run the focused test and verify GREEN**

Run: `npx vitest run src/ui/LiquidGlassMaterial.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit the asset unit**

```bash
git add scripts/render-liquid-glass-assets.swift src/assets/glass/wallpaper-light.png src/assets/glass/wallpaper-dark.png src/assets/glass/lens-light.png src/assets/glass/lens-dark.png src/ui/LiquidGlassMaterial.test.tsx
git commit -m "feat: add ice liquid glass assets"
```

### Task 2: 将卡片折射从静态白边升级为常驻双层光学材质

**Files:**
- Modify: `src/ui/TextureRefraction.tsx`
- Modify: `src/ui/TextureRefraction.test.tsx`
- Modify: `src/ui/textureRefraction.scss`
- Modify: `src/ui/liquidGlass.scss`
- Test: `src/ui/wxssCompatibility.test.ts`

**Interfaces:**
- Consumes: `lens-light.png`, `lens-dark.png`, `rim-light.png`, `rim-dark.png`.
- Produces: `TextureRefraction({ theme, reduced? })`, with `.texture-lens` and `.texture-rim` image layers.

- [ ] **Step 1: Replace the old one-rim assertion with the failing two-layer contract**

Update `src/ui/TextureRefraction.test.tsx`:

```ts
vi.mock('../assets/glass/lens-dark.png', () => ({ default: 'lens-dark' }));
vi.mock('../assets/glass/lens-light.png', () => ({ default: 'lens-light' }));

test('each glass surface keeps a persistent lens behind its optical rim', () => {
  const tree = TextureRefraction({ theme: 'dark' }) as any;
  const images = collect(tree, 'Image');
  expect(images.map(image => image.props.className)).toEqual(['texture-lens', 'texture-rim']);
  expect(images.map(image => image.props.src)).toEqual(['lens-dark', 'rim-dark']);
});

test('reduced transparency retains the rim but drops the decorative lens', () => {
  const images = collect(TextureRefraction({ theme: 'light', reduced: true }) as any, 'Image');
  expect(images.map(image => image.props.className)).toEqual(['texture-rim']);
});
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `npx vitest run src/ui/TextureRefraction.test.tsx`

Expected: FAIL because the component still renders only `.texture-rim` and returns `null` when reduced.

- [ ] **Step 3: Render the lens and rim in stable order**

Implement in `src/ui/TextureRefraction.tsx`:

```tsx
import darkLens from '../assets/glass/lens-dark.png';
import lightLens from '../assets/glass/lens-light.png';

export default function TextureRefraction({ theme, reduced = false }: Props) {
  return <View className="texture-refraction" aria-hidden="true">
    {!reduced ? <Image className="texture-lens" src={theme === 'dark' ? darkLens : lightLens} mode="scaleToFill" /> : null}
    <Image className="texture-rim" src={theme === 'dark' ? darkRim : lightRim} mode="scaleToFill" />
  </View>;
}
```

Keep the existing prop type and define it explicitly if needed:

```ts
type Props = { theme: 'light' | 'dark'; reduced?: boolean };
```

- [ ] **Step 4: Add the permanent optical-layer styles**

Update `src/ui/textureRefraction.scss` so the lens remains visible without scroll state or JavaScript updates:

```scss
.texture-refraction{position:absolute;inset:0;border-radius:inherit;pointer-events:none;overflow:hidden;z-index:0}
.texture-lens{position:absolute;inset:-5%;width:110%;height:110%;pointer-events:none;opacity:.72;transform:scale(1.035)}
.texture-rim{position:absolute;inset:0;width:100%;height:100%;pointer-events:none;opacity:.9}
.liquid-reduced .texture-lens{display:none}
.liquid-reduced .texture-rim{opacity:.72}
```

Update `src/ui/liquidGlass.scss` to increase light-theme clarity without making cards opaque:

```scss
&.mp-theme-light {
  --liquid-fill:rgba(255,255,255,.12);
  --liquid-control:rgba(255,255,255,.28);
  --liquid-shadow:rgba(24,70,88,.11);
}
```

Remove the rule that hides the entire `.texture-refraction` under `.liquid-reduced`; only the lens is hidden while the rim remains.

- [ ] **Step 5: Run component and WXSS tests**

Run:

```bash
npx vitest run src/ui/TextureRefraction.test.tsx src/ui/wxssCompatibility.test.ts
```

Expected: PASS with no unsupported selector or syntax regressions.

- [ ] **Step 6: Commit the optical component**

```bash
git add src/ui/TextureRefraction.tsx src/ui/TextureRefraction.test.tsx src/ui/textureRefraction.scss src/ui/liquidGlass.scss
git commit -m "feat: keep liquid refraction visible"
```

### Task 3: 重建小程序并验收所有公共页

**Files:**
- Verify: `dist/assets/glass/wallpaper-light.png`
- Verify: `dist/assets/glass/wallpaper-dark.png`
- Verify: `dist/assets/glass/lens-light.png`
- Verify: `dist/assets/glass/lens-dark.png`
- Verify: `dist/app.wxss`

**Interfaces:**
- Consumes: Tasks 1-2 assets and `TextureRefraction` contract.
- Produces: rebuilt WeChat mini-program artifacts in `dist/` and visual evidence from WeChat Developer Tools.

- [ ] **Step 1: Run the complete test suite**

Run: `npm test`

Expected: all test files and tests PASS. Existing React DOM casing or ARIA warnings may remain, but no new failure is allowed.

- [ ] **Step 2: Build the WeChat target**

Run: `npm run build:weapp`

Expected: exit `0`; the existing CSS-order warning is recorded separately and does not block this scoped material change.

- [ ] **Step 3: Verify built assets and source identity**

Run:

```bash
shasum -a 256 src/assets/glass/wallpaper-light.png dist/assets/glass/wallpaper-light.png
shasum -a 256 src/assets/glass/wallpaper-dark.png dist/assets/glass/wallpaper-dark.png
shasum -a 256 src/assets/glass/lens-light.png dist/assets/glass/lens-light.png
shasum -a 256 src/assets/glass/lens-dark.png dist/assets/glass/lens-dark.png
git diff --check
```

Expected: each source/dist pair has the same SHA-256 and `git diff --check` exits `0`.

- [ ] **Step 4: Compile the generated `dist/` in WeChat Developer Tools**

Confirm the open project URL contains:

```text
projectpath=/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist
appid=wxd72c311f3711eda9
```

Compile without uploading a release.

- [ ] **Step 5: Visually inspect the five public-page routes**

Check `pages/workspace/index`, `pages/tasks/index`, `pages/functions/index`, `pages/profile/index`, and `pages/profile-detail/index`:

```text
background = soft ice-blue reference composition
card centre = translucent with a visible curved optical band
card edge = bright outer rim plus cool inner shadow
scrolling = lens remains visible continuously; no stop-time flash
controls = clickable; text remains legible
```

Also check `pages/bind/index` and one `pages/module-detail/index` route to confirm login and business interiors did not inherit the material.

- [ ] **Step 6: Record final verification and commit only scoped changes**

If visual inspection reveals no regression, ensure the exact scoped files are committed and report that no mini-program version was uploaded. Do not stage unrelated dirty files.
