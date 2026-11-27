# Batch Book Body Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 修复规则污染正文和 TOS 保存、删除、清理的数据丢失风险。

**Architecture:** 沿用 Go/MySQL/TOS，以独立对象地址保存新正文，数据库事务切换引用并登记待回收对象；物理回收在事务提交和宽限期之后执行。规则入口使用明确文本，失败按版本报告。

**Tech Stack:** Go、database/sql、sqlmock、MySQL、现有迁移登记机制、TOS SDK、React、Vitest。

## Global Constraints

- 设计：`docs/superpowers/specs/2026-11-27-batch-book-body-safety-design.md`，用户已确认。
- 在当前 v88 顺序集成；不建分支，不覆盖其他任务改动，不推送。
- 禁止真实抓书、模型、上传和破坏性 TOS 测试；隔离数据库可使用。
- 4001 切换前确认活跃任务；不打断已有付费任务。
- 仓库实际使用 `storage.Migration` 与 `schema_migrations`，没有独立 Goose SQL 目录：新增版本沿用该机制，避免引入第二套迁移系统。这是对设计“现有迁移方式”的具体核实。

### Task 1: 规则正文读取与逐版本结果

**Files:** 修改 `backend/internal/httpapi/novel_fetch_legacy_workflow.go`；新增 `backend/internal/httpapi/novel_fetch_rules_body_safety_test.go`。诊断源在归档 audit-20261127/httpapi_audit_test.go。

**Interfaces:** 保留 applySensitive 的 HTTP 路由；正文读取返回 `(string,error)`；新增响应 `version_results`，记录 book_id/version_id/status/error，旧 updated/failed 等字段保留，failed 按失败书籍计。

- [x] 把规则污染诊断移为正式回归，增加版本字符串、text 对象、状态对象、存储失败场景。

```go
if body.Content != "原有AI正文" { t.Fatalf("unexpected body: %q", body.Content) }
```

- [x] `go test ./internal/httpapi -run 'Test.*Rules.*Body' -count=1`，先观察版本对象污染断言失败。
- [x] 优先 GetBody；ErrNotFound 才读取 string/text 兼容形态；不接受任意 fmt.Sprint。保存失败追加版本失败结果，不更新该版本引用或 done 状态；部分成功继续保存，返回实际书籍计数。

```go
switch v := raw.(type) {
case string: content = v
case map[string]any: content, _ = v["text"].(string)
}
```

- [x] 重跑上述测试和既存敏感词/规则相关测试，通过后提交本任务文件。

### Task 2: 独立对象与事务保存

**Files:** 修改 `backend/internal/novelfetchworkshop/mysql_store.go`、`object_body_store.go`；新增 `mysql_body_object_lifecycle.go`、`mysql_body_object_lifecycle_test.go`；修改 `backend/internal/storage/novel_fetch_workshop_schema.go` 及其测试。

**Interfaces:** 保留 PutBody/GetBody；新增内部独立地址生成与事务登记方法；新增 `novel_fetch_body_object_retirements` 表，至少 object_key 主键、owner/book/version、registered_at、not_before、state、attempts、last_error。历史地址登记 held，新格式 pending，默认宽限期一小时。

- [x] 正式复现 SQL 失败后旧对象丢失；增加两次写入不同地址、提交未知不删除、版本返回正确场景。

```go
if objects[previousKey] != "old successful body" { t.Fatal("previous body destroyed") }
```

- [x] `go test ./internal/novelfetchworkshop -run 'Test.*(TOS|Object|PutBody)' -count=1`，记录修复前失败。
- [x] 新地址含随机写入标识；事务 SELECT FOR UPDATE 锁定当前记录，更新新引用并登记旧对象，事务内取得准确版本；提交失败绝不删除可能已生效的对象。首次行并发插入的冲突返回可读错误，保留对象，禁止借不确定结果重复调用外部模型。
- [x] 新增表迁移版本使用未占用编号；已发布迁移内容不可改变。验证迁移 SQL 的主键、账号字段、到期索引、错误记录和幂等创建。
- [x] 重跑正文存储和 migration 包测试，更新与旧 SQL 协议冲突的测试预期，不能删除故障覆盖；通过后提交。

