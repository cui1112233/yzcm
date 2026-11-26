# 剧本编辑本机验收记录

日期：2026-11-27  
验收环境：本机 `http://127.0.0.1:4001/script-editor`  
运行二进制：`backend/qiantie-local.exe`  
运行版本：`71e9cd3e`  

## 已完成

- 新增侧边栏入口“剧本编辑”，不改动原有“私人定制”和“漫剧创作”。
- 支持作品库、新建作品、搜索、筛选和排序。
- 支持 TXT、Markdown、DOCX 预览导入；旧版 DOC 会明确提示不支持。
- 支持原文章节新增、编辑、排序、删除和自动保存。
- 支持分集剧本手工创建、自动保存、复制版本、删除版本和设为正式版。
- 支持用户主动点击后，使用已配置文本模型生成剧本；系统提示词只保存在 Go 后端。
- 支持只导出正式版：合并 TXT、分集 ZIP、结构化 JSON。
- “导出交付”页说明剧本编辑与漫剧创作的分工，并提供打开漫剧创作的入口。

## 真实页面验收

使用已登录账号 `choushiyiguai` 创建作品 `复刻验收-20261127-0239`：

1. 创建并保存 1 个原文章节，刷新页面后仍显示 `1 章 · 38 字`。
2. 创建第 1 集 V1，输入 52 字剧本正文，刷新页面后正文和字数仍在。
3. 将 V1 设为正式版后，TXT、ZIP、JSON 三个导出入口均可用。
4. 页面最终保留在“导出交付”页，供继续查看。

验收过程中发现并修复了 MySQL 8.4 JSON 入库问题：Go 的 `[]byte` 会被 MySQL 当成 binary 字符集并报 3144；现已改成 JSON 文本参数，并添加回归测试。修复提交：`71e9cd3e`。

## 自动化验证

- `go test ./internal/scripteditor ./internal/storage -count=1`：通过。
- `go test ./internal/httpapi ./internal/prompt -run 'ScriptEditor|ScriptEditorGenerationPrompt' -count=1`：通过。
- `node --test src/user/pages/script-editor/*.test.js`：6/6 通过。
- `npm run build`：通过。
- `/health`：`ok=true`。
- `/api/runtime-build-info`：`git_sha=71e9cd3e`，`runtime_manifest_sha=71e9cd3e`。

## 本次没有执行

- 没有点击 AI 生成，避免产生真实模型费用；接口和提示词路径已由自动化测试覆盖。
- 没有上传公网或发布生产环境。
- 当前“继续制作”是打开漫剧创作入口，不会自动搬运项目数据；自动交接和持久化后台 AI 队列不在本次复刻范围内。
