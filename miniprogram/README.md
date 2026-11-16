# 一战晟铭微信小程序

独立的 Taro + React 微信小程序工程；现有 `frontend/` H5 不受影响。

## 本地配置

1. 将 `project.private.config.example.json` 复制为 `project.private.config.json`，填写微信小程序 AppID。
2. 将 `.env.development.example` 复制为 `.env.development`，填写已备案、已配置为微信合法域名的 HTTPS API 源站。
3. 安装依赖后运行 `npm run build:weapp`，默认使用 `.env.production` 的公网 API 地址；在微信开发者工具中导入本目录，构建目录为 `dist/`。本机沙箱使用 `npm run build:weapp:sandbox`；开发监听使用 `npm run dev:weapp:watch`，两者读取 `.env.development`。

`project.private.config.json` 和 `.env.development` 均不会提交。不要在前端写入 AppSecret、`session_key` 或微信临时 `code`。

### 本机账号调试

默认构建连接 `https://yzcm.site`，输入已有公网账号；登录页会显示当前服务地址。本机沙箱连接 `.env.development` 配置的 `http://127.0.0.1:13192`，使用本地账号库账号。两者均调用真实 `/api/login`，随后验证 `/api/login/session`；不同源站令牌互相隔离。密码不存储，MFA 账号可输入验证码。

运行相应构建命令后，在微信开发者工具点击“编译”。手机上的 `127.0.0.1` 指向手机本身，因此沙箱地址仅用于当前电脑模拟器。公网真机必须配置微信 request/download/upload 合法域名。

## 认证接口契约

- 当前账号入口：`POST /api/login` → `GET /api/login/session`；退出调用 `POST /api/login/logout` 并立即清理本机令牌和媒体缓存。
- 以下微信绑定函数保留兼容，但不自动调用；是否可用取决于服务端配置，不能用账号登录成功证明微信绑定可用。
- `POST /api/wechat-miniprogram/session`：接收 `wx.login` 返回的 `code`。已绑定账户返回业务 `token` 和账户信息；未绑定账户返回短时 `bindingTicket`。
- `POST /api/wechat-miniprogram/bind`：接收 `bindingTicket`、`username`、`password`，完成显式绑定并返回业务 `token`。

服务端必须用 AppID/AppSecret 调用 `code2Session`，只保存或关联 OpenID/UnionID；禁止将 AppSecret、`session_key`、微信临时 `code` 返回或持久化到小程序端。

## 验证与发布前检查

```powershell
npx tsc --noEmit
npm test
npm run build:weapp
(Get-ChildItem dist -Recurse -File | Measure-Object -Property Length -Sum).Sum
```

在微信开发者工具中完成预览前，确认 request、upload、download 合法域名和备案均已设置。真机验收应覆盖账号登录、MFA、会话失效、上传入口、安全区布局及后台切换后的失败提示。批量工厂公网生产写保护仍保留，不能以按钮存在判断已开放执行。逐模块验证边界见 `docs/full-mobile-client-verification-2026-10-15.md`。
