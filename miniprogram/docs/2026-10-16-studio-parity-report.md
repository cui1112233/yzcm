# 微信小程序 Studio 契约修复

日期：2026-10-16。仅本地源码、模拟请求测试；没有真实服务器请求、登录、付费生成、上传、发布、提交、推送或部署。

## 已核对的两套独立契约

5176 参考：`/private/tmp/yizhanchengming-main-web-preview/frontend/src/modules/agent/AgentStudioWorkspace.jsx` 和 `agentStudioModel.js`。使用 `/api/studio/agent/projects`、`/api/studio/models`、`/api/studio/runs`、`/api/studio/assets`，请求带合法 Cookie，写请求带 `X-Studio-Request: 1`。项目包含 `id/name/revision/document`；节点/对话/事件/绑定属于 document，项目保留服务端版本。

按要求先读了 `/Users/ming/Documents/ChatGPT/yizhanchengming-xin/api/internal/agentstudio`、`authn` 和 `httpapi`：该项目实际是 `/api/v1/agent/projects`、canvas/messages/executions，使用 `/api/auth/login`、`ycm_access`、SameOrigin 与 capabilities。它不是参考 Studio 的接口，未拿来冒充相同系统。

另外只读核对 `/Users/ming/Downloads/小程序开发/yizhanchengming/server-main/backend/internal/studio/{http.go,types.go,agent_projects.go}`、`internal/httpapi/{auth.go,router.go,auth_middleware.go}` 与 `internal/config/config.go`：这里才有上述 `/api/studio/*`，项目写严格拒绝额外字段；默认 Cookie 名 `ycm_session`，既有登录 `POST /api/login`、身份 `GET /api/me`、退出 `POST /api/logout`。没有修改后端，也没有把该源码当作当前公网部署版本证明。

## 变更文件与行为

- `src/pages/module-detail/AgentWorkspace.tsx`：替换旧 `/api/agent/tasks/chat` 挂载，改为 Studio 项目列表、新建、读取、修改、版本保存读回、确认删除；节点列表、文本编辑、隐藏/锁定/移除、素材加入节点，以及保留未知字段的完整文档草稿编辑；模型目录、Agent 规划预览（不自动执行 plan.actions）、用户确认的图片/编辑图片/图生视频/先图后视频；原生受保护素材查看、图片上传、全部 Studio runs、已有项目对话；Brand Kit/Skills 列表、读取、新建/保存文档、项目绑定。
- `src/services/agentStudio.ts` 与测试：严格列表/项目响应；POST 项目、PUT 项目、同项目读回；保存只投影 id/name/revision/document，排除 GET 元数据；按参考字段构建媒体请求，缺模型、空文本和未确认等本地错误标记 unsubmitted。维护会话级 pendingRunKey：页面卸载/客户端重建不会解除未确认的媒体提交；仅匹配 request_key 的服务端运行回执清除，不用无关列表刷新解除。
- `src/services/session.ts` 与 `accountSession.test.ts`：只从合法账号登录响应捕获 Cookie，并在身份验证成功且用户名匹配后发布。Cookie 与精确服务 origin、当前 token、期限关联，只放内存，不生成或持久化伪 token，不保存密码。Cookie-only 登录可用；`/api/login/session` 明确 404 且存在实际 Cookie 时，才尝试已核对的 `/api/me`，401 不切换认证接口；按已验证契约选择退出接口。
- `src/services/api.ts`：同源 Studio 必须有合法 Cookie，缺失时明确不可用，禁止把 Bearer 伪造成 Cookie；加 `X-Studio-Request`，Studio 不发 Bearer；保留其他模块已有令牌请求。账号变化拒绝迟到响应；外部源不附带当前会话 Cookie/令牌，调用者不能通过 header 覆盖认证隔离；支持 Studio 嵌套 error.code/message。
- `src/services/mediaCache.ts` 与测试：原生 MP4 下载同源附带实际 Cookie/已有 Bearer，外部源不附带；缓存与 Cookie、token、sessionRevision 隔离，迟到媒体清理。
- `src/pages/workspace/index.tsx`、`src/pages/profile/index.tsx`：会话门同时支持合法 Cookie 和旧 token；`src/pages/bind/index.tsx` 已调用同一 loginAccount，不需要额外旁路。Cookie-only 应用重启内存消失后，需要重新登录；已有 Bearer 的账号也不能因此绕过 Studio Cookie 条件。
- `src/services/taskSummary.ts` 与对应测试：Agent 来源改为 `/api/studio/runs` 的 runs，而非另一套 agent/tasks；使用真实运行状态，不把项目数量当运行数量。
- `src/pages/module-detail/index.tsx`：Agent 传入 projectId 与 taskId。任务入口的 taskId 表示 Studio run，工作区标记该 run，不假设它是 project ID。
- `src/pages/module-detail/AgentWorkspace.requests.test.tsx`：真实路径、项目版本保存、规划不自动媒体、延迟详情防覆盖、未知运行不重放、确认期间切账号取消、快速连点单确认、本地校验不伪报未知。

## 后续复核修正

主代理的独立复核提出并已修复：确认期间跨账号写入/重复收费；完整 GET 项目元数据导致严格 PUT 拒绝；媒体未知提交丢失于页面卸载；本地校验被误标未知；Go Cookie 身份/退出路径与 legacy 路径不同。对应回归均先观察失败再修复。

所有确认/选图后的写入口，先同步取得交互锁并捕获 sessionRevision/目标 epoch，弹窗、选图和文件读取返回后再次核对；write 本身校验 ticket；每一个序列请求还受创建页面时的会话版本约束。不会用新账号凭据继续旧页面的付费操作。已受理项目写后的读回失败，即使 GET 是 403，也提示结果未知。

## 检查证据与准确剩余边界

11:34 最后专项命令：

`npx vitest run src/services/agentStudio.test.ts src/pages/module-detail/AgentWorkspace.requests.test.tsx src/services/accountSession.test.ts src/services/api.test.ts src/services/mediaCache.test.ts src/services/session.test.ts src/services/localSession.test.ts src/services/taskSummary.test.ts src/services/productionTaskSummary.test.ts`

9 个文件、62 项测试通过。`npx tsc --noEmit` 退出码 0。全量测试与生产构建由主代理统一执行。

这证明本地接线和隔离契约，不证明当前公网返回哪一种认证响应、当前账号具备 Studio 能力、Cookie-only 原生视频真实可播放或真实服务端已持久化。没有合法 Cookie 时即使其他 Bearer 模块能读，Studio 也明确不可用。需要微信开发者工具编译 dist 后核对真实账号、同项目离开重进、保存版本冲突、网站合法域名和媒体下载/播放。

当前保留手机版卡片布局，不声称复刻网页空间画布拖拽、框选、画笔、Touch Edit 蒙版或所有快捷键；这些字段可通过完整文档草稿保留/维护，不等于原生图形编辑体验。Brand Kit/Skills 当前是结构化文档维护，不包含网页全部专门表单/删除入口；项目已有对话可读，新的规划目前是预览结果，未宣称完整聊天生产体验。会话级 pendingRunKey 跨页面保留但不跨应用进程重启持久化；重启后的真实任务应先核对服务端运行记录。未将上述边界标记为已通过真实全功能验收。
