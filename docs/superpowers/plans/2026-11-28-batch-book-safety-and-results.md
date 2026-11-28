# Batch Book Safety and Results Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Track steps with checkboxes.

**Goal:** 实施已审阅的正文、模型参数和真实结果设计，修复R1–R7并更新本地4001。

**Architecture:** 沿用现有正文处理、模型provider、阶段重试与上传连接器。入口共用处理和结果判断；独立归档验证，root整合共享文件，维护v88。

**Tech Stack:** Go、React、MySQL/Redis、Go embed、Go test和node:test。

## Global Constraints

- 设计：docs/superpowers/specs/2026-11-28-batch-book-safety-and-results-design.md；用户已审阅并要求执行。
- 不建分支、不推送、不生产部署；保留所有无关脏改动，尤其app.go和api_integrations.go。
- AI1–AI5；原文另算；不扩大默认生成数量；nil与明确空版本分别保留。
- 保留完整原文、旧AI1、有效AI2和远端305481；本批不用真实调用或上传验证。
- 模型参数和错误公共接口由任务2定义；任务间窄patch交付，共享大文件由root审查整合。

## Task 1: 恢复原文共用正文处理（R1）

Files: httpapi/novel_fetch_workshop.go恢复路由、小型恢复helper及回归；复用novel_fetch_sensitive_metadata.go既有保存接口。

接口：恢复helper接收http请求、Store及既有provider resolver；调用processWorkshopConfiguredText，并经saveNovelFetchSensitiveMetadata保存当前版本结果，不能整包覆盖最新文档。

- [ ] 把审查TestAuditOnlyRestoreRetainsStaleSensitiveSuccess改成恢复后仍为替换词、统计重算的断言，确认RED。
- [ ] 覆盖显式开关、任务快照开关和账号默认；增加处理失败、停止及并发其他版本失败保留断言。
- [ ] 恢复只在处理成功后写original；raw/AI版本不变；失败保留旧正文并记录恢复错误。
- [ ] 相关恢复、敏感元数据和正文存储测试GREEN；交叉审查后提交窄修改。

## Task 2: 错误安全与上传证据（R2、R6）

Files: batchfactoryv11/provider_error_safety.go和测试；httpapi/integration_channel_exec.go、novel_fetch_retry_schedule.go、web_submit_service.go及错误读取/上传回归。

接口：batchfactoryv11.SafeProviderError(error, ...string) error及SafeProviderErrorText(string, ...string) string。保留errors.Is/As分类，公开Error文本脱敏；provider在构造HTTP错误前精确移除本次key。

- [ ] 假密钥回显先RED；断言provider、重试保存与页面错误不包含该假值；已有错误日志和账号隔离测试保持。
- [ ] 复用已有脱敏模式，公共实现供provider、改文保存、重试和返回使用；历史对外错误读取脱敏。
- [ ] 假连接器上传成功、读取超时先RED：submitted保留，verified=false、readback_status=pending，后续核对不能再次上传。
- [ ] 只有可靠匹配远端记录才confirmed；正文hash独立content_verified，不伪造正文核对；已有回执和未知结果保护保持。
- [ ] 上传/重试/敏感错误测试GREEN；交付patch，不改共用设置和真实记录。

## Task 3: 有效参数、独立超时和一份预算（R3、R4、R5）

Files: novelfetchworkshop/rewrite请求与计划及小参数helper；batchfactoryv11/director_contract.go、director_provider.go；httpapi/integration_channel_exec.go和retry helpers；app改文adapter及新helper；相关Go测试。

接口：RewriteRequest增加Timeout与ExtraBody（只携带已校验参数）；TextCompletionRequest增加ExtraBody map[string]any，已有调用不传时保持行为。参数公共组装拒绝model/messages/凭据/输出协议覆盖；SafeProviderError复用任务2接口。

- [ ] 参数本地HTTP捕获、父180秒却子60秒、length丢失三个审查样例改为正确断言并RED。
- [ ] 统一Top P、惩罚、思考及额外参数，渠道默认不得覆盖明确业务值；拒绝互斥思考与受保护字段冲突。
- [ ] 改文读取有效超时默认180秒，分类保持60秒；明确响应协议由模板掌握。
- [ ] 截断/拒绝/配置错误/未知结果进入明确保护状态；非流式稳定保存；队列每轮预算最多3次，retry_times=0不自动重试，输出修复最多一次；内部不再叠加调用。
- [ ] 适配器、请求、参数、安全及阶段重试回归GREEN；app.go仅提交本轮窄修改，保留既有脏内容。

## Task 4: 页面生效口径和本轮进度（R3、R7）

Files: NovelFetchPage.jsx、novel-fetch-progress-summary.js及node:test；必要后端任务视图字段。

接口：summarizeWorkshopProgress(rows, fallbackSubmissions)共用本轮版本读取；优先processSnapshot.selected_versions，严格保留空数组。没有快照的历史行保留已有语义。

- [ ] AI2缺失而旧AI1存在的审查样例改成aiDone=0并RED；覆盖AI2已就绪、空选择、只原文、原始正文不算AI。
- [ ] 统一页面统计来源；温度同步rewrite.temperature，思考互斥，retry_times输入最多2，流式明确禁用，JSON显示实际模板约定。
- [ ] 上传接收与核对状态显示真实字段，不将pending核对显示confirmed，不改变已接收的业务口径。
- [ ] 测书前端测试GREEN；确认未改其他页面后交付。

## Task 5: 整合、审查和4001验收

- [ ] 各任务代码与设计逐条审查，解决共享文件冲突；失败回归转换为正式测试，不保留“缺陷存在即通过”的断言。
- [ ] 精确提交v88，保留无关脏文件；运行相关模块及HTTP回归、前端测试，干净归档构建React和Go embed。
- [ ] 确认4001队列空闲；记录正文和回执指纹、旧程序回退点；新包必须包含当前已运行版本，禁止覆盖较新程序。
- [ ] 本地更新并核对SHA、静态资源、页面、原文/AI1/AI2/回执未变；隔离假服务验证不产生真实模型或上传调用。
- [ ] 更新验收报告和本计划，写明每项结果和实际运行提交。

## Commands

```powershell
go test ./internal/novelfetchworkshop ./internal/app ./internal/httpapi ./internal/batchfactoryv11 -count=1
node --test frontend/src/user/pages/novel-fetch*.test.js
```

Windows前端测试使用Get-ChildItem展开文件列表；临时构建、探针和日志统一归档。root对实际改动选择必要回归，宽测通过后不无故重复。
