# 小程序媒体与进度修复实施计划

> 执行方式：本会话逐项执行；用户已确认“执行”。保留既有未提交内容，不提交、推送或部署。

**目标：** 修复已确认的媒体失败重试、错误丢失和未知状态被计为零进度的问题。

**架构：** 保留公网既有接口及鉴权；仅调整本地展示与当前会话的媒体缓存。已在微信模拟器验证「10151 / 言笑晏晏」现有视频可以显示画面并播放，不能将播放器无障碍回退文本作为失败证据。

**技术栈：** Taro 4.2.1、React 18、TypeScript、Vitest、微信开发者工具。

## 全局约束

- 工程为 `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram`，微信读取 `dist/`。
- 不改公网、密码、会话校验或参考网页；不启动模型生成、合成和提交。
- 不输出媒体签名 URL、Cookie、令牌或密码。
- 测试和构建通过不等于真实微信验收。

## 任务 1：可信进度

文件：`src/pages/batch/PersonalWorksView.tsx`、新增 `PersonalWorksView.render.test.tsx`。

- [x] 渲染真实组件的测试：缺失书状态、未知状态、只返回部分书时均不出现 `0/N` 或完成进度；完整已知状态保留真实完成数。
- [x] 先运行测试确认错误，再修改 `progressFor`：仅在列表完整且状态均可识别时统计，完成判断使用现有 `statusTone`。
- [x] 再运行测试，确认成功、失败、进行中及未知状态不会混淆。

## 任务 2：有效重试与可诊断错误

文件：`src/services/mediaCache.ts`、`mediaCache.test.ts`、`src/pages/batch/bookMedia.ts`、`bookMedia.test.ts`、`BookWorkbenchView.tsx`、`BookWorkbenchView.interaction.test.tsx`。

- [x] 缓存测试先复现：只使当前账号指定媒体缓存失效，再读取会重新下载，其他媒体保持可用；既有跨账号隔离测试通过。
- [x] 新增 `forgetAuthenticatedMedia(url: string): void`，按现有 URL、会话和 revision 缓存键清理对应文件。
- [x] 错误测试：保留下载/播放器错误内容及数字错误码，隐藏包含签名的 URL，不读取整个错误对象。
- [x] `mediaFailureMessage(cause: unknown, phase: "download" | "playback"): string` 提取公开错误字段并脱敏；下载 catch 与原生 onError 均使用。
- [x] 重新读取按钮先清理当前媒体缓存，再增加现有重试计数；只重读，不重新生成视频。
- [x] 针对播放器错误后点重试的用户操作补回归验证。

## 验证与交付

- [x] 运行相关 Vitest、TypeScript、`git diff --check`。
- [x] 运行 `npm run build:weapp`，核对产物变化。
- [x] 在微信模拟器确认作品列表不再伪报零进度，原有视频仍可播放，提示词/原文/资产仍可访问。
- [x] 记录真实检查结果与未验收边界，不宣称全部功能已对齐。
