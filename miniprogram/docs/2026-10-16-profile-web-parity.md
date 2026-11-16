# 个人资料页面对齐

依据：用户提供微信开发工具与 5176 网页的两张截图，以及本地网页版 `frontend/src/components/profile/ProfileEditPage.jsx` 和 `src/api/member.js`。

原因：原小程序页面只编辑、发送 displayName，其余资料为只读；头像入口不在资料页。不是日志中的 getSystemInfoSync 弃用警告导致。

本地修正：头像及账号身份区→完整资料表单，包含名称、手机号、邮箱、团队职务、240 字简介与保存按钮，沿用原生主题。使用已有 PATCH /api/member/profile 和 POST /api/member/avatar（avatarDataUrl JSON），操作后 GET /api/member/me 核对。未修改公网代码、部署或远端仓库，未自动保存真实账号资料。

验证：服务契约测试覆盖完整字段、读回丢字段、空名称、头像接口；组件渲染检查覆盖五个编辑项与头像入口。TypeScript、production 构建已通过。真实头像上传、资料写入与微信模拟器视觉效果尚未验收。

查看：微信开发者工具加载此工程的最新 dist，点击编译，打开“我的→个人资料”。本轮不强行切换用户的主题；深色参考与浅色小程序应在相同主题下核对。
