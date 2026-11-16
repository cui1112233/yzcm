# 统一小程序 UI 与布局 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将剧本生成、私人定制漫画、批量工厂及其他功能内部页面收敛到同一套深浅双主题控件、紧凑选择布局和渐进披露高级交互，同时保持工作区首页与外层玻璃工作台不变。

**Architecture:** 以 `src/ui/tokens.ts` 作为颜色、间距、圆角和控件尺寸的唯一来源；以 `src/ui/system.scss` 提供跨页面基础组件；页面专属 SCSS 只负责布局差异，不再定义独立主题。流程页统一使用“标题 + 步骤 + 主内容 + 高级设置摘要 + 底部主操作”骨架，选择项通过共享行、胶囊和底部抽屉表达。

**Tech Stack:** Taro、React、TypeScript、SCSS、Vitest、微信小程序 WXSS 兼容构建。

## Global Constraints

- 深色与浅色主题必须保留，布局和组件结构完全一致，只切换颜色 token。
- 外层液态玻璃背景、动态壁纸和主题光晕继续保留，并统一为页面壳；内层功能控件不再各自叠加独立玻璃效果。
- 剧本生成、漫画生成等功能内容区必须使用平面化深蓝/冷灰实色表面，不引入 `backdrop-filter`、透明卡片或玻璃高光。
- 必填输入和当前步骤优先展示，高级配置默认折叠并通过底部抽屉/半屏弹层承载。
- 每个流程页只保留一个 `mp-primary` 主操作；次要操作使用 `mp-secondary` 或文字入口。
- 选择项不再重复显示“当前/切换/已选”，选中状态使用统一描边、浅底和勾选。
- 交互区域不小于 44px，兼容安全区、320/375/390/430px 窄屏和 WXSS 限制。
- 不修改既有业务请求、状态机、任务提交和媒体读回链路。
- 工作区首页、品牌顶栏、外层液态玻璃背景和动态壁纸不做结构改版。
- 保留当前工作区中的未提交业务修改，不执行 reset、clean 或覆盖式重构。

---

### Task 1: 统一主题 token 与基础控件契约

**Files:**
- Modify: `src/ui/tokens.ts`
- Modify: `src/ui/system.scss`
- Modify: `src/app.scss`
- Test: `src/ui/tokens.test.ts`
- Test: `src/theme.test.ts`
- Test: `src/ui/wxssCompatibility.test.ts`

**Interfaces:**
- Produces the stable CSS variables `--ui-canvas`, `--ui-surface`, `--ui-surfaceRaised`, `--ui-text`, `--ui-textMuted`, `--ui-divider`, `--ui-controlBorder`, `--ui-accent`, `--ui-accentSoft`, `--ui-onAccent`, `--ui-success`, `--ui-warning`, `--ui-error`, `--ui-info`, spacing, radius and control-height variables.
- Produces shared classes `.mp-card`, `.mp-primary`, `.mp-secondary`, `.mp-input`, `.mp-textarea`, `.mp-picker-field`, `.mp-status-pill`, `.mp-inline-error`, `.mp-bottom-dock`, `.mp-drawer`.

- [ ] **Step 1: Add failing token assertions**

  Extend `src/ui/tokens.test.ts` to assert both themes expose the same variable keys, that the light canvas is distinct from the light raised surface, that dark surfaces preserve a readable contrast order, and that spacing/radius/control sizes are identical across themes.

- [ ] **Step 2: Run the focused token tests**

  Run: `npx vitest run src/ui/tokens.test.ts src/theme.test.ts`

  Expected: the new shared-key and surface-order assertions fail before implementation.

- [ ] **Step 3: Implement the canonical token map**

  Update `uiVariables()` so theme-specific values are limited to palette entries while spacing, type scale, radius and control heights are shared. Add semantic variables for `--ui-surfaceRaised`, `--ui-accentSoft`, `--ui-focusRing`, `--ui-overlay`, and `--ui-dock`.

