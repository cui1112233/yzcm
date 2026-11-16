# 全功能公网客户端 Implementation Plan

> **For agentic workers:** 使用 subagent-driven-development 实施独立模块；保留当前用户指定目录中的本地修改，不提交、不推送、不部署。

**Goal:** 使用已有账号与业务服务实现手机端真实工作流；覆盖登录、工作区、剧本、获取、面板、水货、Agent、批量、任务和个人中心。

**Architecture:** 手机端调用现有 HTTP API，不新增业务后端。普通账号密码登录与微信绑定解耦，统一会话、超时、错误、媒体隔离。每个模块遵循现有网页 API 的参数、状态及持久化边界。

**Tech Stack:** Taro 4.2.1、React 18、TypeScript、Vitest。

## Global Constraints

- 仅修改 `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram` 本地客户端；公网服务、远端仓库、后端及账号库不修改。
- 保留用户现有修改和已确认的手机布局，不重置、不提交、不推送、不部署。
- 验证不创建公网会话或生产任务；不得触发收费生成、合成或提交。生产写保护保持，客户端可实现受保护的操作链路。
- 凭证不打印、不写入报告或源码；密码只用于登录请求，不存储。来源域名之间的会话和媒体必须隔离。
- 参考网页源码 `/private/tmp/qiantie-v88-public-source/frontend/src/shared/api` 仅作契约证据；公网实际资源和认证 API 为运行证据。未知返回不能伪装为空或成功。
- 本轮不以构建或模拟响应代替真实账号/媒体/提供方验收；文档逐模块记录未验证边界。

### Task 1: 全局账号、会话和媒体稳定性

**Files:** src/services/session.ts、api.ts、mediaCache.ts、src/pages/bind/index.tsx、src/pages/workspace/index.tsx、src/config/localAuth.ts 及对应测试。

**Interfaces:** `loginAccount(username,password,mfaCode?)` 复用 POST /api/login，随后 GET /api/login/session 验证账号；`logoutAccount()` 调用现有 logout 并清理当前会话；既有 loginLocalAccount 保持本地限制。

- [ ] 回归测试先覆盖公网账号直登、MFA 透传、登录后身份校验失败不保存令牌、旧 401 不清新令牌、普通业务 401 不触发微信、跨账号媒体不能复用。
- [ ] 复用现有登录端点；域名仅允许 HTTPS 或明确 loopback；支持服务端 MFA 错误，保留微信函数但不自动调用。
- [ ] 通用请求有 15 秒默认超时，重试仅用户显式操作，不自动重放业务写入；401 仅会话错误清理匹配的旧令牌。
- [ ] 登录页改为所有服务均可输入账号密码，显示当前服务；工作区由真实会话校验决定 ready，失效回登录。
- [ ] 媒体按源站和账号会话隔离，退出/切换不复用旧缓存；晚到媒体不污染新会话。

### Task 2: 批量工厂完整客户端链路与读取稳定性

**Files:** src/services/batchFactoryV11.ts、batchLocalAccess.ts、src/pages/batch/ 下文件及测试。

**Interfaces:** 保留已有导出与页面路由；仅 GET/HEAD 的生产写保护保持，loopback 可在明确本地环境中调用已有业务。主任务不会操作 batch 文件。

- [ ] 先写失败回归覆盖单书分镜/资产读取失败显式显示、切换批次晚响应、导入顺序、保存配置刷新读回。
- [ ] 保留个人作品→新建→项目小说列表→单书层级；配置版本与自动化预设分别选择，复用 config-versions/settings 等既有 API。
- [ ] importBooks 使用已有 fetch-originals、intakes/manual、intakes/{id}/batches 契约；按实际参考代码执行顺序，不自行猜字段。立即/定时走已有 automation/start；写保护拦截时明确无请求发出。
- [ ] 单书错误按分镜/资产/图片/生产/合成区分，不吞为 null；切换和刷新不串书；避免重叠轮询。
- [ ] 现有 director/production/merge/publish intents→confirm→submit 仅接受真实回执，不使用“已记录”默认成功；生产写保护继续生效。
- [ ] 运行针对性 Vitest/tsc；不构建、不提交，不进行真实写入；报告本地测试与公网未验收边界。

### Task 3: Agent、任务与水货生产接口对应

**Files:** src/pages/module-detail/AgentWorkspace.tsx、ShuihuoWorkspace.tsx、src/services/agentStudio.ts、taskSummary.ts 及新建的聚焦 API 文件/测试。

**Interfaces:** 使用主任务的 apiRequest；主任务不会修改这些文件。已有布局类名保留。

- [ ] 先测试 Agent tasks/chat/canvas/executions 的真实字段与路径，不再使用公网不存在的 Studio 接口。
- [ ] 对照网页 agent.js 实现任务选择、新建、历史消息、提问、画布/执行结果读取；不把计划当执行。失败显式显示，旧响应不得覆盖新任务。
- [ ] 任务中心使用真实 Agent task 来源并补批量工厂、水货项目任务；点击可进入正确项目/模块，不将小说获取任务冒充批量任务。
- [ ] 水货读取自己的 projects、project detail、segments/assets/tasks，选中项目才显示工作台，接通已有创建/任务/结果入口与媒体；不再把所有项目跳去批量工厂。
- [ ] 运行针对性测试/tsc，不触发真实任务；报告未证实的响应和功能缺口，不虚报完成。

### Task 4: 剧本、小说获取、小说面板与个人中心

**Files:** ScriptWorkspace.tsx、FetchWorkspace.tsx、PanelWorkspace.tsx、profile-detail/index.tsx、scriptWorkspace.ts 和新建 member 服务/组件。

- [ ] 剧本对齐约束、visualStyle、protagonists、预设、原文/人物场景、配音和保存；失败或结构无效不得 JSON 字符串当成果。
- [ ] 小说获取维持已有读写链路，补提交/结果/调度真实字段；平台和模型从服务端选择，不造选项。过期读取不覆盖新选择。
- [ ] 小说面板选书原文/人物场景/分镜结果与参考资产读取和编辑/上传入口，沿用 /api/novel-panel/projects、draft、history 和 reference-assets 既有服务；刷新恢复选中的实体。契约核对纠正：普通 /api/history 是剧本历史，不是小说面板项目，禁止混用。
- [ ] 个人中心接入 member/security、team/governance 和角色对应 API；使用实际权限，未知状态不可伪装可用。普通设置仅设备本地。
- [ ] 针对真实行为补测试；无已验证契约的接口保留明确不可用状态，列入最终待验收清单。

### Task 5: 整体验证与交接

- [ ] npm test、npx tsc --noEmit、npm run build:weapp，使用原生 wcsc 验证 app/batch WXSS。
- [ ] 若没有公网凭证，只运行匿名 GET 与本地账号只读校验，记录运行环境和返回结构，不伪称公网业务通过。
- [ ] 独立复核会话、跨模块跳转、媒体和写保护；修复重要问题后重跑覆盖检查。
- [ ] 写逐模块清单，标明已实现客户端/已验证真实读取/待写回/待媒体播放/待微信真机验收。
