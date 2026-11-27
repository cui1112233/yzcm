# 剧本编辑本机验收记录

日期：2026-11-27
验收环境：本机 `http://127.0.0.1:4001/script-editor`
运行二进制：`_local-archive-20261028/luoshui-parity-20261126/runtime-binaries/qiantie-local-c3932619.exe`
运行版本：`c3932619`（包含剧本编辑后台任务提交 `53bc7467`、前端任务面板提交 `61485c6d`）

## 已完成

- 新增侧边栏入口“剧本编辑”，不改动原有“私人定制”和“漫剧创作”。
- 支持作品库、新建作品、搜索、筛选和排序。
- 支持 TXT、Markdown、DOCX 预览导入；旧版 DOC 会明确提示不支持。
- 支持原文章节新增、编辑、排序、删除和自动保存。
- 支持分集剧本手工创建、自动保存、复制版本、删除版本和设为正式版。
- 支持用户主动提交 AI 出剧本后台任务；系统提示词只保存在 Go 后端。
- AI 任务以 MySQL 为真实记录，Redis 只负责唤醒；Redis 临时失效后，worker 会扫描 MySQL 恢复排队任务。
- 支持跨刷新查看任务、取消、失败部分重试；已经成功保存的分集不会因其他分集失败而丢失。
- 支持 AI 审稿、AI 优化和爆款开头。优化结果只在用户点击“采用为新版本”后写入，不覆盖原稿。
- 模型调用前先保存指纹和“可能已发出”状态；进程中断后不会盲目重复模型调用。
- 支持只导出正式版：合并 TXT、分集 ZIP、结构化 JSON。
- “导出交付”页可以把每集正式版直接创建成漫剧项目，并记录幂等交接关系。

## 真实页面验收

使用已登录账号 `choushiyiguai` 创建作品 `复刻验收-20261127-0239`：

1. 创建并保存 1 个原文章节，刷新页面后仍显示 `1 章 · 38 字`。
2. 创建第 1 集 V1，输入 52 字剧本正文，刷新页面后正文和字数仍在。
3. 将 V1 设为正式版后，TXT、ZIP、JSON 三个导出入口均可用。
4. 点击“导入漫剧创作”，创建项目 `剧本编辑最终验收-20261127-0256`。
5. 漫剧项目 ID 为 `comic_38f845972fa1f8b28a829558`；数据库记录 1 集、1 个结构化段落。
6. 在漫剧创作真实页面打开后显示“共 1 集”，集名和 52 字正文都正确；页面已保留供继续查看。

高级任务链路更新后，在同一作品的“分集剧本”页真实看到：

1. `AI 出剧本`、`AI 审稿`、`AI 优化`、`爆款开头` 四个入口。
2. 独立 `AI 任务` 面板和“离开页面也会继续，回来可查看结果”说明。
3. 打开 `AI 优化` 弹窗后，真实显示已配置文字模型、费用提示，以及“只有点采用为新版本后才写入，不会覆盖原稿”。
4. 没有点击“提交任务（会调用模型）”，因此没有发生真实模型调用或费用。

验收过程中发现并修复了两个真实运行问题：MySQL 8.4 拒绝 binary 字符集 JSON 参数；剧本正文自带“第1集”时，交接包装会重复生成集标题。两项均有回归测试，最终修复提交为 `1e22c0f7`。

## 自动化验证

- `go test ./internal/scripteditor ./internal/httpapi ./internal/app -run 'ScriptEditor|Job|Polish|RedisJobQueue' -count=1`：通过。
- `go test ./internal/scripteditor ./internal/storage ./internal/httpapi ./internal/prompt -count=1`：剧本编辑、存储、提示词包通过；HTTP 全套有 2 个既有批量测书测试失败，分别为 `TestCustomCopyLegacyTextRequiresAuthoritativeReselection` 和 `TestFailedProcessingRetriesOnlyVerifiedRawAndKeepsCaptureSize`，与本次剧本编辑路径无关，未误报全绿。
- `node --test src/user/pages/script-editor/*.test.js src/user/pages/script-editor-page.source.test.js`：10/10 通过。
- `npm run build`：通过。
- `/health`：`ok=true`。
- `/api/runtime-build-info`：`git_sha=c3932619`，`runtime_manifest_sha=c3932619`，前端入口 `/assets/user-BIPAnHzG.js`。

## 本次没有执行

- 没有提交 AI 生成、审稿或优化任务，避免产生真实模型费用；后台执行、局部失败、重试和采用新版本均由假模型自动化测试覆盖。
- 没有上传公网或发布生产环境。
