# 洛水开发资料与一战晟铭漫剧创作：差距核账

核查日期：2026-11-26。核查对象：用户提供的《洛水2026_复刻开发资料.zip》中的剧本创作功能，与当前 V88 漫剧创作。此次仅分析、运行现有测试及整理报告，没有修改产品，没有调用付费模型，没有重启或部署。

## 结论及数量口径

主干已经存在：原文/章节 → 剧本 → 人物场景道具 → 分镜 → 图片视频/音色字幕 → 时间线/成片。不能把现状说成“只做了页面”。但也不能称为洛水剧本创作完整复刻。

现有36组台账记录为13组 implemented、23组 partial。implemented只表示已有实现记录，partial可能缺功能、缺对应入口，也可能主要缺完整验收。该分组不是功能完成百分比，更不能说23组完全没有代码。台账的完整原版对照仍没有整组关闭，属于验收记录口径。

本次按新资料和当前源码，把明确需要继续补齐的内容归并为下面10个工作包。这是本次检查覆盖范围内的重点，不是假定已穷尽包内每个按钮、渠道和历史版本。

## 资料与当前源码身份

- ZIP SHA256：830289365e0badcabcec29cf6925b5b28c2d5129c79218f039a0ccf6dabbae14。
- 共105个成员；69份Markdown，另有JS/CSS/JSON/HTML等。重点阅读20后端、21前端、98验收勘误和剧本工作台/生产工作区等附录。
- 原件、选取文件摘要：`_local-archive-20261028/luoshui-dev-audit-20261126/source-manifest.json`；98文件为后续单独选取的`98-corrections.md`。
- 当前V88 HEAD：7a104150。相关当前文件摘要、未提交前端改动记录：同目录`current-source-snapshot.json`。制作字段、模型配置、分镜设置等文件有其他会话未提交改动，故本报告的源码检查不代表这些改动已上线。
- 资料中的GAPS和98报告描述的是那份Vue演示复刻工程，不是一战晟铭；里面的mock对照不能当作真实模型执行成功。
- 资料中的Python/Vue/SQLite、pywebview目录、113个硬编码模型等不直接照搬。本项目继续用Go、React+Antd、MySQL/Redis/TOS，功能差距不包含技术栈不同。

## 十个待补工作包

