# Private Comic Public Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将私人定制漫画小程序重构为与公网语义一致、且只暴露真实接口能力的移动端工作台。

**Architecture:** 保留 `comic-new` 作为唯一入口，拆出配置、资产、生成配置确认、分页出图和导出五个明确阶段。前端状态机只驱动已有 `comicFlow`、`comicAssets`、`comicResult` 服务；不新增后端 endpoint，不把批量工厂的视频/音频/发布能力带入私人定制。

**Tech Stack:** Taro 4 + React + TypeScript + SCSS + Vitest。

## Global Constraints

- 图片画幅固定为公网同源的 `16:9 / 9:16 / 1:1`，提交字段为 `imageAspectRatio`。
- 首次主操作只创建批次、上传参考图和提取资产；显式确认配置后才调用 `submitComicRun`。
- 没有真实 endpoint 支持的分镜编辑、资产文本编辑、手动上传和主图切换不得出现可点击假按钮。
- 不修改批量工厂页面、后端、数据库、账号、密码和部署。
- 不自动重放未知或失败的正式生成任务。
- 触控目标至少 44px，320/375/390/430px 和深浅主题无横向溢出。

## File Map

- Create: `src/comic/pages/comic-new/privateComicParity.ts` — 公网对齐字段、阶段前置条件与文案纯函数。
- Create: `src/comic/pages/comic-new/privateComicParity.test.ts` — 字段映射、阶段前置、动作权限测试。
- Create: `src/comic/pages/comic-new/ComicSettingsStage.tsx` — 故事、画风、图片画幅、参考图配置卡。
- Create: `src/comic/pages/comic-new/ComicAssetsStage.tsx` — 分类资产卡、真实资产动作和只读能力提示。
- Create: `src/comic/pages/comic-new/ComicConfigConfirmStage.tsx` — 生成前配置确认，不伪装成分镜编辑器。
- Modify: `src/comic/pages/comic-new/index.tsx` — 组合阶段组件、统一状态和动作边界。
- Modify: `src/comic/pages/comic-new/index.test.tsx` — 新阶段交互和公网字段测试。
- Modify: `src/comic/styles/comic.scss` — 公网对齐卡片、状态标签、移动端响应式样式。
- Modify: `docs/2026-11-05-private-comic-studio-verification.md` — 记录公网对齐验证证据。

### Task 1: 建立公网对齐纯函数和测试

**Files:**
- Create: `src/comic/pages/comic-new/privateComicParity.ts`
- Create: `src/comic/pages/comic-new/privateComicParity.test.ts`

**Interfaces:**
- `PUBLIC_COMIC_ASPECTS: readonly [{ value: "16:9"; label: "16:9" }, { value: "9:16"; label: "9:16" }, { value: "1:1"; label: "1:1" }]`
- `privateComicStage = "settings" | "assets" | "config" | "render" | "result"`
- `canEnterPrivateComicStage(stage, input): boolean`
- `privateComicAction(stage, input): { label: string; enabled: boolean }`

- [ ] **Step 1: Write failing tests** for exact aspect ordering, settings-to-assets gating, config requiring assets, render requiring submitted status, and result requiring pages.
- [ ] **Step 2: Run focused test** with `npm test -- --run src/comic/pages/comic-new/privateComicParity.test.ts`; expect missing module failure.
- [ ] **Step 3: Implement pure constants and functions** with no Taro or network imports.
- [ ] **Step 4: Run focused test** and verify PASS.
- [ ] **Step 5: Commit** with `git add ... && git commit -m "test: define private comic public parity state"`.

### Task 2: Extract settings stage

**Files:**
- Create: `src/comic/pages/comic-new/ComicSettingsStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Modify: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicSettingsStageProps = { source; styleId; aspectRatio; referencePath; styles; thumbs; setupLoading; error; onSourceChange; onStyleChange; onAspectChange; onOpenReference; onContinue }`

