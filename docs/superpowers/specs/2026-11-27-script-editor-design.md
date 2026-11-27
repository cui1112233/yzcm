# 剧本编辑复刻设计

日期：2026-11-27  
目标入口：`/script-editor`  
用户可见名称：`剧本编辑`

## 1. 目标

在本地 4001 新增一个独立的“剧本编辑”工作台，复刻洛水“短剧工作室（作品库）”中与剧本生产直接有关的能力，形成以下真实闭环：

`导入小说或剧本 → 章节管理 → AI 分集剧本 → 多版本编辑与审稿 → 导出 → 一键导入漫剧创作`

“剧本编辑”负责决定拍什么、每集怎么写；现有“漫剧创作”继续负责人物、场景、道具、分镜、图片、视频、配音、生产任务和成片交付。两者不复制同一套媒体生产能力。

## 2. 已确认的产品边界

### 2.1 本次包含

- 独立左侧菜单“剧本编辑”和独立路由 `/script-editor`。
- 作品库：新建、导入、打开、搜索、类型筛选、标记、排序、宫格/列表视图、软删除。
- 原文：章节识别、章节增删改、拖动排序、全文搜索、自动保存。
- 剧本：按集管理、多版本、正式版本选择、复制成新版本、导入剧本、自动保存。
- 原文与剧本左右对照，剧本版本之间并排比较。
- AI 生剧本、剧本审稿、剧本优化、爆款开头分析；系统提示词全部由后端提供。
- 后台生成任务状态、取消、失败原因、重试和刷新恢复。
- 合并 TXT、逐集 TXT ZIP、结构化 JSON 导出。
- 选择正式版本后一键创建“漫剧创作”项目，并跳转到现有 `/comic-screenplay`。

### 2.2 本次不包含

- 人物、场景、道具资产制作。
- 分镜规划、分镜 JSON、生图、生视频、配音、字幕、AI-CUT、剪映草稿和成片。
- 洛水原版 momo 画布。
- 公网发布、付费模型真实调用和用户现有作品数据迁移。

这些能力继续由现有“漫剧创作”及其他生产模块承担。

## 3. 方案选择

采用“独立作品库 + 明确交接”的方案：

- 新建 `scripteditor` Go 业务包和 `script_editor_*` MySQL 表。
- 新建 React 页面，不把现有 `/script`“私人定制”改名，也不改造 `/comic-screenplay`。
- 复用现有账号鉴权、模型目录、提示词管理、Redis、文件下载和漫剧项目服务。
- 通过后端内部服务把选定剧本转换成漫剧项目；前端不得拼装或直接写漫剧数据库。

这样可以避免三类冲突：私人定制的本地草稿语义、漫剧创作的生产项目语义、剧本编辑的多作品多版本语义。

## 4. 页面设计

### 4.1 左侧入口

在“小说创作”和“水货生产”附近增加：

- 图标：`FilePenLine`
- 名称：`剧本编辑`
- 地址：`/script-editor`

现有“私人定制”“漫剧创作”的名称和地址保持不变。

### 4.2 作品库

页头文案：

- 标题：`剧本编辑`
- 副标题：`导入小说或剧本，按章、按集整理并生成正式剧本`

主要操作：

- `新建作品`
- `导入作品`
- 搜索作品
- 类型筛选：`全部 / 小说 / 剧本`
- 标记筛选：`重点 / 待完成 / 已完成 / 未标记`
- 排序：更新时间升降序、名称升降序
- 宫格/列表切换

作品卡展示封面占位、名称、类型、章节数、剧本集数、总字数、更新时间和标记。卡片菜单提供打开、重命名、标记和删除。删除使用软删除，二次确认文案为：

`确定删除“{作品名}”吗？作品会进入已删除状态，不会立即清除正文和剧本。`

### 4.3 新建与导入

新建空白作品要求填写作品名和类型。作品名去除首尾空格后不能为空，最长 120 个字符。

导入支持：

- `.txt`
- `.md`
- `.docx`

单文件最大 20 MiB。旧 `.doc` 不在 Go 运行时内调用 Word COM，界面明确提示：`暂不支持旧 DOC，请在 Word 中另存为 DOCX 后导入。`

后端识别“第N章、第二章、第N集、第一集、Chapter N、Episode N”等标题。没有识别到标题时按“全文”导入，并在用户确认前展示识别预览，不静默拆分。

