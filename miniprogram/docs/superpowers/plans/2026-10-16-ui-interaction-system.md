# UI Interaction System Implementation Plan

> **For agentic workers:** Use executing-plans inline. Preserve the user's current local checkout and unfinished features. No Git operations, public deployment, server writes or paid tasks.

**Goal:** 将已确认的UI规范落到本地主题、公共控件和批量工厂四个核心页面。

**Architecture:** 用 `src/ui/tokens.ts` 同时为页面CSS变量与原生导航提供颜色。公共组件样式由 `src/ui/system.scss` 管理，批量工厂 `mobile.scss` 合并为单一页面规则，保留现有API及页面状态。

**Tech Stack:** Taro 4.2.1、React 18、TypeScript、SCSS、Vitest、微信wcsc、隔离浏览器布局验收。

## Global Constraints

- 所有新增令牌尺寸使用逻辑px；运行时CSS变量不再经Taro二次转换。
- 色彩采用规范中的深浅色对，正文16/26px、控件14/20px、辅助12/18px，触控至少44px。
- 不修改公网、密码、接口契约、任务执行逻辑，不提交推送。
- 高级动效只定义条件；不新增业务不需要的时间轴、视差或排序。
- 保留已有确认、去重、账号书籍边界、视频读取和真实空状态。
- 本轮优先完成基础系统与批量四页；其他模块专用布局只继承公共基础，不声称全站专用组件均已迁移。

### Task 1: 统一令牌和主题

Files: create `src/ui/tokens.ts`, `src/ui/system.scss`; modify `src/theme.tsx`, `src/app.scss`, `src/app.tsx`, `src/app.config.ts`, `src/theme.test.ts`; create `src/ui/nativeTabIcons.ts` and 16 local icon variants.

Interface: `uiPalette(theme)`返回语义颜色；`uiVariables(theme)`返回CSS自定义属性；theme保持原有存储及订阅API。

- [x] 添加并运行真实前景背景对比测试，普通文字≥4.5、控件边界≥3。
- [x] 在每个ThemedPage根应用变量，原生导航和tabbar复用同一调色板。
- [x] 公共按钮、文字、输入、卡片、图标、品牌栏使用统一令牌；背景装饰退出生产界面。
- [x] 检查主题切换原测试及类型检查，不改主题偏好存储格式。

### Task 2: 批量四页统一

Files: replace duplicate declarations in `src/pages/batch/mobile.scss`; modify `PersonalWorksView.tsx`, `BookWorkbenchView.tsx` only for semantic controls and readable media.

- [x] 合并重复页面样式，字号、间距、边框、状态色全部采用语义变量。
- [x] 作品行、上方选书、配置面板、导入启动方式保持原业务结构；小屏切为纵向选项避免挤压。
- [x] 页签和筛选使用真实按钮，保留原onClick和选中逻辑；资产使用aspectFit；原文不限制固定阅读高度。
- [x] 播放器按视频元数据比例适配，保留原鉴权加载、失效重试和分镜同步。
- [x] 运行现有批量链路和单书交互测试。

### Task 3: 底栏、阅读与动效

Files: create `src/components/OperationDock.tsx`; modify batch/index and BookWorkbenchView; modify theme and appearance only if needed for reduced-motion preference.

- [x] 底栏测量自身实际高度含安全区，并提供等高占位＋16px；尺寸变化重新测量、卸载清理。
- [x] 正文提示词提供可选择文本和复制；原文提供16/18/20px阅读字号。复制仅用户点击执行，不触发业务写入。
- [x] 全局默认取消背景装饰循环；仅保留短时按钮与选中反馈；系统减少动效时取消装饰过渡。
- [x] 无新增循环装饰时不提供无作用的设置；高级跟随能力保持未启用状态。

### Task 4: 验证与交付

- [x] `npm test -- --reporter=dot`，`npx tsc --noEmit`，`git diff --check`。
- [x] `npm run build:weapp`；微信wcsc编译app与batch样式。
- [x] 隔离布局环境禁止公网请求及业务写入，检查深浅主题四页、320/375/390/430宽度、长文本、按钮高度、页面溢出、操作栏占位、配置与启动方式入口。
- [x] 截图保存当前workspace，记录实际测试数字及未验收边界；打开新截图供用户查看。
- [x] 明确真实账号视频播放和真机验收仍须实际微信环境，不以布局样例替代。