- [ ] **Step 4: Replace duplicate base declarations**

  Move common card, field, status, drawer and bottom-dock declarations into `src/ui/system.scss`; remove conflicting page-wide background and control declarations from `src/app.scss` while preserving page-specific positioning rules and the shared outer liquid-glass layer.

- [ ] **Step 5: Run compatibility and token tests**

  Run: `npx vitest run src/ui/tokens.test.ts src/theme.test.ts src/ui/wxssCompatibility.test.ts`

  Expected: PASS with no unsupported WXSS selectors or properties introduced.

- [ ] **Step 6: Commit the shared foundation**

  ```bash
  git add src/ui/tokens.ts src/ui/system.scss src/app.scss src/ui/tokens.test.ts src/theme.test.ts src/ui/wxssCompatibility.test.ts
  git commit -m "refactor: unify mini-program theme tokens and controls"
  ```

### Task 2: 建立统一页面骨架与渐进披露交互

**Files:**
- Create: `src/ui/WorkflowShell.tsx`
- Create: `src/ui/WorkflowShell.test.tsx`
- Create: `src/ui/BottomDrawer.tsx`
- Create: `src/ui/BottomDrawer.test.tsx`
- Modify: `src/ui/system.scss`
- Modify: `src/theme.tsx`

**Interfaces:**
- `WorkflowShell({ title, description, steps, activeStep, summary, children, secondaryAction, primaryAction })` renders the shared flow layout without owning business state.
- `BottomDrawer({ open, title, onClose, onApply, children })` owns overlay, safe-area padding, cancel/apply affordances and focus dismissal; callers own draft values.

- [ ] **Step 1: Write failing component contract tests**

  In `WorkflowShell.test.tsx`, render a shell and assert title, step labels, summary slot, one primary action and one secondary action are present. In `BottomDrawer.test.tsx`, assert closed drawers render no overlay, open drawers render title and both cancel/apply callbacks, and cancel does not invoke apply.

- [ ] **Step 2: Run focused component tests**

  Run: `npx vitest run src/ui/WorkflowShell.test.tsx src/ui/BottomDrawer.test.tsx`

  Expected: FAIL because the components do not exist.

- [ ] **Step 3: Implement the shell and drawer**

  Implement the components with Taro `View`, `Text`, `Button` primitives. Keep all state outside the components; only drawer visibility and callback wiring belong to the presentation layer.

- [ ] **Step 4: Add responsive and safe-area styles**

  Add `.mp-workflow-shell`, `.mp-workflow-content`, `.mp-workflow-summary`, `.mp-bottom-dock`, `.mp-drawer-mask`, `.mp-drawer-panel` to `system.scss`. Use the existing theme variables and `env(safe-area-inset-bottom)`; avoid fixed content heights.

- [ ] **Step 5: Run component and theme tests**

  Run: `npx vitest run src/ui/WorkflowShell.test.tsx src/ui/BottomDrawer.test.tsx src/theme.test.ts`

  Expected: PASS.

- [ ] **Step 6: Commit the layout primitives**

  ```bash
  git add src/ui/WorkflowShell.tsx src/ui/WorkflowShell.test.tsx src/ui/BottomDrawer.tsx src/ui/BottomDrawer.test.tsx src/ui/system.scss src/theme.tsx
  git commit -m "feat: add shared workflow shell and bottom drawer"
  ```

### Task 3: 迁移剧本生成与其他功能内部页面

**Files:**
- Modify: `src/pages/module-detail/ScriptWorkspace.tsx`
- Modify: `src/pages/module-detail/FetchWorkspace.tsx`
- Modify: `src/pages/module-detail/AgentWorkspace.tsx`
- Modify: `src/pages/module-detail/PanelWorkspace.tsx`
- Modify: `src/pages/module-detail/index.tsx`
- Modify: `src/pages/functions/functions-studio.scss`
- Modify: `src/pages/functions/functions-glass.scss`
- Test: existing interaction and request tests under `src/pages/module-detail/**`

**Interfaces:**
- Consumes `WorkflowShell`, `BottomDrawer` and canonical `mp-*` classes from Tasks 1–2.
- Preserves existing navigation callbacks, theme switching and API calls.

