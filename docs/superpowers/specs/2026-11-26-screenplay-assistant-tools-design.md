# 作品助手工具接入

## 2026-11-27 D01已有创作镜头文字方案（实施边界）

本层接modify_storyboard的update及modify_image_prompt、modify_video_prompt的直接replace/append。原件§6.2/§7.2已核对：modify_script_text指画面描述，当前创作StoryboardShot没有独立画面描述及其消费端，因此明确拒绝，保留待办；不能把它写入Line或VideoDesc冒充已接入。原版数值shot_id和shot_index不猜测索引基准，使用本系统真实字符串shot_id或唯一shot_number。只处理已保存创作board中的镜头文字；不增删镜头、不AI rewrite/fix_violations、不生成媒体。按当前认证作品的episode_no/engine选board，再按真实shot_id或唯一shot_number定位；多board必须明确选择，编号和序号冲突、未知或跨范围目标均拒绝。参数使用精确字段名，拒绝重复键、空值、身份、作品、媒体和准备标记。

独立creative_shot动作保存服务器读取的完整原镜头、目标board ID/版本、修改字段前后文本；修改未指定字段、协议、来源、绑定和媒体均保留。准备不写board。前端显示集数、引擎、真实镜头ID/序号与字段前后对照，沿用原确认。模型直接返回creative_shot或旧shot动作均不能绕过服务器准备；旧shot仅对应legacy文档，服务层历史兼容保留，模型入口明确拒绝不完整旧方案。准备成功回复由服务器改成待确认，不采用模型的完成声称。

确认核对作品、作者记录版本、pending状态、board ID/版本和原镜头完整内容。生产只接受同一MySQL数据库仓库，在一个事务中锁定作品、作者状态、board与目标镜头，复用现有镜头校验与协议序列化，仅UPDATE该镜头，递增board版本并保存applied回执。任一步失败全部回退；禁止分开写再记状态、禁止删除重插镜头或清空媒体版本；重复确认拒绝且不重复写。内存仓库也需专用原子提交能力，不允许通用Store降级分开保存。

落实总计划D01，按原资料40项创作工具与38项编辑工具分别对照，不把同名不同参数混成一项，也不把静态名称登记当作已实现。

## 第一部分：真实只读数据

保留storyboard_failures，新增get_current_screenplay、analyze_screenplay、get_editor_status、get_shot_numbers、get_shot_detail、read_assets、read_shot_video_prompts、read_shot_image_prompts、get_all_shots_voicetext。它们只能读取当前认证成员有权访问的作品；模型不能提供owner、项目路径或任意URL。

参数使用本系统真实镜头ID、episode_no与engine，名称/类型/镜头序号可以过滤；多个剧集或引擎有歧义时要求明确选择，不能悄悄挑第一个。忽略或伪造筛选条件都不能返回另一成员的数据。拒绝未知参数、未知工具与超限输入，返回体限制数量/字节，明确是否截断，不把部分数据当作完整。

原文与剧本对照工具提供原文/当前剧本供分析，不声称用字符匹配已证明情节完整。只读调用不执行生成、不改分镜或媒体；工具结果回送一次现有助手回答，记录实际读取的工具与结果。第二次回答不允许再次申请工具形成无界模型循环。

提示词合同集中在后端prompt模块，修正原系统词禁止一切工具而附加词允许工具的矛盾。前端继续使用已有消息工具记录展示，不另写系统提示词。

提供同一鉴权范围内的POST assistant/read-tool只读检查接口，使用read权限，不依赖文本模型、不保存对话。它与助手共用分发和参数校验，便于无模型费用地核验实际数据；不能接受写入或媒体工具。

## 后续部分

下一小步先接modify_character、modify_scene、modify_prop的现有资产update：服务器按真实ID或唯一精确名称读取原记录，保留未请求的字段和媒体信息，生成含当前资产版本的待确认动作。准备方案时不写资产；用户通过现有确认按钮才应用。名称/ID冲突、类型不符、跨作品、未知字段、新增/删除请求均明确拒绝，不猜目标。模型不能自行提供owner、project_id或确认状态，回复不得把准备方案说成已修改。