### Task 3: 安全删除、清理和持久回收

**Files:** 修改 `mysql_store_cleanup.go`、`mysql_store.go` 中 DeleteBody、`body_cleanup.go`；回收实现放 `mysql_body_object_lifecycle.go`；修改 `backend/internal/app/app.go` 既有清理周期；更新相关测试。

**Interfaces:** 新增 `ReapRetiredBodyObjects(ctx context.Context, now time.Time, limit int) (int,error)`；BodyCleanupResult 增加 `objectsPending`/`objectsHeld`，deleted 仍指正文记录移除数量。

- [x] 复现清理候选已被重存时不能删对象、SQL 回滚不能删对象、直接删除同样安全。

```go
if result.Deleted == 0 && len(deletedObjects) != 0 { t.Fatal("deleted reactivated body") }
```

- [x] 运行清理测试，记录修复前失败；统一用事务重读并核对候选版本、地址和 releasable 时间，确认条件不变才删记录、移除引用、登记回收，提交前零 TOS 删除。
- [x] 回收只删除 pending、新格式、到期且没有活跃引用的精确地址；查询/记录失败停止，不删对象；失败增加次数并写错误，成功标记完成。单轮限量，不删除 held 历史地址，不扫描桶。
- [x] 接入现有低频清理周期，回收失败保留记录下轮继续；不创建新的高频无限循环。
- [x] 用 sqlmock 验证事务与故障顺序；有可用隔离 MySQL 时验证重新保存与清理交错。运行所有正文存储测试，通过后提交。

### Task 4: 页面显示真实规则与清理结果

**Files:** 修改 `frontend/src/user/pages/NovelFetchPage.jsx`；新增小型 `novel-fetch-rule-result.js` 与 `novel-fetch-rule-result.test.js`；定位正文清理状态的现有页面并更新相关文案。

**Interfaces:** 结果辅助方法 `describeRuleApplyResult(result, fallbackText)` 返回 `{type,message}`，type 为 success/warning/error。失败数大于零绝不 success；无失败保留原成功文案。

- [x] 写部分失败提示测试：

```js
expect(describeRuleApplyResult({updated:1,failed:1}, '已完成').type).toBe('warning');
```

- [x] 使用仓库实际 node:test；新模块实现前测试因模块缺失失败，补齐后通过，并替换三处无条件 success。
- [x] 已确认当前 React 没有正文容量清理页面或接口调用，因此不增加新页面；清理响应扩展 objectsPending/objectsHeld，由后端测试确认 deleted 仅指正文记录，不能声称已经释放空间。
- [x] 运行相关前端测试，通过后提交。

### Task 5: 整体验证与本地交付

**Files:** 更新本计划复选框；新增 `docs/acceptance/2026-11-27-batch-book-body-safety.md`，记录源 SHA、测试结果、构建和运行版本。

- [x] `go test ./internal/novelfetchworkshop ./internal/storage ./internal/app -count=1`；httpapi 规则、敏感词、正文相关测试通过，另有真实隔离 MySQL 验证通过。
- [x] 使用仓库实际 node:test，相关前端 11 项通过；React 生产构建与 Go embed 候选构建通过，输出位于归档目录。
- [x] 固定 c4518c3b 独立源码构建，在隔离端口 4027 以测试账号验证启动、正文读写、规则结果和前端资源；未触发真实外部服务。
- [x] 4001 仍为 c33aa9d3，业务库只读确认另一个漫画批次自动化作业仍 running，因此保留候选，不重启或切换。
- [x] 仅提交本次相关变动；新增前端测试已接入默认命令且保留 package.json 原有改动；提交前检查无真实凭据或正文泄漏并通过 diff 检查。剩余限制见验收报告。

## Self-review

覆盖设计中的规则文本、逐版本失败、独立对象、提交未知、并发清理、直接删除、宽限期、历史对象、崩溃恢复、迁移和本地验收。当前报告与实现计划不把模拟测试等同真实 TOS 验收。任务 2、3 共享存储文件，应由同一实现者顺序执行；规则任务可独立处理，主执行者负责集成。