- [ ] **Step 1: Add tests** asserting the exact `16:9 / 9:16 / 1:1` labels, `图片画幅` title, source validation, and that the continue callback does not submit a run.
- [ ] **Step 2: Run focused page test** and verify the new contract fails before extraction.
- [ ] **Step 3: Move existing settings JSX** into the component; keep `maxlength={-1}`, existing source length rules, style thumb loading, and reference picker.
- [ ] **Step 4: Wire `onContinue`** to the existing create/upload/extract action and transition to assets only after validation.
- [ ] **Step 5: Run page tests** with `npm test -- --run src/comic/pages/comic-new/index.test.tsx`.
- [ ] **Step 6: Commit** with `git commit -m "feat: align private comic settings stage"`.

### Task 3: Replace asset summary with a real mobile asset workbench

**Files:**
- Create: `src/comic/pages/comic-new/ComicAssetsStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Modify: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicAssetsStageProps = { assets; assetImages; assetThumbs; loading; busy; error; onExtract; onGenerateMissingImages; onRetry; onRefresh; onContinue; onBack }`

- [ ] **Step 1: Add tests** for category counts, missing image state, real action callbacks, and disabled unsupported edit controls.
- [ ] **Step 2: Implement category tabs** for 人物/场景/道具 and compact search/filter over already-loaded assets.
- [ ] **Step 3: Implement action strip** using only `extractComicAssets`, `generateComicAssetImages`, `retryComicBookStage`, and reload; keep per-asset image failure local.
- [ ] **Step 4: Add an explicit read-only note** that asset text editing, manual upload, and primary-image switching are not available in this mini-program endpoint set.
- [ ] **Step 5: Gate “继续：生成配置确认”** on a real asset list; never submit the production run here.
- [ ] **Step 6: Run page and asset service tests**, then commit `feat: add public-aligned private comic assets stage`.

### Task 4: Rename and implement the configuration confirmation stage

**Files:**
- Create: `src/comic/pages/comic-new/ComicConfigConfirmStage.tsx`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/styles/comic.scss`
- Modify: `src/comic/pages/comic-new/index.test.tsx`

**Interfaces:**
- `ComicConfigConfirmStageProps = { sourceLength; styleName; aspectRatio; referenceReady; assets; submitting; error; onBack; onConfirm }`

- [ ] **Step 1: Add tests** asserting the stage title is “生成配置确认”, that it lists real settings and assets, and that confirm invokes exactly one `submitComicRun` call.
- [ ] **Step 2: Implement read-only confirmation cards** for source summary, style, image aspect, reference state, and asset counts.
- [ ] **Step 3: Keep unsupported storyboard editing out**; use explanatory copy instead of fake page editing controls.
- [ ] **Step 4: Wire confirmation** to the existing run payload with stored reference metadata and transition to render only after the request resolves.
- [ ] **Step 5: Run focused tests** and commit `feat: add truthful private comic config confirmation`.

### Task 5: Recompose result/render and verify

**Files:**
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/pages/comic-new/index.test.tsx`
- Modify: `src/comic/styles/comic.scss`
- Modify: `docs/2026-11-05-private-comic-studio-verification.md`

- [ ] **Step 1: Replace stage rail labels** with `故事设置 / 资产工作台 / 生成配置确认 / 分页出图 / 成果导出`; only stages passing `canEnterPrivateComicStage` are clickable.
- [ ] **Step 2: Keep render polling and result export** bound to the existing batch/book/run/page scope; stop polling on stage change, unmount, finished, or error.
- [ ] **Step 3: Add tests** for no-submit-before-confirm, one-submit-after-confirm, aspect payload parity, result gating, and stale page image invalidation.
- [ ] **Step 4: Run verification:**
  - `npm test -- --run src/comic`
  - `npm run build:weapp`
  - `npx tsc --noEmit --pretty false` and record only the existing unrelated errors.
- [ ] **Step 5: Inspect `dist/` identity** with `git rev-parse HEAD`, then document what was and was not visually verified in WeChat Developer Tools.
- [ ] **Step 6: Commit** with `git commit -m "feat: complete private comic public parity workbench"`.

## Plan Self-Review

- Public batch-factory video/audio/publish features are explicitly excluded.
- Every new stage maps to existing service calls or is explicitly read-only.
- No task introduces a backend endpoint or claims storyboard mutation support.
- The plan includes exact tests for the previous mismatch: visual stage labels must not imply unsupported operations.
