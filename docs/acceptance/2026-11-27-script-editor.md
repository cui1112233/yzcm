# 剧本编辑本机验收记录

日期：2026-11-27
验收环境：本机 `http://127.0.0.1:4001/script-editor`
运行二进制：`backend/qiantie-local.exe`
运行版本：`1e22c0f7`

## 已完成

- 新增侧边栏入口“剧本编辑”，不改动原有“私人定制”和“漫剧创作”。
- 支持作品库、新建作品、搜索、筛选和排序。
- 支持 TXT、Markdown、DOCX 预览导入；旧版 DOC 会明确提示不支持。
- 支持原文章节新增、编辑、排序、删除和自动保存。
- 支持分集剧本手工创建、自动保存、复制版本、删除版本和设为正式版。
- 支持用户主动点击后，使用已配置文本模型生成剧本；系统提示词只保存在 Go 后端。
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

验收过程中发现并修复了两个真实运行问题：MySQL 8.4 拒绝 binary 字符集 JSON 参数；剧本正文自带“第1集”时，交接包装会重复生成集标题。两项均有回归测试，最终修复提交为 `1e22c0f7`。

## 自动化验证

- `go test ./internal/scripteditor ./internal/storage -count=1`：通过。
- `go test ./internal/httpapi ./internal/prompt -run 'ScriptEditor|ScriptEditorGenerationPrompt' -count=1`：通过。
- `node --test src/user/pages/script-editor/*.test.js`：6/6 通过。
- `npm run build`：通过。
- `/health`：`ok=true`。
- `/api/runtime-build-info`：`git_sha=1e22c0f7`，`runtime_manifest_sha=1e22c0f7`。

## 本次没有执行

- 没有点击 AI 生成，避免产生真实模型费用；接口和提示词路径已由自动化测试覆盖。
- 没有上传公网或发布生产环境。
- AI 生成当前是用户点击后同步完成；设计中的 Redis 可恢复后台任务、取消、失败重试和审稿面板尚未实现，不能把这一部分称为已验收。
