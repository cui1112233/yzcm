# Private Comic Studio Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将私人定制漫画页从“创作/结果混合页”整理为可恢复的单本阶段工作台，保留现有真实服务调用，只改善信息架构、交互状态和错误可见性。

**Architecture:** 保留 `comic-new` 作为私人定制唯一入口，新增显式的本地 `stage` 状态（source/assets/storyboard/render/result），把现有服务调用绑定到阶段动作；不新增后端接口。通过小型展示组件承载阶段卡片和结果区，避免继续把所有 JSX 挤在同一个条件分支中。

**Tech Stack:** Taro 4 + React + TypeScript，现有 `comicFlow`、`comicAssets`、`comicResult` 服务，Vitest/Taro 组件测试，现有 CSS 变量与双主题系统。

## Global Constraints

- 只修改本地小程序；不改批量工厂页面、后端、数据库、账号、密码或部署。
- 私人定制链路保持：故事来源 → 资产提取 → 资产定妆 → 分镜确认 → 分页出图 → 成果导出。
- 不新增第二套 comic API；继续复用 `comicFlow.ts`、`comicAssets.ts`、`comicResult.ts`。
- 未知提交结果不得自动重复；轮询和媒体加载必须绑定当前 batch/book/run/page 作用域。
- 不以构建成功、HTTP 200、模拟 API 或页面打开代替真实任务回执、媒体读回和视觉验收。
- 触控目标至少 44px；深浅主题均需验证，无横向溢出。

## File Map

- Modify: `src/comic/pages/comic-new/index.tsx` — 阶段状态机、阶段导航、阶段动作、草稿/错误/恢复边界。
- Create: `src/comic/pages/comic-new/ComicStageHeader.tsx` — 私人定制阶段标题、阶段进度和当前作品摘要。
- Create: `src/comic/pages/comic-new/ComicSourceStage.tsx` — 正文/已有故事输入、画风/画幅/参考图草稿表单。
- Create: `src/comic/pages/comic-new/ComicAssetStage.tsx` — 资产摘要、定妆图状态和单阶段动作入口。
- Create: `src/comic/pages/comic-new/ComicStoryboardStage.tsx` — 分镜摘要、确认和返回编辑入口。
- Create: `src/comic/pages/comic-new/ComicResultStage.tsx` — 生成进度与结果大图、候选、分页和导出操作。
- Modify: `src/comic/styles/comic.scss` — 阶段工作台视觉层、移动交互、状态颜色、阶段卡和结果操作区。
- Modify: `src/comic/pages/comic-new/index.test.tsx` — 阶段导航、输入、错误和结果动作测试。
- Create: `src/comic/pages/comic-new/stageState.ts` — 纯函数阶段映射、前置条件和文案，便于测试。
- Create: `src/comic/pages/comic-new/stageState.test.ts` — 阶段状态纯函数单测。

### Task 1: 建立阶段状态纯函数

**Files:**
- Create: `src/comic/pages/comic-new/stageState.ts`
- Create: `src/comic/pages/comic-new/stageState.test.ts`

**Interfaces:**
- `ComicStage = "source" | "assets" | "storyboard" | "render" | "result"`
- `stageIndex(stage: ComicStage): number`
- `stageTitle(stage: ComicStage): string`
- `stageCanEnter(stage: ComicStage, input: { sourceLength: number; batchId: string; assetsCount: number; pagesCount: number; finished: boolean }): boolean`
- `stageNext(stage: ComicStage): ComicStage | null`

- [ ] **Step 1: Write failing tests**

```ts
import { describe, expect, test } from "vitest";
import { stageCanEnter, stageIndex, stageNext, stageTitle } from "./stageState";

describe("private comic stage state", () => {
  test("maps stages in production order", () => {
    expect(stageIndex("source")).toBe(0);
    expect(stageIndex("assets")).toBe(1);
    expect(stageIndex("result")).toBe(4);
    expect(stageNext("storyboard")).toBe("render");
    expect(stageNext("result")).toBeNull();
  });

  test("does not enter assets without a created book", () => {
    expect(stageCanEnter("assets", { sourceLength: 120, batchId: "", assetsCount: 0, pagesCount: 0, finished: false })).toBe(false);
    expect(stageCanEnter("assets", { sourceLength: 120, batchId: "b1", assetsCount: 0, pagesCount: 0, finished: false })).toBe(true);
  });

  test("uses user-facing Chinese titles", () => {
    expect(stageTitle("storyboard")).toBe("确认分镜");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm test -- --run src/comic/pages/comic-new/stageState.test.ts`

Expected: FAIL because `stageState.ts` does not exist.

- [ ] **Step 3: Implement the pure mapping**

