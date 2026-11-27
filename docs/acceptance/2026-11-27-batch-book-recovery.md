# 批量测书恢复：本地验收记录

## 已完成

- 用户确认方案后，在 v88 提交方案与计划 `2b954985`，业务修复 `aa084a90`。未创建分支、未推送、未发布公网。
- latest、单任务、全自动状态及实时概览使用队列存在性核对。旧 queued/running/waiting_retry 在队列已不存在时返回只读失败投影和 `status_reason=queue_missing`，保留原始历史记录，不自动重试。
- Redis 故障返回 `queue_check_error`，不据此判定历史记录失效；真实队列任务保留状态。
- 前端明确展示队列核对结果，最新任务为空时清除旧状态；旧失效记录不遮盖最新有效进度。空批次与空历史给出真实说明。
- 书号 `7705784637234105369` 已恢复任务关联，当前恢复批次为 `recovery-20261127T041042766100Z`。恢复时间单独说明，原任务日期、平台、分类和上传回执未编造。
- 三份正文均恢复 ready，回收/过期时间清空，内容与 revision 未重写。任务带 `manualOnly=true`，不会被全自动队列自动生成或上传。
- 操作前已导出压缩正文、可读正文和哈希清单，位置：`_local-archive-20261028/batch-book-reference-20261127/existing-bodies-20261127T035519Z/`。恢复前生命周期及当前批次指针快照在 `before-recovery.json`。
- 恢复后重复检查返回 `already_recovered=true`，没有重建第二份任务；原账号 1869 条历史 runs 保留。

## 正文校验

| 版本 | 字数 | SHA256 |
| --- | ---: | --- |
| original_raw | 5656 | acf8648b89050c16a82109371bad1b532eaf3789f535cd7271fdbb4eefae6b5f |
| original | 3734 | 08db4bfb3874992a9f0916edbbfce6c493a695a77b44293d9affc60881cb017d |
| ai1 | 3733 | e53a6dfaac8146f5fb3a8929544186cbe35ba1037d6ce142602e473cd7d4c7aa |

## 检查证据

- Go：`go test ./internal/novelfetchworkshop -count=1` 通过；`go test ./internal/httpapi -run 'Test(NovelFetch|Workshop|QueuedRecovery|GenerateOnlySchedule|ProcessJob)' -count=1` 通过。
- 新增状态回归测试先复现旧 queued 直接返回问题，修复后通过；覆盖 queue_、go-workshop-、go-schedule- 前缀、队列存在/不存在/故障及历史不变。
- 前端：排除单独卡住的旧 `novel-fetch-config-defaults.test.js` 后，测书页面相关 141 项检查通过。该旧配置测试在加载 React/Antd 阶段未返回，本轮中止，不计入通过数。
- 未改动的 `src/shared/api/novelFetchWorkshop.test.js` 单独运行仍有 4 项失败，错误为账号前置确认 `IMPERSONATION_UNCONFIRMED`。对应 API 和账号前置确认文件没有本次改动；本轮未修改权限逻辑以迁就旧测试。
- Python：恢复文档构造与不完整备份拒绝测试 2 项通过；真库 dry-run 哈希检查通过，事务恢复成功，恢复后核对三版本 ready、到期时间 NULL。
- 真库/真实 Redis 同任务 A/B：`TestNovelFetchRecoveredBookLocalAcceptance` 通过（6.33 秒）。同一份旧记录经原映射仍为 queued，经新核对为 queue_missing；Go Store 实际读取三版本正文并验证固定哈希，恢复文档状态 pending_submit 且自动处理资格为 false，当前批次活动/排队均为 0。该检查没有启动 router 或 worker。
- React：`npm run build` 成功，6267 modules，耗时约 6 分 25 秒；保留已有大包与静态/动态重复导入警告。
- 浏览器：在 4001 已登录账号上刷新后，当前批次由 0 本变为 1 本，AI1 勾号、`3734/5656`、`处理完成，待提交` 可见。详情原文 textarea 实测 3734 字，AI 版本 1 显示 3733 字且正文可见。未点击保存、生成或提交。

## 尚待交付的运行包切换

4001 当前仍运行 `qiantie-local-c33aa9d3.exe`。恢复数据已被该程序读取，但不能把这当成本次代码修复已在实际运行。

当前监听程序还在执行其他漫画批次：`batch_78c4df7c08e16b77b0af8a84bb5407b8`、`batch_e81b39fe61edf53eac87919dd4b95c4c`。本轮未停止它们、未发起其中的模型调用，也未重启 4001。

新版前端已同步至 Go embed，候选 Windows Go 运行包已构建成功：`_local-archive-20261028/batch-book-reference-20261127/runtime/qiantie-local-bca52a33.exe`，SHA256 `c742a145c33c2659f28382b0f4de20584721377a5e34d07062af8008d51e16fa`，前端入口 `/assets/user-Klte0C_k.js`。构建基线 `bca52a33` 包含本轮修复；工作区有用户原有未提交前端改动，manifest 如实标记 `frontendSourceState=dirty`，不能称为干净提交的精确发布包。

最后核对监听进程仍为 42620。第一个漫画批次已不在 running，第二个批次仍为 running，counts 为 total=25、running=24、queued=1。需要确认无活动任务后才能切换，或由用户明确决定如何处理正在执行的任务。不能为交付本次小修而擅自中断其他可能已扣费的请求。本轮没有重启，`/health` 与新版 `/api/runtime-build-info` 的运行验收因此未完成。

本轮不包含真实模型新生成、外部网站登录或上传验收，视频管理系统页面仍显示未登录。
