# 网页 UI 全量对标实施计划

> **For agentic workers:** Execute inline in this dirty working tree. Do not commit, push, or reset existing work. Every task uses its own red-green verification cycle.

**Goal:** 让微信小程序的全部已实现页面与 `http://127.0.0.1:5176/` 的移动网页在信息层级、布局、图标语义和状态表面上对齐，同时保留真实服务端行为。

**Architecture:** 新增独立的本地图标目录和 `AppIcon` 呈现组件，所有页面只通过语义图标名取用同一份映射。将页面外观规则收敛到共享样式，再逐页替换文字图标和非网页参考的主视觉；请求、会话和状态派生函数不改动。

**Tech Stack:** Taro 4, React 18, TypeScript, WXSS, Vitest, 微信小程序静态资源。

## Global Constraints

- 网页是视觉参考，不是小程序 API；所有数据仍来自既有服务端接口。
- 图标只使用本地静态资源，不新增网络请求、字体依赖或浏览器存储。
- 不复制网页中的静态微信胶囊，保留微信系统状态栏、胶囊和 TabBar。
- 现有工作树含未提交改动；只增量编辑本计划列出的文件，不提交、不推送、不重置。
- 每一项完成后运行对应测试；最终运行 `npm test`、`npx tsc --noEmit`、`npm run build:weapp`。

---

### Task 1: 本地图标系统与 TabBar 图标

**Files:**
- Create: `src/assets/icons/*.png`
- Create: `src/components/AppIcon.tsx`
- Create: `src/components/AppIcon.test.ts`
- Modify: `src/app.config.ts`
- Modify: `src/app.config.test.ts`

**Interfaces:**
- Produces: `type AppIconName` and `<AppIcon name size tone className />`.
- Consumes: 本地图标路径；不读取 API 或页面状态。

- [ ] **Step 1: 写失败测试**

```ts
test("语义图标映射覆盖网页一级导航和六个模块", () => {
  expect(APP_ICON_NAMES).toEqual(expect.arrayContaining([
    "home", "folder-clock", "notebook-tabs", "user-round",
    "file-pen-line", "book-open", "clapperboard", "bot", "factory",
  ]));
});

test("TabBar 为四个一级页面声明普通和选中图标", () => {
  for (const item of appConfig.tabBar.list) {
    expect(item.iconPath).toMatch(/^assets\/icons\//);
    expect(item.selectedIconPath).toMatch(/^assets\/icons\//);
  }
});
```

- [ ] **Step 2: 运行测试，确认当前失败**

Run: `npm test -- src/components/AppIcon.test.ts src/app.config.test.ts`

Expected: 图标组件和 TabBar 图标路径尚不存在。

- [ ] **Step 3: 生成网页同名 Lucide 的本地 PNG 图标，并实现映射组件**

```tsx
export type AppIconName = "home" | "folder-clock" | "notebook-tabs" | "user-round" | "file-pen-line" | "book-open" | "clapperboard" | "bot" | "factory" | "crown" | "shield-check" | "key-round" | "chart" | "sun" | "settings" | "arrow-left" | "chevron-right" | "refresh" | "camera";

export default function AppIcon({ name, size = 20, className = "" }: AppIconProps) {
  return <Image className={className} src={ICONS[name]} mode="aspectFit" style={{ width: `${size}px`, height: `${size}px` }} />;
}
```

为 TabBar 添加 `iconPath` 与 `selectedIconPath`，普通态使用灰色图标，选中态使用网页品牌绿色图标。

- [ ] **Step 4: 运行测试，确认通过**

Run: `npm test -- src/components/AppIcon.test.ts src/app.config.test.ts`

Expected: 所有图标映射与四个 TabBar 图标断言通过。

### Task 2: 共享移动工作台表面与顶部操作

**Files:**
- Modify: `src/app.scss`
- Modify: `src/components/BrandHeader.tsx`
- Modify: `src/components/BrandHeader.test.ts`

**Interfaces:**
- Consumes: `AppIcon` 的 `arrow-left`。
- Produces: 可复用的顶栏、标题行、卡片、分段控件和图标按钮样式。

- [ ] **Step 1: 写失败测试**