Implement the five-stage tuple in production order. `assets` requires `batchId`; `storyboard` requires `batchId` and at least one asset or a known server-side empty state; `render` requires `batchId`; `result` requires `pagesCount > 0` or `finished === true`. Keep all functions pure.

- [ ] **Step 4: Run the focused test**

Run: `npm test -- --run src/comic/pages/comic-new/stageState.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/comic/pages/comic-new/stageState.ts src/comic/pages/comic-new/stageState.test.ts
git commit -m "test: define private comic stage state"
```

### Task 2: Extract the source stage and stage header

**Files:**
- Create: `src/comic/pages/comic-new/ComicStageHeader.tsx`
- Create: `src/comic/pages/comic-new/ComicSourceStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Test: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicStageHeaderProps = { stage: ComicStage; title: string; subtitle: string; hasResult: boolean; onSelectStage(stage: ComicStage): void }`
- `ComicSourceStageProps = { source: string; styleId: string; aspectRatio: string; referencePath: string; styles: ComicStylePreset[]; thumbs: Record<string,string>; setupLoading: boolean; error: string; onSourceChange(value:string): void; onStyleChange(id:string): void; onAspectChange(value:string): void; onOpenReference(): void; onContinue(): void }`

- [ ] **Step 1: Add interaction tests before extraction**

Add tests asserting:正文输入 updates the draft; stage header shows `1 / 5`; source stage does not call `createComicBatch` before “继续：提取人物与场景”; clicking the continue action invokes the supplied callback only after the existing source validation passes.

- [ ] **Step 2: Run the focused page test to verify missing behavior**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx`

Expected: FAIL for the new stage labels and callback contract.

- [ ] **Step 3: Implement header and source stage**

Move the existing正文纸张、画风横滑、画幅选择、参考图 bottom sheet trigger into `ComicSourceStage`. Add a visible stage rail with labels `故事来源 / 资产提取 / 分镜确认 / 分页出图 / 成果导出`. Keep the textarea `maxlength={-1}`, existing `MIN_COMIC_SOURCE_CHARS`, existing style thumb loading, and existing reference picker.

- [ ] **Step 4: Wire source stage into `index.tsx`**

Replace the current create/result tab as the primary navigation with `stage`. Keep a small “结果” shortcut only when pages exist; it must navigate to `result` without changing batch/book state. The “继续” callback sets stage to `assets` only after the current `canSubmit` checks have passed.

- [ ] **Step 5: Run focused tests and typecheck**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx && npx tsc --noEmit --pretty false`

Expected: PASS with no TypeScript errors.

- [ ] **Step 6: Commit**

```bash
git add src/comic/pages/comic-new/ComicStageHeader.tsx src/comic/pages/comic-new/ComicSourceStage.tsx src/comic/pages/comic-new/index.tsx src/comic/styles/comic.scss src/comic/pages/comic-new/index.test.tsx
git commit -m "feat: add private comic source stage"
```

### Task 3: Add asset and storyboard stages

**Files:**
- Create: `src/comic/pages/comic-new/ComicAssetStage.tsx`
- Create: `src/comic/pages/comic-new/ComicStoryboardStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Test: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicAssetStageProps = { assets: ComicAssetItem[]; assetThumbs: Record<string,string>; assetImages: Record<string,ComicAssetImageItem[]>; loading: boolean; busy: PrivateComicAssetBusy; error: string; onExtract(): void; onGenerateMissingImages(): void; onRetry(): void; onBack(): void; onContinue(): void }`
- `ComicStoryboardStageProps = { pages: ComicPageItem[]; loading: boolean; error: string; onBack(): void; onConfirm(): void; onEditPage(index:number): void }`

- [ ] **Step 1: Add failing tests**

Cover: asset stage shows counts from `comicAssetKindCounts`; missing images show a “补齐资产图” action; asset error keeps stage visible with retry; storyboard renders page ordinal/prompt summary and calls `onConfirm` without starting a second request.

