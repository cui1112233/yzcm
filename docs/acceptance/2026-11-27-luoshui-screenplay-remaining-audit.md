# 洛水漫剧创作：执行后遗漏复核

日期：2026-11-27。源码v88核对点6eb3fcd9；本地4001实际运行db2ee863，manifest一致。本次只查代码和资料，没有调用付费模型或上线。

## 结论

原件创作40条、编辑38条，共72个去重工具名。当前其中9个读取工具已接，3个资产修改工具只接update分支，剩余60个名称尚未接入作品助手工具调度。另有本系统原有storyboard_failures。这个数量只统计工具名，不是产品功能完成率；手动页面已有能力不算缺失，也不能算助手已经能调用。

## 仍未完成的工作

| 项目 | 当前真实状态 | 需要补的内容 |
|---|---|---|
| D01 助手工具 | 读取与三类资产更新已接，确认/重复保护已测 | 创建删除、镜头绑定/增删改、台词/情绪审核修正、生成、拆图及导出工具逐一接现有服务 |
| D02 会话工作区 | 当前仍为作品内单份消息和方案面板 | 多会话/子会话、文件附件、@引用、Skill选择及权限范围，不能把批次session_id当会话界面完成 |
| D03 自定工作流 | state.go仍为固定九步 | 步骤选择排序、依赖检查、全局/项目保存、后台执行与断点 |
| D04 来源检查 | 显式来源编号检查及本地验收完成 | 普通未编号原文的语义遗漏/衔接核对尚未实现；当前明确返回未核验，不猜映射 |
| D05 图像制作 | 已有普通生图和split_grid | 参考拼图/融合/溶图、宫格生成再拆分绑定的完整流程 |
| D06 白模导演台 | 站位图已有 | 白模视频库、裁切历史、逐镜/批量绑定、导演台结果回流与视频参考请求 |
| D07 工作台 | 已有镜头/媒体/配音/时间线 | 简易高级同页切换、单镜素材和多轨完整交互对照；不能称一比一 |
| D08 重拍 | screenplay_reshoot.go仍固定ReshootModeFrames/image_to_video | video_ref分支、模型能力限制、智能切段和恢复 |
| D09 交付 | 媒体包/基础剪映草稿/成片已有 | 分模式导出完整核对，真实剪映打开草稿验收仍缺 |
| D10 提示词协议 | 138/293已支持明确逐批继续 | 138自动生产衔接未完成；46条核心逐用途绑定及最终请求核对未完成，原审计37条未同文不能当37个功能 |

## 本轮额外发现的收尾遗漏

- 总计划任务1的勾选仍全为空，与已提交成果不一致；进度在单独账本，容易误读。需要以验收报告交叉判断，不能凭旧勾选重新做或宣称没做。
- 来源审计原文写“仅storyboard_failures”“138/293受限”属于11月26日历史快照，现状已变化；本复核与第一轮交付报告是后续状态，不覆盖原证据。
- 已接读取工具的部分参数不同：当前支持shot_numbers和单shot_id，原件部分工具另支持shot_ids数组；需要明确适配，不宜标为完整相同。
- 助手前端将storyboard_failures之外的读取统一显示“只读工具”；新增工具缺中文逐项名称和更明确的执行状态展示。
- 读取/准备修改的独立接口目前用于复用与验收；用户页面通过聊天和确认方案使用，不应把没有独立按钮误报成业务未接。
- 资产确认已补MySQL事务和旧不完整方案拦截；其余镜头、正文、章节动作需要分别核查失败保存与重复保护，不能沿用资产测试结论。
- 完整同作品链路的保存重开、取消后迟到回包、跨用户、选中版本、实际供应商输出与最终导出对照还未整体验收。

## 证据入口