同一方案准备逻辑提供assistant/prepare-asset-edit接口，需要edit权限和当前expected_revision，不依赖模型。它只保存pending方案，不能携带confirmed；实际改动仍须通过原assistant/apply确认接口。重复旧版本提交、确认前被其他入口更新的资产均拒绝，不能覆盖较新的改动。

生产MySQL确认时，资产内容和方案applied状态必须在同一事务内提交，任一步失败全部回退。必须使用同一数据库的作者仓库；不允许因配置了不同仓库而悄悄退回分开保存。事务提交结果不确定时，刷新只能看到“两者已保存”或“两者未保存”，不能留下资产已改但方案仍pending。复用已有资产写入校验，不复制一份修改规则。

内容写入、编号修改、资产绑定与增删须保存待确认动作，确认时核验作品/版本/权限，再调用现有服务；媒体生成、审核、解析、导出须独立记录请求标识、实际步骤、失败/未知结果与恢复。不能因为第一部分可读就标记所有40/38项完成。其他工作包和37条未同文核心提示词登记继续按总计划推进。

## 第一部分验收

先红绿测试真实结果与保存前后内容一致；跨用户/跨作品拒绝、所选引擎和剧集隔离、无数据、歧义、未知工具/参数、超限和被截断状态。真实HTTP受控助手两轮读取结果并落消息工具记录；无写库副作用；原storyboard_failures与138/293回归不退。

## 2026-11-27 D01资产新增/删除层

本层把上述第一步的update扩展为add/update/delete。三个modify工具省略action仍为update；支持add_character/add_scene/add_prop和delete_character/delete_scene/delete_prop别名，别名与action冲突立即拒绝。工具参数继续严格拒绝身份、作品、权限、路径、媒体字段和服务器准备标记；模型直接返回asset动作（包括伪造asset_prepared）仍被HTTP入口拒绝。

新增按类型和完整名称排重；服务器准备时分配唯一资产编号和完整待确认快照，准备不写资产。确认再次检查同名记录；同一方案只应用一次，不在重试时另生成编号。删除先读真实记录和版本，检查本作品所有保存的剧本版本、全部剧集和引擎的分镜引用；确认阶段再次检查真实版本和引用。人物/场景/道具引用按对应结构化字段完整名称解析，与实际参考图匹配的逗号、顿号、分号、斜杠、竖线、换行、@规则一致；姓名内部空格保留。扩展协议中的显式资产ID引用也检查。引用存在时指出资产与镜头并拒绝，不自动解绑；只删除资产记录，原素材文件和媒体记录保留。

MySQL通过同一数据库、同一Serializable事务锁定作品、待确认状态和目标资产；删除扫描并FOR UPDATE锁住本作品两张镜头表的完整查询范围，空范围也受保护，防止本事务检查后、提交前出现新引用。新增复用现有CreateAsset的校验与INSERT事务函数，资产写入和方案applied状态一起提交，确认状态写失败则资产新增/删除回滚。更新保留原完整字段与版本保护。

并发边界：当前镜头表缺少project_id/owner_username前导索引，删除范围查询可能扩大锁定范围，暂时阻塞其他作品镜头写入；未新增迁移。其他创作入口原本允许保存尚未提取成资产的名称，删除事务之后的新创作仍遵循此合同，不把名称强制变成外键。内存仓库用于受控测试，生产原子保证由MySQL实现。下游失效标记仍复用原逻辑，在资产与确认事务提交后执行；该标记失败不回退已提交的资产及applied回执。

前端待确认列表明确显示新增资产/修改资产/删除资产和真实名称，删除显示“保留原素材文件”；沿用原确认按钮和版本字段，applied/stale方案不可再次执行。后端prompt模块同步工具合同，前端没有新增系统提示词。

### 本层测试证据

