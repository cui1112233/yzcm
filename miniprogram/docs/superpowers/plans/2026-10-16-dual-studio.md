# 双主题手机工作台 Implementation Plan

**Goal:** 将已确认 A/B 方案接入小程序既有 dark/light 主题，保留真实功能。

**Architecture:** 单一业务组件树配合语义颜色变量和主题布局差异，不复制页面，不按主题设置 React key。复用既有 API、能力判断、确认流程与媒体读取。

**Tech Stack:** Taro 4.2.1、React18、TypeScript、SCSS、Vitest。

## Constraints

只修改本地客户端；不部署、不提交推送、不运行生产任务；保留已有未提交修改。用户已通过比较原型确认两套均接入，无需重新选择方向。

### 1. 配色

- [ ] 修改 `src/ui/tokens.ts`：暖白墨绿/石墨珊瑚橙；增加次级表面与弱主色。正文、状态色和按钮对比度仍达标。
- [ ] 原生 Tab 选中图标同步颜色，验证 `src/ui/tokens.test.ts` 与 `src/theme.test.ts`。

### 2. 展示组件

- [ ] 修改 `PersonalWorksView.tsx`：独立新建入口、展开搜索、状态归并标题区。
- [ ] 修改 `BatchProjectView.tsx`/`NovelListView.tsx`：单份列表、紧凑元数据与真实阶段状态，保持内部 ID 导航。
- [ ] 修改 `BookWorkbenchView.tsx`：媒体先于标签、制作工具弹层、资产网格及复制。保留真实视频读取、原文、合成提交确认与禁用原因。
- [ ] 修改 `mobile.scss`：布局匹配对比稿，统一44px命中区、安全区及两主题专属样式，不隐藏错误和业务入口。

### 3. 验证与交付

- [ ] 运行 `npm test -- --run`、`npx tsc --noEmit`、`npm run build:weapp`。
- [ ] 隔离样例使用实际页面组件，在两个主题四个宽度检查导航、表单、提示词、资产、滚动、安全区；阻止公网请求。
- [ ] 使用真实 React DOM 主题切换验证保留书/分镜/标签及输入值。
- [ ] 提供实际组件截图与新 dist 位置；不以构建代替微信模拟器和真实播放验收。