- 原件：归档20-backend.md §6.2/7.2与21前端、98勘误。
- 当前助手：assistant_tools.go、assistant_asset_edit.go、screenplay_authoring_routes.go、ComicScreenplayAuthoringPanels.jsx。
- 固定工作流：backend/internal/screenplay/state.go StepDefinitions。
- 重拍：backend/internal/httpapi/screenplay_reshoot.go Mode/GenerationMode。
- 导出边界：backend/internal/httpapi/screenplay_media_workbench.go jianying capability及说明文件。
- 已完成的提交/本地证据：docs/acceptance/2026-11-26-luoshui-parity-first-delivery.md。

## 原件工具逐项对照

| 原件场景 | 工具名 | 当前状态 |
|---|---|---|
| 创作 | `run_screenplay_agent` | 未接入作品助手工具调度 |
| 创作 | `batch_generate_chapter_scripts` | 未接入作品助手工具调度 |
| 创作 | `batch_generate_chapter_storyboards` | 未接入作品助手工具调度 |
| 创作 | `run_character_agent` | 未接入作品助手工具调度 |
| 创作 | `run_scene_agent` | 未接入作品助手工具调度 |
| 创作 | `run_prop_agent` | 未接入作品助手工具调度 |
| 创作 | `run_voice_agent` | 未接入作品助手工具调度 |
| 创作 | `run_all_agents` | 未接入作品助手工具调度 |
| 创作 | `regenerate_character_info` | 未接入作品助手工具调度 |
| 创作 | `modify_character` | 仅update已接入，add/delete未接 |
| 创作 | `modify_scene` | 仅update已接入，add/delete未接 |
| 创作 | `modify_storyboard` | 未接入作品助手工具调度 |
| 创作 | `modify_prop` | 仅update已接入，add/delete未接 |
| 创作 | `run_director_agent` | 未接入作品助手工具调度 |
| 创作 | `analyze_screenplay` | 读取已接入，筛选参数有差异 |
| 创作 | `get_current_screenplay` | 读取已接入，筛选参数有差异 |
| 创作 | `get_shot_numbers` | 读取已接入，筛选参数有差异 |
| 创作 | `fix_shot_numbers` | 未接入作品助手工具调度 |
| 创作 | `review_video_prompts` | 未接入作品助手工具调度 |
| 创作 | `modify_video_prompt` | 未接入作品助手工具调度 |
| 创作 | `run_character_parse_agent` | 未接入作品助手工具调度 |
| 创作 | `run_scene_parse_agent` | 未接入作品助手工具调度 |
| 创作 | `run_prop_parse_agent` | 未接入作品助手工具调度 |
| 创作 | `run_asset_quality_review_agent` | 未接入作品助手工具调度 |
| 创作 | `run_character_complete_check_agent` | 未接入作品助手工具调度 |
| 创作 | `run_scene_complete_check_agent` | 未接入作品助手工具调度 |
| 创作 | `run_prop_complete_check_agent` | 未接入作品助手工具调度 |
| 创作 | `run_all_asset_complete_check_agents` | 未接入作品助手工具调度 |
| 创作 | `run_synopsis_agent` | 未接入作品助手工具调度 |
| 创作 | `generate_shot_image` | 未接入作品助手工具调度 |
| 创作 | `generate_shot_video` | 未接入作品助手工具调度 |
| 创作 | `modify_shot_image` | 未接入作品助手工具调度 |
| 创作 | `modify_asset_image` | 未接入作品助手工具调度 |
| 创作 | `batch_generate_images` | 未接入作品助手工具调度 |
| 创作 | `batch_generate_videos` | 未接入作品助手工具调度 |
| 创作 | `split_shot_image` | 未接入作品助手工具调度 |
| 创作 | `export_jianying_video` | 未接入作品助手工具调度 |
| 创作 | `split_shot_image_cell` | 未接入作品助手工具调度 |
| 创作 | `split_all_shots_image` | 未接入作品助手工具调度 |
| 创作 | `open_asset_manager` | 未接入作品助手工具调度 |
| 编辑 | `review_image_prompts` | 未接入作品助手工具调度 |
| 编辑 | `modify_image_prompt` | 未接入作品助手工具调度 |
| 编辑 | `batch_optimize_image_prompts` | 未接入作品助手工具调度 |
| 编辑 | `review_video_prompts` | 未接入作品助手工具调度 |
| 编辑 | `modify_video_prompt` | 未接入作品助手工具调度 |
| 编辑 | `review_asset_bindings` | 未接入作品助手工具调度 |
| 编辑 | `fix_asset_binding` | 未接入作品助手工具调度 |
| 编辑 | `auto_fix_all_bindings` | 未接入作品助手工具调度 |
| 编辑 | `director_review` | 未接入作品助手工具调度 |
| 编辑 | `modify_voice_text` | 未接入作品助手工具调度 |
| 编辑 | `get_editor_status` | 读取已接入，筛选参数有差异 |
| 编辑 | `optimize_shot_prompts` | 未接入作品助手工具调度 |
| 编辑 | `modify_script_text` | 未接入作品助手工具调度 |
| 编辑 | `get_shot_detail` | 读取已接入，筛选参数有差异 |
| 编辑 | `split_shot_image_cell` | 未接入作品助手工具调度 |
| 编辑 | `add_character` | 未接入作品助手工具调度 |
| 编辑 | `modify_character` | 仅update已接入，add/delete未接 |
| 编辑 | `delete_character` | 未接入作品助手工具调度 |
| 编辑 | `add_scene` | 未接入作品助手工具调度 |
| 编辑 | `modify_scene` | 仅update已接入，add/delete未接 |
| 编辑 | `delete_scene` | 未接入作品助手工具调度 |
| 编辑 | `add_prop` | 未接入作品助手工具调度 |
| 编辑 | `modify_prop` | 仅update已接入，add/delete未接 |
| 编辑 | `delete_prop` | 未接入作品助手工具调度 |
| 编辑 | `get_all_shots_voicetext` | 读取已接入，筛选参数有差异 |
| 编辑 | `compare_original_vs_voicetext` | 未接入作品助手工具调度 |
| 编辑 | `auto_generate_missing_shots` | 未接入作品助手工具调度 |
| 编辑 | `review_dialogue_supplements` | 未接入作品助手工具调度 |
| 编辑 | `apply_dialogue_supplements` | 未接入作品助手工具调度 |
| 编辑 | `split_long_shots` | 未接入作品助手工具调度 |
| 编辑 | `review_voice_emotions` | 未接入作品助手工具调度 |
| 编辑 | `fix_voice_emotions` | 未接入作品助手工具调度 |
| 编辑 | `read_shot_video_prompts` | 读取已接入，筛选参数有差异 |
| 编辑 | `read_shot_image_prompts` | 读取已接入，筛选参数有差异 |
| 编辑 | `read_assets` | 读取已接入，筛选参数有差异 |
| 编辑 | `add_shot` | 未接入作品助手工具调度 |
| 编辑 | `delete_shots` | 未接入作品助手工具调度 |
| 编辑 | `bind_shot_assets` | 未接入作品助手工具调度 |

## 本次执行后的增量（不覆盖上面的审计快照）

- e8a12dd1已补三类modify_*的add/delete及六个add_/delete_别名；资产增删确认、真实MySQL、引用拒删、重复保护均已本地验收，独立复查通过。剩余未接入助手的原件去重名称由60减至54。
- a6bd32ee、d868a3c1已补shot_ids及严格参数解析；四工具本地HTTP指定ID/缺失/混用/大写均验收通过。九个读取工具中文名已补，真实Chrome组件测试通过。
- 本地实际运行1e22c0f7是本任务提交的后代；保留其他任务本地更新，未退回旧版本。本任务未远端发布。证据见第一轮交付报告最新章节。

- 96530f05补三种镜头文字工具的直接更新分支，真实4001确认/重复/保留字段/恢复已验收；未接助手名称由54减到51。modify_script_text、AI重写、镜头新增删除仍未接，不把部分工具算全分支完成。
- 本地最终运行4a276374，包含上述成果和历史迁移校验修复。详细证据及不支持分支见第一轮交付报告最新章节；不是整体对标完成或线上验收。