- 先红：新增CRUD测试中三个add_*别名均以“不支持的修改工具”失败；再实现并转绿。
- 继续红绿：嵌套协议绑定漏检、姓名空格/@引用漏检分别先失败再修正；CreateAsset事务拆分后原校验顺序用失败测试捕获并恢复。
- 干净测试源码：`_local-archive-20261028/luoshui-parity-20261126/runtime-source`（HEAD db2ee863）；独立Go overlay：`review-overlay/assistant-asset-crud.json`，仅覆盖本层及已有助手必需文件，不改变干净目录HEAD。
- `go test -overlay .../assistant-asset-crud.json ./internal/screenplay -count=1`通过；定向`./internal/screenplay ./internal/httpapi -run 'Test(Assistant|MySQL.*Asset|Authoring.*Asset)' -count=1`通过。
- 覆盖：三类资产准备/确认新增删除、未确认不写、重复确认、跨用户/跨作品、只读成员、同名新增冲突、服务器ID、伪造参数、删除前/确认前新引用、历史版本引用、资产版本改变、MySQL新增/删除确认写失败回滚和禁止分开保存、SQL NULL资产列表。
- 真实headless Chrome组件测试`node --test frontend/src/user/pages/comic-assistant-asset-crud.browser.test.js`通过（1/1，无跳过）：核对新增/删除文字和资产名、保留文件说明、确认弹窗前零写入、确认后单次正确POST、applied按钮禁用。`comic-assistant-intelligence.source.test.js`5项回归通过。
- 本层未调用付费模型、未远端推送、未部署、未重启4001。真实4001新增/删除保存回读及独立审查由主负责人接续，不能将本层自动化测试当作整体D01完成。

## 2026-11-27 D01读取筛选和中文工具名补齐

四项镜头列表读取（get_shot_numbers、read_shot_video_prompts、read_shot_image_prompts、get_all_shots_voicetext）增加shot_ids：必须为1至200项字符串数组，每个ID为1至256字节，无首尾空白，仍受整个参数8192字节上限约束。空数组/null、数字/null元素、空白和超长ID均拒绝；重复ID去重，结果仍按真实镜头序号排列。shot_ids不得与shot_numbers或shot_id同时出现（即使后者为空也拒绝），避免悄悄扩大或改变范围。get_shot_detail保留单shot_id合同，资产/作品概览等其他工具明确拒绝shot_ids，不默默忽略。

先按认证成员可访问的当前作品和episode_no/engine选择真实分镜，多集/多引擎仍要求明确选择。随后逐一核对所有指定ID；任一ID不存在、属于其他作品/成员或不在所选剧集/引擎中，整次返回错误，不返回部分命中的结果，不扫描其他作品来猜测ID。提示词合同同步放在后端prompt模块。

助手消息展示九项读取工具各自中文名称，保留分镜失败核对原名称和未知工具“只读工具”回退；工具摘要和失败颜色保持现有逻辑。中文名映射只查自身键，未知名称如toString也走回退。

### 本层测试证据与限制

- 先红：干净源码加测试后get_shot_numbers的shot_ids报“工具参数无效”；真实Chrome组件原界面显示“只读工具”，与“读取当前剧本”断言不符。随后分别实现并转绿。
- 独立干净源码为`_local-archive-20261028/luoshui-parity-20261126/runtime-source`，HEAD `e8a12dd110a248b31cb42714b7c8c2b17b50d6c4`；独立overlay `review-overlay/assistant-read-ids.json`仅覆盖本层读取实现、读取测试、HTTP读取测试和后端prompt，不借用其他会话脏文件。
- `go test -overlay ../../review-overlay/assistant-read-ids.json ./internal/screenplay -count=1`通过。业务用例覆盖四项真实ID读取、200项边界、未选镜头不扩入、不存在/跨作品/跨剧集/跨引擎拒绝、参数类型/长度/混用拒绝和单ID详情合同保留。
- 定向`./internal/screenplay ./internal/httpapi -run 'Test(AssistantRead|Authoring.*ReadTool|AuthoringAssistantRead)' -count=1`通过；签名HTTP读取验证认证alice仅取得选中的实际提示词，bob或不存在ID拒绝，读取不保存会话，也不调用模型。
- `node --test frontend/src/user/pages/comic-assistant-read-labels.browser.test.js frontend/src/user/pages/comic-assistant-intelligence.source.test.js frontend/src/user/pages/comic-assistant-asset-crud.browser.test.js`为7/7通过、无跳过。真实headless Chrome加载实际React+Antd组件，核对九项中文名、原失败核对名、未知及继承键回退与真实摘要；新增/删除确认组件也回归通过。
- 未调用付费模型、未远端推送、未部署、未重启4001。本层只证明源码自动化合同，不代表4001已经运行新版本，也不代表D01其他工具或整条创作链完成。

