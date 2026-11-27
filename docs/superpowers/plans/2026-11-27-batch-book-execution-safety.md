# Batch Book Execution Safety Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans / subagent-driven-development and test-driven-development; preserve the current shared v88 workspace. Root integrates and reviews every group.

**Goal:** 修完已确认剩余问题，再统一更新本地运行版本。

**Architecture:** 共用真实正文读取、每书准入与现有持久队列；预算保存在阶段重试表；摘要扫描与正文读取分开。第一批不可变对象与安全回收保持不变。

**Tech Stack:** Go、MySQL、Redis、React、node:test、sqlmock。

## 约束

- 范围与细节见同日 execution-safety-design，用户已授权先修复再统一更新。
- 不新建分支、不覆盖无关修改、不推送、不触发真实抓书/付费生成/上传。
- 工作区其他任务临时编译错误不修改其实现；必要时使用只读基线或归档 overlay 验证本批，不伪造依赖。
- 先提交、独立构建及隔离验收，最后处理本地 4001 一次切换；不打断别的付费任务。

### 1. 正文读取与摘要同步

Files: novelfetchworkshop/rewrite.go、store.go、mysql_store.go、mysql_body_object_lifecycle.go、新 body_text_reader.go 及相应测试。

- [ ] 迁入仅引用正文生成失败、保存正文后计数陈旧的正式回归；运行并记录 RED。
- [ ] 实现 `ReadBodyText(ctx,store,owner,document,versionID)(string,error)`，正文优先、ErrNotFound 才兼容、其他错误返回。
- [ ] Memory PutBody 同锁、MySQL 同事务同步已有任务的引用与字数；不建虚假任务。
- [ ] PutDocument 合并权威正文引用并避免重新导入旧内嵌正文；数据库只读每书引用，不下载对象。
- [ ] 执行 `go test ./internal/novelfetchworkshop`，补独立 MySQL 的 PutBody->旧 PutDocument->读取一致性验证。

### 2. 执行准入、停止、队列核验与错误退避

Files: httpapi/novel_fetch_workshop.go、novel_fetch_retry_queue.go、novel_fetch_legacy_workflow.go 的停止路径、执行快照文件、novelfetchworkshop/queue.go 仅 GenerateOnly 字段、新准入与退避辅助文件及测试。

- [ ] 重现两个并发生成、新停止被旧重试覆盖、空队列误判运行、立即报错的 Claim 高频循环。
- [ ] 直接生成入现有唯一队列，返回 `{ok:true,queued:true,job_id,task,message}`；重复冲突零模型调用。
- [ ] GenerateOnly 进入快照、恢复和 worker，不重新抓取/分类/提交；执行锁与租约沿用现有能力。
- [ ] 短准入内重新读取、CAS 更新、队列实际核验；停止共享准入，失败回滚只能操作自己的文档版本。
- [ ] Claim 错误可取消退避至 30 秒，正常队列阻塞超时不额外重试风暴。
- [ ] 运行上述回归及原 workflow_consistency 七场景；停止与重复生成均以实际模型替身调用数验证。

### 3. 持久预算与轻量扫描

Files: httpapi/novel_fetch_retry_schedule.go、novel_fetch_automation.go、新预算测试与 novelfetchworkshop 的候选读取文件。

- [ ] RED: 普通次数超限、格式输出零间隔、上传未知结果自动重发、同轮扫描重复读取全文。
- [ ] retry_schedule 保存失败次数、首次失败时间与格式补救次数；普通三次失败人工，分类一次失败人工，格式错误最多一次自动补救且至少 30 秒，原文保留现有五次补跑。
- [ ] 明确人工错误和未知结果，保留上传回读；阶段成功只清本阶段；明确手动重试才清全轮。
- [ ] 轻量候选接口只加载摘要，去除双 ListDocuments，恢复改动定点刷新；内存存储回退保持行为。
- [ ] 验证重复调用扫描、重启、新 job_id 不能恢复耗尽预算，不增加任何模型/上传调用。

### 4. 前端终态与后台生成提示

Files: frontend/src/user/pages/NovelFetchPage.jsx、novel-fetch-progress.js/test.js 或独立小型状态辅助模块与测试。

- [ ] 以 stopped/cancelled 与 queued 返回编写行为测试。
- [ ] 执行等待复用共同终态判断，不把停止继续等到 30 分钟；生成结果 queued 提示已入队。
- [ ] 等待轮询按现有策略降低刷新频率，避免每秒重读所有任务；保留停止等待和真实停止的区别。
- [ ] 相关 node:test、页面构建通过，保留第一批部分失败提示。

### 5. 历史时长迁移的可恢复执行

Files: storage/batch_factory_v11_schema.go、新 duration_migration_resume.go/test.go。

- [ ] 从 1100017 原 SQL 部分执行状态重现再次运行重复字段失败；1100018 同类场景覆盖。
- [ ] 增加 Adopt 回调，核验现有 decimal(10,3)、非空、默认零，只补缺失字段；冲突明确失败。
- [ ] 原 SQL/CallbackChecksum 不改，检查迁移校验结果保持不变；真实隔离库复现部分执行后恢复，再运行不重复 DDL。

### 6. 统一验证、提交与本地更新

- [ ] 各组独立审查和整合审查，相关 Go / node 测试通过；无关正在修改的代码单列，不夹带提交。
- [ ] 更新审计报告与验收记录，记录修复 SHA、实际通过项及未完成边界。
- [ ] 从最终确定提交独立构建 React 和 Go embed；临时端口测试真实会话、仅引用正文、排队/停止/超限状态，外部服务使用替身。
- [ ] 只读检查 4001 与其他批次；在安全窗口统一切换同一提交，检查 health/runtime-build-info/正文与队列状态。仍存在付费运行时保留候选，不盲目中断。

## 自审

前三组文件边界分别约定，MySQLStore 仅正文组维护，workshop 路由仅执行组维护，根代理负责前端与旧迁移。新增 GenerateOnly 必须贯穿快照与恢复，不能只在 HTTP 请求中存在。最终只更新一次运行服务，源码和隔离验证先行。
