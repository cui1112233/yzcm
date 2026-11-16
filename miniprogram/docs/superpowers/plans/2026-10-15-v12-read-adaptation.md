# V12 客户端读取适配 Implementation Plan

> **For agentic workers:** 使用 executing-plans 在当前会话逐项实施。用户已授权执行；本轮不委派、不提交、不推送、不部署。

**Goal:** 保留移动端布局，复用网页 V12 读取接口，正确区分作品摘要、配置版本和自动化预设。

**Architecture:** 保留兼容模块名 batchFactoryV11；统一客户端 API 根路径为 V12。首页仅读取摘要；单书继续按需读取；配置版本单独读取并保留错误状态。

**Tech Stack:** Taro 4、React 18、TypeScript、Vitest。

## Global Constraints

仅修改本地小程序，保留已有未提交变更。公网和远端仓库禁止修改。所有批量写请求继续受 batchLocalAccess 保护。不创建测试作品，不触发提供方任务。

### Task 1: V12 读取契约

**Files:** src/services/batchFactoryV11.ts、src/services/batchFactoryV11.test.ts。

- [x] 添加回归测试：作品从 `/api/batch-factory/v12/batches/summary` 读取，保留 `bookCount: 7`，不请求详情；无效结构抛错而非伪装空列表。
- [x] 添加配置版本回归：`{configVersions:[{id:"system-default-v1",name:"System Default",config:{source:"system"}}]}` 从 `config-versions` 解析；网络错误原样传播。
- [x] 执行测试观察失败，再统一既有接口根路径、实现读取函数和小说数量函数。

### Task 2: 页面与配置语义

**Files:** src/pages/batch/PersonalWorksView.tsx、src/pages/batch/index.tsx、src/services/batchLocalAccess.ts。

- [x] 作品封面沿用原布局，小说数读取摘要计数，移除误导的“公网”加载文案。
- [x] 统一配置显示服务端配置版本及读取失败/空状态，不再显示为自动化预设或假装保存成功；自动化预设保留在新建流程。
- [x] 写保护提示明确当前连接服务，不改变任何写权限。

### Task 3: 验证

- [x] 本地真实账号登录后 GET V12 五个读取接口；输出仅状态和非敏感结构。
- [x] npm test、npx tsc --noEmit、npm run build:weapp。
- [x] 对 dist 的 app.wxss 和 pages/batch/index.wxss 使用微信原生编译器校验。
- [x] 明确空账号仅证明认证读取和空状态，单书真实媒体与模拟器验收仍待验证。

## 验证结果

2026-10-15：145 项测试通过；tsc、Taro 构建及微信原生 app/batch WXSS 编译通过。实际 TypeScript 适配器读取本地四项 V12 接口均为 200。当前为空账号，仅有一个全局配置版本，无自动化预设；未验证真实单书媒体、写回、提供方任务或模拟器渲染。