### 读取字段解析复审修正

复审发现Go标准JSON结构体解码会接受大小写别名，而原筛选存在性检查按精确字段名取值：SHOT_IDS可能绕过筛选并返回全部镜头；重复shot_numbers后置空数组也可能扩大范围。已先用失败测试复现，再改为逐字段读取顶层JSON：只接受规定的精确小写字段名，解码后同名字段第二次出现即拒绝（包括用Unicode转义拼出同名字段），最后再执行原类型、范围和混用校验。拒绝请求不返回任何镜头结果；后端提示词也明确小写与不重复合同。

独立e8a12dd1源码及同一assistant-read-ids overlay验证：新增大小写、SHOT_ID/SHOT_NUMBERS混用、shot_ids/shot_numbers/shot_id/episode_no/engine重复键、转义重复键和其他读取工具大小写别名用例先红再绿；screenplay全包通过，签名HTTP补充用例要求400且无提示词数据。未重启4001、未部署、未调用模型。


### D01创作镜头文字层测试与交接

- 原件依据：归档`luoshui-dev-audit-20261126/20-backend.md` §6.2/§7.2。已接三项工具的直接文字分支，不代表全部40/38项接通。modify_script_text没有本系统创作board画面描述消费端，明确拒绝并保留待办；不新增虚设字段。原版数值shot_id/shot_index不自动转换，改用真实字符串ID或唯一shot_number。
- 无模型准备接口：POST `assistant/prepare-shot-edit`，权限edit，输入expected_revision、tool、tool_args；只追加pending方案。确认仍为原`assistant/apply`。支持modify_storyboard action=update、new_data中video_desc/image_prompt/line/voice_actor/emotion；modify_image_prompt/modify_video_prompt action=replace/append（兼容mode，但冲突拒绝），new_prompt为文字。直接追加不自动加分隔符。单个文字字段16384字节、参数32768字节上限。
- 先红：业务测试原先缺PrepareAssistantShotEdit编译失败；HTTP真实受控工具请求报“不支持的只读工具”；真实Chrome组件缺“第2集”和前后文字。各自实现后转绿。补充红绿：JSON RawMessage在方案保存时自动规范空白，原始字节比较会误报冲突，改成JSON序列化后的完整镜头比较。
- 干净测试基线`_local-archive-20261028/luoshui-parity-20261126/runtime-source` HEAD d868a3c1，独立overlay `review-overlay/assistant-shot-edit.json`。不移动快照HEAD、不借其他脏文件、不重启4001。
- `go test -overlay ../../review-overlay/assistant-shot-edit.json ./internal/screenplay -count=1`通过；`./internal/screenplay ./internal/httpapi -run 'Test(Assistant|Authoring.*(Shot|Asset|Read)|MySQLAuthoring)' -count=1`通过。
- 覆盖：准备零写入、完整原字段/来源/协议/绑定保留；真实镜头ID与唯一序号、剧集/引擎歧义、跨作品/跨用户/只读成员拒绝；ID/序号冲突、大小写/重复/未知字段/null、增删/AI重写/违规修正/媒体字段拒绝；原作者版本、board版本、同版本镜头内容变化；重复确认；完整合法creative_shot快照伪造和旧raw shot动作均不能从模型绕过准备。
- MySQL通过sqlmock验证同一个事务中目标镜头UPDATE、board版本UPDATE、作者回执UPDATE的任一步失败均Rollback；提交结果不确定返回失败、不声称成功；跨数据库仓库拒绝且不降级分开写；完整未知protocol_json根字段也保留。未运行真实数据库故障注入。内存Store也在两个仓库锁内一次提交，不走先写镜头再Save作者状态。
- 真实Chrome加载实际React+Antd组件：镜头前后对照与单次确认、原资产新增删除、九项读取中文名、智能展示回归合计8/8通过，无跳过。前端无系统提示词，后端prompt明确支持与不支持范围。
- 未调用付费模型、未推送、未部署、未重启4001；实际4001保存回读及独立审查交主负责人接续。仅此层源码合同完成，不能标记D01整体完成。