```ts
test("自定义顶栏以图标呈现返回操作而非 Unicode 字符", () => {
  const source = readFileSync(resolve(__dirname, "BrandHeader.tsx"), "utf8");
  expect(source).toContain('<AppIcon name="arrow-left"');
  expect(source).not.toContain("‹");
});

test("共享表面使用网页的紧凑卡片和 8 到 12rpx 圆角", () => {
  const styles = readFileSync(resolve(sourceRoot, "app.scss"), "utf8");
  expect(styles).toContain(".mp-surface-card");
  expect(styles).not.toContain("border-radius: 28rpx");
});
```

- [ ] **Step 2: 运行测试，确认当前失败**

Run: `npm test -- src/components/BrandHeader.test.ts`

Expected: 顶栏仍输出文本返回箭头，网页对标表面样式尚不存在。

- [ ] **Step 3: 实现共享表面和顶部图标操作**

在 `BrandHeader` 使用 `AppIcon` 呈现返回；保留冷启动回工作区的回退逻辑。将共享卡片、标题行、筛选条和图标容器的圆角、边框、间距收敛为网页移动端规则。

- [ ] **Step 4: 运行测试，确认通过**

Run: `npm test -- src/components/BrandHeader.test.ts`

Expected: 顶栏图标、系统胶囊去重、返回回退和滚动约束全部通过。

### Task 3: 一级页面工作区、任务与功能

**Files:**
- Modify: `src/pages/workspace/index.tsx`
- Modify: `src/pages/tasks/index.tsx`
- Modify: `src/pages/functions/index.tsx`
- Modify: `src/pages/primary-navigation.test.ts`
- Modify: `src/config/modules.ts`

**Interfaces:**
- Consumes: `AppIconName` 与 `MODULES[*].icon`。
- Produces: 工作区快捷入口、任务来源、功能卡不再使用 Unicode 图标。

- [ ] **Step 1: 写失败测试**

```ts
test("六个功能模块使用网页同名图标", () => {
  expect(MODULES.map((module) => module.icon)).toEqual([
    "file-pen-line", "book-open", "notebook-tabs", "clapperboard", "bot", "factory",
  ]);
});

test("一级页不再以 Unicode 功能字符渲染图标", () => {
  for (const page of ["workspace/index.tsx", "tasks/index.tsx", "functions/index.tsx"]) {
    expect(readFileSync(resolve(pagesRoot, page), "utf8")).not.toMatch(/[✦⚡◎▤▥▦◌]/);
  }
});
```

- [ ] **Step 2: 运行测试，确认当前失败**

Run: `npm test -- src/pages/primary-navigation.test.ts src/config/modules.test.ts`

Expected: 模块未定义 `icon`，三个页面仍含文本图标。

- [ ] **Step 3: 实现三页网页布局**

工作区改为紧凑标题和功能行；任务保留四来源真实汇总、筛选与进度，仅替换为网页任务卡层级和刷新图标；功能页改为六张连续工具卡，使用模块图标、当前标签和右箭头图标。

- [ ] **Step 4: 运行测试，确认通过**

Run: `npm test -- src/pages/primary-navigation.test.ts src/config/modules.test.ts`

Expected: 图标映射、一级页结构和真实状态入口断言通过。

### Task 4: 我的、个人中心详情和绑定页

**Files:**
- Modify: `src/config/profile.ts`
- Modify: `src/pages/profile/index.tsx`
- Modify: `src/pages/profile-detail/index.tsx`
- Modify: `src/pages/bind/index.tsx`
- Modify: `src/config/profile.test.ts`

**Interfaces:**
- Consumes: `AppIconName`、服务端会员角色与现有 `PROFILE_SECTIONS`。
- Produces: 网页同语义图标的菜单项和角色可见性。

- [ ] **Step 1: 写失败测试**

```ts
test("个人中心菜单采用网页语义图标名", () => {
  expect(PROFILE_SECTIONS.flatMap((section) => section.items).map((item) => item.icon)).toEqual(expect.arrayContaining([
    "user-round", "crown", "shield-check", "key-round", "chart", "sun", "settings",
  ]));
});

test("个人中心和绑定页不含 Unicode 功能图标", () => {
  for (const source of [profilePage, bindPage]) expect(source).not.toMatch(/[♙♕⌕☼⚙⌾›]/);
});
```

