# 微信登录：本地核对与待补源码

## 范围

本次只修改本地小程序。公网仅做只读核对；不部署、不改配置、不执行数据库迁移、不提交或推送仓库。

## 当前证据

- 用户开发者工具截图中的 `POST /api/wechat-miniprogram/session` 返回 404；发生在账号密码校验之前。
- 小程序的 `/session`、`/bind` 请求路径与 `server-main/backend/internal/wechatminiprogram` 的 HTTP 契约一致。
- 公网容器镜像标签为 `1fc8db97433e354d6c9dc9aba7020b9ecab1174f`，但宿主机将 `/opt/qiantie/v88/direct/current/go/qiantie` 挂载到容器 `/qiantie`。镜像标签不能代表当前运行程序。
- 2026-10-15 从 Nginx 容器读取上游 `/api/runtime-build-info`，返回 `runtime=go`、`git_sha=7df6290b0c8294f89f7e226fdeecf3024cedbdcf`。
- 该版本发布包只列出 `go/qiantie`，没有 Go 源码。已检查的服务器发布目录未找到该认证模块源码。
- 分别查询 `cui1112233/-`、`cui1112233/yizhanchengming` 的该提交，GitHub 均返回找不到提交。仅说明这两个仓库当时无法解析该 SHA，不代表其他仓库或目录一定不存在。
- 本地 `server-main/backend` 模块为 `github.com/cui1112233/yizhanchengming/backend`；实际程序的构建模块为 `qiantie/backend`，且包含不同的 `internal/auth` 实现。不能拿旧工程全量替换当前程序。

## 本地改动

- 登录和微信凭证获取增加原生超时及每阶段 15 秒兜底；迟到响应不会保存会话或继续提交密码。
- 404 明确提示微信登录接口不存在或尚未上线。
- 错误响应优先展示服务端 `message`，避免向用户展示 `invalid_credentials` 等机器错误码。
- 原生网络失败转换为页面可展示的 Error。
- 绑定响应必须先验证并保存业务令牌，之后才清除短期绑定凭证。

## 验证及边界

- 小程序：33 个测试文件、128 个用例通过；TypeScript 检查及 Taro 微信构建通过。
- 旧本地 Go 工程：`go test ./...`、`go vet ./...` 通过。这只验证该旧工程，不能证明当前公网后端兼容。
- `dist/` 已重新构建；仍需微信开发者工具编译并验收错误提示。
- 尚未完成真实微信授权、账号绑定、受保护业务 API 或媒体播放验收。

## 后端适配前必须具备的输入

找到当前运行版本的可追溯源码，确认账号表、密码算法、会话存储、认证中间件和账号停用策略。再在本地补齐微信换码、一次性绑定票据、身份绑定和会话签发，并验证该会话确实能访问业务 API。

不得为绕过 404 而生成假令牌、改用户密码、退回旧账号文件或用旧补丁覆盖现有服务。公网发布需要独立明确授权。
