# 洛水剧本创作差距落实：总执行计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

依据：用户已确认执行`docs/acceptance/2026-11-26-luoshui-development-materials-screenplay-gap-audit.md`。目标是按原件功能合同逐项补齐，不把文档、按钮、模板条数当作完整复刻完成。

## 全局约束

- 只在现有v88主线记录改动，不新建分支；保留其他会话脏文件。
- Go后端、React+Antd、MySQL/Redis/TOS及现有模块边界不变；系统提示词归后端prompt模块。
- 无默认真实付费生成；受控生成器可以验证传输/持久化，不冒充供应商验收。
- 修改先有失败用例，相关测试通过后审查、提交；真实本地4001验收在发布之前。
- 原本可用的站位、多视图、媒体、重拍、已有规划和版本保护不回退。
- 维护一个进度账本，完成一个可验收任务就记提交/测试/限制；未做项保持待办。

## 子项目和顺序

| 顺序 | 核账项 | 交付要求 |
|---|---|---|
| 1 | D04 | 原文来源覆盖检查与结构检查分开；查出编号尾部遗漏/越界/重复；无来源时明确未核验，不冒充情节完整；已有补齐功能不拿来源编号当镜头序号 |
| 2 | D10 | 逐项核实138/293续写合同与现有规划/剧本服务；先红绿用例再开放协议；46条核心按用途和现有9条正文映射，接入真实选择/最终请求，不能只存文件 |
| 3 | D01 | 原创作/编辑工具映射到现有真实服务；读取与写入/付费动作分开；待确认、执行、失败、重复与保存回读可追踪；未知工具明确拒绝 |
| 4 | D02 | 作品内会话、正文/分镜/文档附件、文件/资产引用、Skills及权限模式；复用全局已有能力，但身份、作品范围和保存必须绑定 |
| 5 | D03 | 项目与默认工作流配置、步骤排序/启停、保存重读、依赖校验、后台执行和断点；原固定九步保留为默认方案 |
| 6 | D05 | 资产参考拼图/融合/溶图及宫格生成→拆分→绑定；参考素材归属、布局、模型能力、不完整输入及重复任务保护 |
| 7 | D06 | 白模上传/历史/预览/裁切/改名/删除与逐镜头/批量绑定，导演台结果回流，视频请求仅对支持的视频参考渠道开放 |
| 8 | D08 | 重拍video_ref与keyframes明确分支、智能切段/恢复、实际片段引用和拼接；不支持的模型明确禁用该方式 |
| 9 | D07 | 在现有工作台完成已选范围的简易/高级视图、单镜素材与多轨交互；切换不丢数据；旧版历史编辑器兼容另列，不能借此删现有能力 |
| 10 | D09 | 分模式导出、真实媒体/字幕/时长/轨道/路径完整性校验；本机剪映存在时验导入；应用缺失或付费实测未授权时保留明确验收阻塞 |
| 11 | 全部 | 同一作品保存重开、取消/迟到、失败重试、跨用户、选中版本、最终请求及导出核验；按同项原版证据更新台账，不能批量标完整通过 |

## 当前任务1的文件边界

- 业务：`backend/internal/screenplay/storyboard_check.go`及新增独立来源核对文件/测试。
- 路由：`backend/internal/httpapi/screenplay_routes.go`中检查端点，或抽到独立检查文件；对应`screenplay_check_routes_test.go`。
- 页面：`frontend/src/user/pages/ComicScreenplayStoryboardCheck.jsx`及其检查浏览器测试/可独立测试的格式转换函数。
- 不修改已有脏的CreationFields、ModelConfig、StoryboardSettings。

## 任务1可执行步骤

- [x] 1. 阅读编号原文、SourceElements/ProtocolFields、章节与作品来源及当前检查/补齐端点，确定真实可用的来源标识。
- [x] 2. 写失败用例：镜头序号连续但原文最后一项缺失、未知来源编号、重复来源、无原文/无来源映射、跨集来源误用。
- [x] 3. 在独立纯函数中核对显式来源；没有稳定来源标识时返回“来源覆盖未核验”，不让编号检查假装来源完整。普通未编号自然段不得凭空编号并猜镜头对应。
- [x] 4. 检查端点绑定所选集的实际原文/脚本来源，保留旧结构报告字段，新增独立来源报告和状态；只读检查不调用模型、不改库。
- [x] 5. 补齐端点仍按缺失镜头seq运行；来源缺口不得直接交给原seq补齐器；不能因检查把已生成图片/视频删除。
- [x] 6. 页面分别展示“镜头序号”和“原文来源覆盖”，仅来源确实核验后显示覆盖通过。报告下载保留两者，不堆额外解释提示。
- [x] 7. 跑Go业务/HTTP定向测试和真实组件测试；独立审查修正后提交；构建本地4001核验。
- [x] 8. 记录完成证据及下一项，不终止总任务或询问是否继续。

## 检查命令

- `cd backend; go test ./internal/screenplay -run 'Test.*(StoryboardCheck|SourceCoverage)' -count=1`
- `cd backend; go test ./internal/httpapi -run TestScreenplayRoutesStoryboardCheck -count=1`
- `cd frontend; node --test src/user/pages/comic-screenplay-storyboard-check*.test.js`
- 最后再跑screenplay全包及相关前端回归；跳过的浏览器测试不算真实浏览器通过。

## 2026-11-27复核索引

任务1勾选已按ce3d7b43及第一轮本地验收证据同步；仅表示D04显式来源检查交付，不表示普通原文语义审核或整体复刻完成。138/293及助手分层成果见第一轮交付记录；剩余工作与原件72个去重工具名逐项表见`docs/acceptance/2026-11-27-luoshui-screenplay-remaining-audit.md`。


## 2026-11-28 显式文字引用本地交付

D02六类当前作品文字引用已提交9bce3fbf，通过真实MySQL事务/行锁及4001接口、已登录Edge选择/预览/草稿隔离验收。代码、运行清单和前端入口已核对。详见docs/acceptance/2026-11-28-screenplay-assistant-references-local.md。专用付费开关仍关闭，真实模型调用0；文件/图片附件、Skills、Auto及其他D项保持待办，不能据本切片勾完D02或总计划。