- [ ] **Step 1: Add structural assertions to page tests**

  Extend existing tests to assert each primary page renders a single primary action, uses the shared summary/card classes, and does not render duplicated action labels for the same choice.

- [ ] **Step 2: Run the page tests before migration**

  Run: `npx vitest run src/pages/functions/functions.interaction.test.tsx src/components/BrandHeader.test.ts src/pages/profile/profile-palette.test.tsx`

  Expected: existing tests pass; new structural assertions fail until migration.

- [ ] **Step 3: Migrate feature-internal markup to shared primitives**

  Replace only feature-internal title/card/button wrappers with `WorkflowShell`, `.mp-card`, `.mp-primary`, `.mp-secondary`, `.mp-status-pill` and summary rows. Keep the existing outer brand header and liquid-glass background layers unchanged.

- [ ] **Step 4: Remove conflicting page-level control colors**

  In the listed SCSS files, replace hardcoded control backgrounds, borders and text colors with shared variables. Keep only decorative hero art and layout-specific geometry.

- [ ] **Step 5: Run focused page tests and build**

  Run: `npx vitest run src/pages/functions/functions.interaction.test.tsx src/components/BrandHeader.test.ts src/pages/profile/profile-palette.test.tsx && npm run build:weapp`

  Expected: PASS and a regenerated `dist/` bundle.

- [ ] **Step 6: Commit the main navigation migration**

  ```bash
  git add src/pages/module-detail src/pages/functions/functions-studio.scss src/pages/functions/functions-glass.scss
  git commit -m "refactor: streamline feature-internal layouts"
  ```

### Task 4: 迁移小说、剧本与批量工厂工作流

**Files:**
- Modify: `src/pages/module-detail/index.tsx`
- Modify: `src/pages/module-detail/ScriptWorkspace.tsx`
- Modify: `src/pages/module-detail/FetchWorkspace.tsx`
- Modify: `src/pages/module-detail/AgentWorkspace.tsx`
- Modify: `src/pages/module-detail/PanelWorkspace.tsx`
- Modify: `src/pages/module-detail/script-studio.scss`
- Modify: `src/pages/module-detail/fetch-studio.scss`
- Modify: `src/pages/batch/index.tsx`
- Modify: `src/pages/batch/mobile.scss`
- Test: existing interaction/request tests under `src/pages/module-detail/**` and `src/pages/batch/**`

**Interfaces:**
- Consumes shared workflow shell and drawer APIs.
- Preserves existing picker ranges, request payloads, idempotency keys, draft semantics and retry behavior.

- [ ] **Step 1: Add tests for progressive disclosure**

  Extend workflow tests to assert model/configuration controls are hidden until the advanced-settings entry is opened, and that applying the drawer preserves the selected value in the summary row.

- [ ] **Step 2: Run the affected request and interaction tests**

  Run: `npx vitest run src/pages/module-detail src/pages/batch`

  Expected: existing behavior tests pass; new progressive-disclosure assertions fail before migration.

- [ ] **Step 3: Migrate each workflow page without changing service calls**

  Wrap each stage in `WorkflowShell`; convert repeated button stacks into one primary action plus secondary text actions; move model, aspect, schedule and advanced flags into `BottomDrawer` while keeping draft state local to the page.

- [ ] **Step 4: Normalize batch controls**

  Convert batch list filters, output settings and book selection to compact rows with one visible summary. Keep list virtualization/scroll behavior unchanged.

- [ ] **Step 5: Run tests, typecheck and build**

  Run: `npx vitest run src/pages/module-detail src/pages/batch`

  Then run: `npx tsc --noEmit --pretty false`

  Expected: workflow tests and build pass; any type errors are recorded with exact file and line instead of being masked. Finish with: `npm run build:weapp`.

  Expected: workflow tests and build pass; any remaining type errors must be documented as pre-existing and unrelated.

- [ ] **Step 6: Commit workflow migration**

  ```bash
  git add src/pages/module-detail src/pages/batch
  git commit -m "refactor: streamline workflow and batch layouts"
  ```

