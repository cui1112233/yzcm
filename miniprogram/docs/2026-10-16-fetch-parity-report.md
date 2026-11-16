# 小说获取功能对应修复

日期：2026-10-16。仅本地源码与隔离请求测试；没有请求真实服务器、登录、生成、发布、提交、推送或部署。

## 参考与变更

参考：`/private/tmp/yizhanchengming-main-web-preview/frontend/src/modules/fetch/NovelFetchWorkspace.jsx`、`NovelFetchAdvancedPanel.jsx` 与 `src/api/novelFetchWorkshop.js`；需求见工作目录中的 `5176与微信小程序功能差异审查-2026-10-16.md` 与本项目 `docs/superpowers/plans/2026-10-16-functional-parity-repair.md`。

- `src/pages/module-detail/FetchWorkspace.tsx`：保留当前手机工作台与请求版本保护；定时支持编辑时间/自动提交、启停、确认删除；创建默认不自动提交，由用户选择。编辑只 PATCH runAt/autoSubmit，不覆写原 inputSnapshot；启停只 PATCH enabled。维护写后读取 schedules；未知结果阻止再次写，直到显式刷新成功。原版本 work-form 保存增加 config 读回与同步写锁、未知结果阻断。新增高级维护面板入口。
- `src/pages/module-detail/FetchMaintenancePanel.tsx`：workflow 三个自动流程开关，fetch 默认字数/并发/重试；改文/处理规则/知识库使用提示词、真实知识库统计、敏感修复开关、按已保存规则预览；网站账号替换密码、默认配置档和逐版本绑定；配置档/样式同步，环境检查；日志与问题记录。保留服务端配置中未编辑字段，不填假统计。长文本 maxlength=-1，普通输入有明确限制。
- `src/services/fetchMaintenance.ts`：维护写请求与读回，显式拒绝抛错，网络不确定或读回失败提示结果未知，不自动重放。密码从可编辑 settings 和 app_config.web_submit 中去除；只有用户输入新的密码才在该次保存请求中包含 password，不持久化本地、不回显已保存密码。
- `src/services/fetchMaintenance.test.ts`、`src/pages/module-detail/FetchMaintenancePanel.test.tsx`、`src/pages/module-detail/FetchWorkspace.requests.test.tsx`：凭据隔离、保留未编辑字段、失败读回、显式拒绝、启停编码、编辑保留快照、自动提交关闭、确认删除与读回；同时保留已有 race/长超时/重复操作保护回归。

## 检查证据

先运行缺失能力测试，确认失败于缺少实现/缺少操作/原 work-form 无读回；实现后重新验证。

`npx vitest run src/pages/module-detail/FetchWorkspace.requests.test.tsx src/pages/module-detail/FetchMaintenancePanel.test.tsx src/services/fetch*.test.ts`：6 个测试文件，30 项测试通过（最后执行 11:11 本地时间）。

`npx tsc --noEmit`：退出码 0。构建和全量检查由主代理统一执行。

已请求主代理对本模块做后续代码复核；本报告不把代理自检算作独立复核。

## 准确的剩余边界

目前证明源码接线、参考接口方法/字段与模拟读回行为，不证明真实账号权限、网站登录有效、定时实际执行、知识库实际修改或目标网站提交成功。维护请求完成并读回最新配置，不等于所有字段已被真实服务器持久化验收。需要用户在微信开发者工具编译 dist 后检查高级面板原生排版、长文本粘贴，以及同账号同实体保存后离开重进的真实读回。

新增入口未触发任何自动提交；原任务预览/确认提交路径继续复用已有 fetchSubmission。规则预览使用已保存的服务端规则，未保存的提示词不会悄悄写入。网站环境检查失败不宣称登录成功。
