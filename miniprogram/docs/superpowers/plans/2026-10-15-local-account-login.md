# Local account login implementation plan

Goal: 开发版连接本地真实账号后端；正式版微信登录保持原流程。

Constraints: 只修改本地小程序，不提交、不推送、不部署，不读取用户密码；保留批量业务写保护。

1. `src/config/localAuth.ts` 判定精确回环主机；测试拒绝非回环和伪造域名。读取源站使用已有 `apiUrl('/')`。
2. `src/services/session.ts` 增加 `loginLocalAccount(username,password)`，仅回环调用 `/api/login`，校验响应令牌，本地存储按 origin 隔离；先写失败测试，再实现。
3. `api.ts` 的本地 401 清除本地令牌、返回登录页，不调用微信、不自动重发。`workspace` 无本地令牌时打开登录页，`bind` 依环境选真实账号登录或微信绑定。测试本地登录及恢复边界。
4. 开发 `.env.development` 改为回环源站；开发专用配置关闭合法域名校验。正式配置不改。说明本地账号与公网账号不是同一个账户库。
5. 运行 `npm test`、`npx tsc --noEmit`、`npm run build:weapp`、原生 WXSS 编译、`git diff --check`；确认 bundle 包含本地源站。检查本地接口响应；不以错误提示或构建通过冒充真实账号验收。