### Task 5: 迁移私人定制漫画页面

**Files:**
- Modify: `src/comic/styles/comic.scss`
- Modify: `src/comic/pages/comic-new/index.tsx`
- Modify: `src/comic/pages/comic-new/ComicSettingsStage.tsx`
- Modify: `src/comic/pages/comic-new/ComicAssetsStage.tsx`
- Modify: `src/comic/pages/comic-new/ComicConfigConfirmStage.tsx`
- Modify: `src/comic/pages/comic-result/index.tsx`
- Modify: `src/comic/pages/comic-config/index.tsx`
- Test: `src/comic/pages/comic-new/index.test.tsx`
- Test: `src/comic/pages/comic-config/index.test.tsx`
- Test: `src/comic/pages/comic-result/index.test.tsx`

**Interfaces:**
- Preserves the existing comic stage state machine, image receipt gating, aspect ratio propagation and result navigation.
- Replaces large choice cards with compact choices and uses `BottomDrawer` for aspect, style, reference and advanced settings.

- [ ] **Step 1: Add layout behavior tests**

  Assert the settings stage renders one visible story input, one current-selection summary, compact aspect choices, and a single primary continue action. Assert advanced options are absent until opened and are applied back to the summary.

- [ ] **Step 2: Run comic tests before migration**

  Run: `npx vitest run src/comic/pages/comic-new/index.test.tsx src/comic/pages/comic-config/index.test.tsx src/comic/pages/comic-result/index.test.tsx`

  Expected: existing state-machine tests pass; new layout assertions fail.

- [ ] **Step 3: Migrate comic settings and asset stages**

  Keep story extraction and asset calls unchanged. Replace repeated `comic-choice` blocks with compact rows, one summary row for current project/book/style/aspect, and a drawer for the full option list.

- [ ] **Step 4: Align result and config pages**

  Use the same stage header, card surface, navigation footer and status components as the new flow. Preserve image candidate selection, page navigation and export actions.

- [ ] **Step 5: Run comic tests and build**

  Run: `npx vitest run src/comic/pages/comic-new src/comic/pages/comic-config src/comic/pages/comic-result && npm run build:weapp`

  Expected: PASS with unchanged service payload assertions and regenerated `dist/`.

- [ ] **Step 6: Commit comic migration**

  ```bash
  git add src/comic/styles/comic.scss src/comic/pages/comic-new src/comic/pages/comic-result src/comic/pages/comic-config
  git commit -m "refactor: streamline private comic workflow layout"
  ```

### Task 6: 双主题、窄屏与视觉验收

**Files:**
- Modify: `src/ui/tokens.test.ts`
- Modify: `src/ui/wxssCompatibility.test.ts`
- Modify: relevant page tests for 320/375/390/430px assumptions
- Verify: `dist/release-manifest.json`

- [ ] **Step 1: Add theme parity assertions**

  Assert every shared component class has light and dark token coverage, both themes preserve readable button/input/status contrast, and no page-specific theme selector overrides the canonical control variables.

- [ ] **Step 2: Run the full test suite**

  Run: `npx vitest run`

  Expected: all relevant tests pass; unrelated pre-existing failures are listed with exact file and line.

- [ ] **Step 3: Build the WeChat bundle**

  Run: `npm run build:weapp`

  Expected: build succeeds and `dist/release-manifest.json` records the current commit identity.

- [ ] **Step 4: Inspect narrow-width and theme surfaces**

  Verify 320/375/390/430px widths for horizontal overflow, bottom-dock safe-area spacing, drawer scrollability, long labels and selected-state visibility in both themes.

- [ ] **Step 5: Record visual acceptance boundaries**

  Record whether checks were performed in WeChat Developer Tools, whether authenticated/provider-backed pages were opened, and which visual states remain unverified. Do not claim complete visual parity from build output alone.

- [ ] **Step 6: Commit verification notes**

  ```bash
  git add src/ui/tokens.test.ts src/ui/wxssCompatibility.test.ts docs/superpowers
  git commit -m "test: verify unified mini-program ui across themes"
  ```