### 4.4 作品详情

作品详情顶部显示返回、作品名、章节数、剧本集数、总字数和保存状态。主区域包含三个页签：

1. `原文`
2. `剧本`
3. `导出与交接`

#### 原文页签

- 左栏：章节列表、新建章节、搜索、拖动排序。
- 中栏：章节标题和正文编辑器。
- 修改后 800 ms 防抖自动保存。
- 保存状态固定使用：`待保存… / 保存中… / 已保存 / 保存失败`。
- 离开未保存章节前先尝试保存；保存失败则留在当前章节。

#### 剧本页签

- 左栏按集显示剧本，同一集的版本相邻。
- 中间默认显示原文与正式剧本左右对照。
- 多版本比较可打开 2 至 4 个版本并排查看。
- 每个版本都有独立正文、字数、生成来源、模型、提示词版本、创建时间和修订号。
- `设为正式版`每集只能有一个；操作必须带当前修订号。
- AI 生成、复制和导入永远新建版本，不覆盖已有版本。
- 用户手工编辑采用 800 ms 防抖自动保存；发生版本冲突时不覆盖服务器内容，提示用户重新载入或复制本地内容为新版本。

工具栏：

- `AI 生剧本`
- `新建空白版本`
- `导入剧本`
- `复制为新版本`
- `设为正式版`
- `AI 审稿`
- `AI 优化`
- `爆款开头`
- `查找替换`

#### 导出与交接页签

导出范围支持全部集或勾选集数。多版本存在但没有正式版的集数不能导出，界面列出缺少正式版的集数。

导出格式：

- 合并为一个 TXT
- 每集一个 TXT，并打包 ZIP
- 结构化 JSON，保留集数、版本、原文章节绑定和正文

“导入漫剧创作”要求：

- 至少选择一集。
- 每集必须存在正式版；只有一个版本时允许在确认弹窗中自动设为正式版。
- 项目名默认使用作品名，可修改；重名时不覆盖旧项目。
- 提交前展示将导入的集数、版本和总字数。
- 成功后记录交接关系，并跳转 `/comic-screenplay?projectId={id}`。

## 5. 数据模型

所有业务数据按当前登录账号隔离。主键使用不可猜测 ID，所有可编辑实体都有 `revision`，写操作采用乐观锁。

### 5.1 `script_editor_works`

- `id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `title VARCHAR(120)`
- `work_type VARCHAR(16)`：`novel` 或 `script`
- `source_filename VARCHAR(255)`
- `mark_status VARCHAR(16)`：空、`key`、`pending`、`done`
- `revision BIGINT`
- `created_at DATETIME(6)`
- `updated_at DATETIME(6)`
- `deleted_at DATETIME(6) NULL`

### 5.2 `script_editor_chapters`

- `id VARCHAR(64)`
- `work_id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `title VARCHAR(160)`
- `sort_order INT`
- `content MEDIUMTEXT`
- `word_count INT`
- `revision BIGINT`
- `created_at DATETIME(6)`
- `updated_at DATETIME(6)`

### 5.3 `script_editor_scripts`

- `id VARCHAR(64)`
- `work_id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `episode INT`
- `version INT`
- `title VARCHAR(160)`
- `content MEDIUMTEXT`
- `source_chapter_ids JSON`
- `preferred BOOLEAN`
- `generation_source VARCHAR(24)`：`manual`、`import`、`ai`、`copy`
- `model_id VARCHAR(191)`
- `prompt_id VARCHAR(191)`
- `prompt_revision BIGINT`
- `revision BIGINT`
- `created_at DATETIME(6)`
- `updated_at DATETIME(6)`

唯一约束保证同一作品的 `episode + version` 不重复。设置正式版在事务中先清除同集其他正式版，再设置目标版本。

### 5.4 `script_editor_reviews`

- `id VARCHAR(64)`
- `work_id VARCHAR(64)`
- `script_id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `review_type VARCHAR(32)`：`source_map`、`dialogue_diff`、`polish`、`viral_opening`
- `result_json JSON`
- `model_id VARCHAR(191)`
- `prompt_id VARCHAR(191)`
- `created_at DATETIME(6)`