| 编号 | 工作包 | 洛水资料中的能力 | 我们当前实际情况 | 判定/优先级 |
|---|---|---|---|---|
| D01 | 完整Agent工具调度 | 20后端§6.2列出40个创作调度工具，§7.2列出38个编辑调度工具；可选集、解析、改资产、生图、生视频、检查、导出 | 作品助手能提出修改方案并确认应用；实时工具分发只接受`storyboard_failures`，执行一轮工具回读。手动功能存在不等于Agent能调用 | 部分接入；P0 |
| D02 | Agent工作区与Skills | 21前端§5.1包含多会话、子会话、作品文件、剧本/分镜/文档/图片附件、@引用、/Skill及会话权限 | 当前作品助手是单一作品消息/方案面板，没有对应的完整会话抽屉、附件/文件面板和Skills选择绑定。全局Agent模块的能力不能直接算漫剧入口已接通；现有成员权限、确认方案要保留 | 入口和能力链未完整接入；P1 |
| D03 | 可编辑的Agent工作流 | 21前端§5.2的AgentWorkflowModal，可选步骤和顺序，保存全局/项目流水线，区分输入、摘要、三类解析、生成等 | 已有九步生产、定时、暂停、恢复；`state.go`/`batch_executor.go`是固定生产步骤，没有同等自定义Agent流水线界面与保存/执行合同 | 缺对应编排；P1 |
| D04 | 按原文核对分镜覆盖 | 原文编号/来源与分镜对照，查漏、重复、越界及衔接修正 | `storyboard_check.go`检查现有镜头序号1..max是否连续；源码明确`Extra`恒为空。这不能证明原文情节没有遗漏，也不能识别所有新增情节。已有剧本医生/分镜质检不能直接当作同一规则 | 行为差异，容易误认为“检查通过即完整”；P0 |
| D05 | 宫格生图与参考融合/溶图 | 20后端§10.2–10.6包含宫格生成/分格、带名称的场景角色道具参考拼图、组合参考后送模型并写回镜头 | 已有`split_grid`裁切保存及绑定、资产参考图和普通图片生成；在漫剧前端及对应screenplay/httpapi模块未找到完整角色融合/溶图配置、合成、生成、回写闭环。拆已有图不是整条融合链 | 部分能力存在，缺完整制作链；P1 |
| D06 | 白模库与3D导演台回流 | 21前端§8.8：白模上传、历史、预览、裁切、改名、逐镜头/批量绑定；导演台导出视频后自动入库 | 站位分析、参考图、文字标注和PNG保存已做；在漫剧关联模块未找到同等白模视频库与导演台录制→库→镜头→视频模型的完整绑定。其他模块已有导演台不等于漫剧接通 | 漫剧对应入口/闭环未找到；P1 |
| D07 | 分镜生产工作区完整交互 | 21前端§6、§8的简易/高级工作区、单镜素材及同页多轨/逐句配音等 | 已有镜头编辑、媒体、配音字幕、时间线及进入AI-CUT；没有证明同页多轨、模式切换、所有字段和保存行为与原版等价。旧版编辑器兼容应单列范围，不因资料含旧版就全部推倒重做 | 部分实现与交互对齐待补；P2 |
| D08 | 片段重拍的全部参考方式 | 21前端§8.5同时支持整段视频参考`video_ref`与首尾帧`keyframes`，还有画面变化智能切段/恢复 | 已有真实抽帧、片段生成、拼接回写；`screenplay_reshoot.go`创建任务固定为`ReshootModeFrames`，生成固定`image_to_video`，不是两种参考方式均已接入。智能切段也不能由现有普通区间重拍推断完成 | 功能分支未完整覆盖；P2 |
| D09 | 原版完整剪映交付 | 20后端§11、21前端§6.7涉及不同导出方式、媒体/字幕/轨道配套与实际打开草稿 | 已有媒体包、基础轨道剪映兼容草稿、MP4成片；`screenplay_media_workbench.go`明确声明尚未在本机新版剪映验证导入。不能把zip可下载等同原版草稿已完整可用 | 部分实现、真实导入验收未完成；P1 |
| D10 | 提示词及输出协议覆盖 | 包内3条剧本大师、41条剧本分镜，以及46条“剧本核心”记录 | 前两类44条完整同文均已存在。139已由运行适配层启用，旧表的禁用状态不能直接引用。138、293仍受续写完成核验限制；46条核心中9条在后端TXT库找到同文、37条未找到完整同文，且这46条未作为该恢复目录的source_id注册；入口与解析适配要继续核对 | 部分协议和扩展提示词待接；P0/P1 |

D01中的40/38是两个场景的工具定义条目，含同名及交叉能力，不能相加成78个独立缺失功能。D05/D06的未找到范围限于漫剧创作前端、screenplay包及相关HTTP模块，不宣称全系统不存在可复用的工具。

D10的“37条”是对后端`prompt`模块全部TXT正文进行去空白同文比较的结果，不覆盖运行时私人提示词/数据库自定义记录，也不否认已有改写版提示词。46条核心里还混有仿写、小说、脑洞等扩展用途，应先筛适用范围，不能全算成主流程必需功能，更不能把37条换算成37个未做功能。

## 不能再按旧报告算成“完全没做”的项目