- [ ] **Step 2: 运行测试，确认当前失败**

Run: `npm test -- src/config/profile.test.ts src/components/BrandHeader.test.ts`

Expected: 配置图标仍是字符，头像与列表箭头仍是文本。

- [ ] **Step 3: 实现网页个人中心层级**

将菜单图标改为 `AppIcon`；将头像编辑、详情箭头、退出和绑定表单改为图标辅助的工作台行。保留服务端角色的成员、治理和开发者入口，缺失数据继续显示真实缺失状态。

- [ ] **Step 4: 运行测试，确认通过**

Run: `npm test -- src/config/profile.test.ts src/components/BrandHeader.test.ts`

Expected: 角色分组、图标语义和绑定返回入口断言通过。

### Task 5: 六个工作区详情页

**Files:**
- Modify: `src/pages/module-detail/index.tsx`
- Modify: `src/pages/module-detail/ScriptWorkspace.tsx`
- Modify: `src/pages/module-detail/FetchWorkspace.tsx`
- Modify: `src/pages/module-detail/PanelWorkspace.tsx`
- Modify: `src/pages/module-detail/ShuihuoWorkspace.tsx`
- Modify: `src/pages/module-detail/AgentWorkspace.tsx`
- Modify: `src/pages/batch/index.tsx`
- Modify: `src/pages/module-detail/script-workspace-visual.test.ts`

**Interfaces:**
- Consumes: 模块图标映射、`AppIcon` 和现有服务端请求函数。
- Produces: 六个工作区统一的标题区、图标操作、标签和真实状态卡。

- [ ] **Step 1: 写失败测试**

```ts
test("每个工作区引用对应模块图标并保留真实状态入口", () => {
  for (const [page, icon] of Object.entries({
    "ScriptWorkspace.tsx": "file-pen-line",
    "FetchWorkspace.tsx": "book-open",
    "PanelWorkspace.tsx": "notebook-tabs",
    "ShuihuoWorkspace.tsx": "clapperboard",
    "AgentWorkspace.tsx": "bot",
  })) {
    const source = readFileSync(resolve(workspaceRoot, page), "utf8");
    expect(source).toContain(`name=\"${icon}\"`);
    expect(source).not.toMatch(/[✦⚡◎▤▥▣♧◖]/);
  }
});
```

- [ ] **Step 2: 运行测试，确认当前失败**

Run: `npm test -- src/pages/module-detail/script-workspace-visual.test.ts`

Expected: 工作区仍有 Unicode 图标且未共享网页语义图标。

- [ ] **Step 3: 实现详情页视觉对齐**

每个页面保留已有表单、选项、任务、错误和重试行为；替换页面顶部英雄卡为网页的紧凑功能头部，替换操作符号为图标，统一分段控件和结果卡样式。批量工厂使用 `Factory`，模块分发页从 `MODULES` 获取图标而不重复定义。

- [ ] **Step 4: 运行测试，确认通过**

Run: `npm test -- src/pages/module-detail/script-workspace-visual.test.ts`

Expected: 六个工作区图标、编辑/结果状态和真实 API 调用边界断言通过。

### Task 6: 全量验证与微信开发者工具验收

**Files:**
- Verify only: `dist/`

- [ ] **Step 1: 运行完整验证**

Run: `npm test && npx tsc --noEmit && npm run build:weapp && git diff --check`

Expected: 所有 Vitest 测试、TypeScript 和 Taro 微信构建通过，差异无空白错误。

- [ ] **Step 2: 检查产物**

Run: `rg -n 'navigationStyle|mp-capsule|[✦⚡◎▤▥▦♙♕⌕☼⚙⌾]' dist --glob '*.{json,js,wxml,wxss}'`

Expected: `dist/app.json` 保留 `navigationStyle: custom`；无 `mp-capsule` 和功能 Unicode 图标。

- [ ] **Step 3: 微信开发者工具人工验收**

在根目录 `dist/` 的项目中点击“编译”，检查工作区、任务、功能、我的、剧本生成、绑定页；确认顶部无原生标题重复、图标清晰、长内容可滚动、详情页可返回，且真实服务端错误或空状态仍可见。