审稿结果只做建议；用户明确点击“应用”后才新建剧本版本，不直接覆盖正文。

### 5.5 `script_editor_jobs`

- `id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `work_id VARCHAR(64)`
- `job_type VARCHAR(32)`
- `status VARCHAR(24)`：`queued`、`running`、`succeeded`、`partial`、`failed`、`cancelled`
- `request_key VARCHAR(96)`
- `input_json JSON`
- `result_json JSON`
- `progress_current INT`
- `progress_total INT`
- `error_code VARCHAR(64)`
- `error_message TEXT`
- `created_at DATETIME(6)`
- `updated_at DATETIME(6)`

MySQL保存任务真相，Redis承担执行队列和短期锁。相同账号、任务类型和 `request_key` 必须幂等，不重复扣费或重复创建版本。

### 5.6 `script_editor_handoffs`

- `id VARCHAR(64)`
- `owner_username VARCHAR(191)`
- `work_id VARCHAR(64)`
- `request_key VARCHAR(96)`
- `screenplay_project_id VARCHAR(64)`
- `episodes_json JSON`
- `created_at DATETIME(6)`

交接记录用于重试回读。相同 `request_key` 再次提交时返回原项目，不创建重复漫剧项目。

## 6. 后端接口

统一前缀：`/api/script-editor`。

### 6.1 作品与章节

- `GET /works`
- `POST /works`
- `GET /works/{workID}`
- `PATCH /works/{workID}`
- `DELETE /works/{workID}`
- `POST /imports/preview`
- `POST /imports/commit`
- `POST /works/{workID}/chapters`
- `PATCH /works/{workID}/chapters/{chapterID}`
- `DELETE /works/{workID}/chapters/{chapterID}`
- `POST /works/{workID}/chapters/reorder`
- `GET /works/{workID}/search?q=...&scope=...`

### 6.2 剧本与版本

- `GET /works/{workID}/scripts`
- `POST /works/{workID}/scripts`
- `PATCH /works/{workID}/scripts/{scriptID}`
- `DELETE /works/{workID}/scripts/{scriptID}`
- `POST /works/{workID}/scripts/{scriptID}/copy`
- `POST /works/{workID}/scripts/{scriptID}/prefer`
- `POST /works/{workID}/scripts/import/preview`
- `POST /works/{workID}/scripts/import/commit`

所有修改接口都要求 `expectedRevision`。冲突返回 HTTP 409，并携带当前服务器修订号。

### 6.3 AI 与任务

- `POST /works/{workID}/jobs/script-generation`
- `POST /works/{workID}/jobs/review`
- `GET /works/{workID}/jobs`
- `GET /works/{workID}/jobs/{jobID}`
- `POST /works/{workID}/jobs/{jobID}/cancel`
- `POST /works/{workID}/jobs/{jobID}/retry`

前端只发送作品、章节、模型、用户补充要求和提示词槽位 ID。后端读取并组合系统提示词正文。缺少模型、提示词或原文时在排队前拒绝，不能建立假任务。

### 6.4 导出与交接

- `POST /works/{workID}/exports`
- `GET /works/{workID}/exports/{exportID}/download`
- `POST /works/{workID}/handoffs/comic-screenplay`
- `GET /works/{workID}/handoffs`

交接由后端调用现有漫剧创作服务，在一个事务边界内创建项目和剧本文档。前端不直接访问数据库。

## 7. AI 生成规则

- 系统提示词新增后端模块 `script-editor`，前端只展示名称和说明。
- AI 生剧本可以选择原文章节、每集包含章节数、生成集数、每集版本数、模型和补充要求。
- 每个生成结果落为新版本，保留模型和提示词版本信息。
- 批量任务允许部分成功；成功集立即保存，失败集保留明确原因并允许只重试失败项。
- 自动重试不得重新生成已成功集，不得修改用户手工编辑过的版本。
- 取消后停止领取新集；已经收到模型完整结果的集仍保存，并把任务标记为 `partial` 或 `cancelled`。
- 未获得用户明确授权时，验收只使用假模型，不触发付费请求。

## 8. 错误与恢复

固定处理规则：

- 401：跳转登录，不清除页面草稿。
- 403：显示无权限，不泄露作品是否存在。
- 404：返回作品库并提示作品不存在或已删除。
- 409：保留本地内容，提供“重新载入”和“复制为新版本”。
- 413：提示文件超过 20 MiB。
- 422：显示具体字段错误或无法识别的文档格式。
- 生成失败：保存错误码、供应商摘要和已完成集数；密钥及完整上游响应不得返回前端。
- 页面刷新：从 MySQL 重读作品和任务；运行中任务从 Redis/任务记录恢复显示。
- 服务重启：`running` 且无有效执行租约的任务回到可恢复状态，不直接重复调用模型。

## 9. 文件边界

计划新增的主要文件：

- `backend/internal/scripteditor/`：模型、服务、MySQL存储、导入解析、生成任务、导出、漫剧交接。
- `backend/internal/storage/script_editor_schema.go`：MySQL迁移。
- `backend/internal/httpapi/script_editor_routes.go`：HTTP路由和权限校验。
- `backend/internal/prompt/`中的剧本编辑提示词注册文件。
- `frontend/src/shared/api/scriptEditor.js`：前端API。
- `frontend/src/user/pages/ScriptEditorPage.jsx`：路由外壳。
- `frontend/src/user/pages/script-editor/`：作品库、原文编辑、剧本版本、任务、导出交接组件。
- `frontend/src/user/pages/script-editor.css`：页面样式。

单个组件只承担一种职责，避免再形成一个上万行页面文件。

## 10. 分阶段交付

### 阶段一：本地数据闭环

- 菜单和路由。
- 作品库、新建、导入预览与提交。
- 章节编辑、排序、搜索。
- 分集剧本、多版本、正式版、自动保存。
- 刷新重读和账号隔离。

验收：不调用AI，也能从导入文件一直编辑到正式分集剧本；重启4001后数据不丢。

### 阶段二：AI 剧本与审稿

- 后端提示词槽位。
- 生成任务、Redis队列、进度、取消、失败恢复。
- AI生剧本、审稿、优化和爆款开头。

验收：假模型覆盖全部状态；真实模型只有在用户另行明确授权后测试。

### 阶段三：导出与漫剧交接

- TXT、ZIP、JSON导出。
- 正式版完整性检查。
- 幂等创建漫剧项目、写入分集文档、记录交接并跳转。

验收：用本地测试作品完成一次真实MySQL交接；漫剧创作能打开对应项目、看到正确集数和正文；重复提交不产生第二个项目。

### 阶段四：4001整体验收

- 前端测试和构建。
- Go专项测试和相关包回归。
- 重建前端并嵌入Go二进制。
- 重启本地4001。
- 检查 `/health`、`/api/runtime-build-info`。
- 在已登录浏览器实际完成新建、导入、编辑、刷新、多版本、导出和交接。

只有源码、测试、嵌入构建、4001运行和浏览器真实操作都通过，才能称为完成。

## 11. 测试与验收标准

### 后端

- MySQL迁移在空库和已有库都可重复执行。
- 账号隔离、软删除、修订冲突、同集正式版唯一。
- TXT/MD/DOCX解析；无标题全文、中文数字、英文标题、空正文、超大文件。
- AI任务幂等、部分成功、取消、只重试失败项、重启恢复。
- 导出内容、文件名清洗和集数顺序。
- 漫剧交接事务、正式版缺失、重名、重复请求回读。

### 前端

- 筛选排序不修改原始数组。
- 导入预览确认前不创建作品。
- 自动保存成功、失败和409冲突。
- 多版本比较及正式版选择。
- 任务刷新恢复和取消。
- 未选正式版时禁止导出和交接。
- 交接成功后跳转正确项目。

### 4001运行验收

- 左侧出现“剧本编辑”，原有菜单不消失。
- 本地创建的测试作品不会改动用户已有作品。
- 刷新、退出重进、服务重启后数据仍在。
- 导出的TXT/ZIP/JSON可实际下载并读取。
- 导入漫剧创作后，项目标题、集数、顺序和正文一致。
- 未经授权不产生真实模型费用，不发布公网。

## 12. Git与工作区约束

- 所有修改直接在当前 `v88` 分支完成，不新建零散分支。
- 每个阶段只提交该阶段相关文件，不夹带现有无关改动。
- 不重置、清理、覆盖当前脏工作区。
- 不复制编译产物回源码实现页面。
- 不推送远端，不发布公网；除非用户另行明确要求。