| 能力 | 当前可追到的实现 |
|---|---|
| 三步工作台、原文章节/分集、编辑保存、版本 | `ComicScreenplayStoryStage.jsx`、`ComicScreenplayAuthoringPanels.jsx`、`authoring_service.go` |
| 资产后台检查/进度/重开 | `asset_check_runtime.go`、`asset_check_repository.go`及相应前端检查面板 |
| 多视图独立生成与选用 | `ComicScreenplayAssetStage.jsx`、`ComicScreenplayAssetDialog.jsx`与多视图选择逻辑 |
| 站位参考、画布文字标注与PNG保存 | `ComicScreenplayPositioningStage.jsx`、`ComicPositioningImageEditor.jsx` |
| 逐场并发、失败场景、断点结果保存 | `storyboard_scene_concurrency.go`、`storyboard_generation*.go`、`ComicScreenplaySceneProgress.jsx` |
| 规划、进度、已有稿续写 | `storyboard_planning.go`、`storyboard_plan_progress.go`、`storyboard_plan_existing.go`。不据此宣称所有138/293及助手规划路径都等价 |
| 音色、TTS/ASR、字幕、独立媒体下载、SRT | `voice_design.go`、`screenplay_media_workbench.go`、`ComicScreenplayMediaPanel.jsx` |
| 超长视频、基本片段重拍、成片 | `screenplay_long_video.go`、`screenplay_reshoot.go`及AI-CUT关联代码；仍须按上表分支补验 |

## 提示词专项数量

| 类别 | ZIP条目 | 后端恢复目录登记 | 后端TXT找到完整同文 |
|---|---:|---:|---:|
| 剧本大师 | 3 | 3 | 3 |
| 剧本分镜 | 41 | 41 | 41 |
| 剧本核心 | 46 | 0（指同source_id登记） | 9 |

139运行时启用见`backend/internal/prompt/screenplay_protocol_overrides.go:15`；仅看冻结的catalog JSON会得到错误结论。164是此前另一原件的专项适配，已有`screenplay_zip.go`与测试，不再照旧报告称缺失。逐条元数据和同文位置在归档`prompt-coverage.json`，未复制提示词正文进本报告。

## 关键源码证据

- Agent仅一个实时只读工具：`backend/internal/httpapi/screenplay_authoring_routes.go:17`、约440行的工具白名单；约460行限制每次方案动作数量。修改方案的四类目标见`backend/internal/screenplay/authoring_service.go`。
- 助手面板及会话能力：`frontend/src/user/pages/ComicScreenplayAuthoringPanels.jsx`中的`ComicAssistantPanel`；当前接口数据以作品的Messages/Proposals为主。
- 固定生产步骤：`backend/internal/screenplay/state.go:23`和`batch_executor.go:172`以后。
- 检查语义差异：`backend/internal/screenplay/storyboard_check.go:11–23`。
- 格子拆图已有、剪映仍有边界：`backend/internal/httpapi/screenplay_media_workbench.go:172`、`case "split_grid"`及约998行交付说明。
- 重拍仅首尾帧：`backend/internal/httpapi/screenplay_reshoot.go:443`；这不否认整条重拍服务存在。

## 先后顺序建议

1. **先处理会误导生产结果的地方**：D04原文覆盖检查、D10的138/293续写/完成规则，并核对每种提示词真实最终请求及原文结转。
2. **补创作中枢**：D01助手工具逐项绑定现成服务，D02文件/引用/会话/Skills，D03编排。复用已有服务，保留逐项确认、失败可见和成员权限，不能把所有付费动作默认无确认全开。
3. **补制作工具**：D05/D06和D08。原版特殊渠道以已配置自有模型的实际能力映射，不写死洛水地址和模型列表。
4. **完成交付与视觉验收**：D07/D09，以及同一作品、同一输入在原版和本系统的对照；逐项覆盖关闭重开、取消/迟到、失败/重试、选中版本和最终导出。

## 本次检查与尚未验证

- `go test ./internal/screenplay -count=1`通过（2.742s）。
- 分阶段、工作流与媒体相关前端纯测试17项通过，0跳过。
- 核查的是资料合同、当前实现和现有测试；没有使用ZIP里的预览mock冒充真实生产，也没有执行包内程序或付费生成。
- 完整供应商剧本→资产→分镜→视频→字幕→剪映/成片的逐项原版对照，以及全部弹窗/状态/像素，仍需单独验收。
- 旧台账13/23及0/36是其已登记状态，不是本报告重做了36组全量验收后的通过率。