- [ ] **Step 2: Run focused tests to verify failure**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx`

Expected: FAIL because the stage components and callbacks are not present.

- [ ] **Step 3: Implement asset stage**

Move the current asset summary sheet and asset actions into a dedicated stage. Keep `extractComicAssets`, `generateComicAssetImages`, `retryComicBookStage`, and their existing `assetBusy` guards. Make each action label its scope (“重新提取资产”“补齐缺失定妆图”“重试失败阶段”). Asset image failures remain local to the asset row; do not turn a missing one into a page-level error.

- [ ] **Step 4: Implement storyboard stage**

Use loaded `ComicPageItem[]` as the read-only server-backed storyboard projection. Show one compact card per page, expand one page at a time, expose prompt summary and page status, and keep “返回资产定妆” and “确认分镜，开始分页出图” as separate actions. If the existing backend does not expose a dedicated storyboard mutation, the confirm action must only transition into the existing production submission path; do not invent a new endpoint.

- [ ] **Step 5: Wire and test**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx && npx tsc --noEmit --pretty false`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/comic/pages/comic-new/ComicAssetStage.tsx src/comic/pages/comic-new/ComicStoryboardStage.tsx src/comic/pages/comic-new/index.tsx src/comic/styles/comic.scss src/comic/pages/comic-new/index.test.tsx
git commit -m "feat: add private comic asset and storyboard stages"
```

### Task 4: Add render/result stage and resilient advanced interactions

**Files:**
- Create: `src/comic/pages/comic-new/ComicResultStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Test: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicResultStageProps = { status: BatchFactoryAutomationStatus | null; pages: ComicPageItem[]; pageIndex: number; paths: Record<string,string>; progressError: string; resultError: string; busy: string; finished: boolean; onSelectPage(index:number): void; onPreview(imageId:string): void; onSave(): void; onDownload(): void; onRetryStage(): void; onBack(): void }`

- [ ] **Step 1: Add failing tests**

Cover: result stage displays page progress, candidate images, and separate “保存本页 / 整本下载” actions; a failed status exposes a retry action; switching page clears stale image paths before loading the new page.

- [ ] **Step 2: Run focused tests to verify failure**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx`

Expected: FAIL for the new result-stage labels and failure branch.

- [ ] **Step 3: Implement result stage**

Move existing result markup into the stage component. Keep authenticated media loading, `Taro.previewImage`, album permission handling, collection creation and ZIP download. Add explicit per-page advanced action labels; where backend support is not yet present, keep the control disabled or show an unavailable message instead of fake success.

- [ ] **Step 4: Make async scope explicit**

Use the existing `imageVersion`, `resultLoaded`, `assetsLoadedFor`, and polling stop refs to invalidate late responses on stage change, new work, page change, and account/session change. Keep polling sequential and stop it on finished/error/unmount. Do not replay a failed or unknown production submission automatically.

- [ ] **Step 5: Run tests, typecheck, and build**

Run: `npm test -- --run src/comic/pages/comic-new/index.test.tsx src/comic/services/comicAssets.test.ts src/comic/services/comicResult.test.ts && npx tsc --noEmit --pretty false && npm run build:weapp`

Expected: all selected tests pass, typecheck passes, and `dist/` is generated. This does not constitute real provider or media acceptance.

- [ ] **Step 6: Commit**

```bash
git add src/comic/pages/comic-new/ComicResultStage.tsx src/comic/pages/comic-new/index.tsx src/comic/styles/comic.scss src/comic/pages/comic-new/index.test.tsx
git commit -m "feat: organize private comic render and result stages"
```

### Task 5: Visual and runtime verification

**Files:**
- Modify: `docs/2026-11-05-private-comic-studio-verification.md`

- [ ] **Step 1: Run the full comic test set**

Run: `npm test -- --run src/comic`

Expected: all comic page/service tests pass.

- [ ] **Step 2: Run build and inspect generated identity**

Run: `npm run build:weapp` and record the generated `dist/` timestamp and `git rev-parse HEAD`. Do not claim the Developer Tools package is updated until the actual `dist/` project is recompiled there.

- [ ] **Step 3: Verify layout matrix**

In the Developer Tools simulator, inspect 320/375/390/430px widths × dark/light themes for: source input, style rail, reference sheet, asset cards, storyboard cards, render progress, result stage, sticky action bar, and error/retry states. Capture screenshots only after confirming the loaded project directory is the real `miniprogram` checkout.

- [ ] **Step 4: Verify read-only real data boundary**

With an authenticated account, open an existing private comic record and verify actual asset/page/media reads, candidate image preview, page switching and download readback. Do not submit a new paid generation in this task unless the user explicitly confirms it.

- [ ] **Step 5: Record evidence without overclaiming**

Write what was actually verified, what remains unverified (provider receipt, real generation, mobile device), and the exact source/build identity. Do not call the task complete from build success or page opening alone.

- [ ] **Step 6: Commit verification record**

```bash
git add docs/2026-11-05-private-comic-studio-verification.md
git commit -m "docs: record private comic layout verification"
```

## Plan Self-Review

- Spec coverage: source input, staged layout, assets, storyboard, render/result, API boundaries, retries, unknown results, theme/touch, tests, and non-goals are covered by Tasks 1–5.
- Placeholder scan: no TBD/TODO steps; all tasks name concrete files, commands, interfaces, and expected outcomes.
- Type consistency: `ComicStage`, stage components, and callback contracts are defined before their consumers; existing service types are reused.
- Scope: one client subsystem only; batch factory remains explicitly out of scope.
