# 助手逐镜头范围引用设计

2026-11-28。承接已批准D02总计划与assistant-shot-reference-brief-20261128.md；9bce3fbf文字引用已由父任务真实4001验收。本轮起始最新v88 HEAD为011d1527ae3f07b14b6b4766950510e9e020ecf0（68676b00后为其他对话profile文档提交，保留）。不新建分支、不提交、不调用DB/4001/远端/真实模型。

## 方案与范围

沿当前服务器ID引用合同补“选择镜头”，不让用户手填seq冒充ID。拒绝先Resolve整份再客户端切片：大于16KiB的整份必须仍可列目录。采用按作品/会话授权的有界镜头页，whole-board Describe只核整份版本和digest，所选真实shot_ids继续通过现有resolve预览为range digest，最终事务完全保持。

## 目录合同

POST /api/comic-screenplays/projects/{id}/assistant/references/storyboard-shots。

请求只接session_id、board_id、expected_revision、expected_digest、offset、limit；expected证据必须是整份board，不能传局部range digest。offset默认0、非负且不超过总数；limit省略或0默认100，否则1..100。未知字段/正文/URL/owner/path拒绝。先复用作品read与checkReferenceSession，再从已认证owner/project读board，Describe全量核证据，不调用Resolve或模型。

返回board={id,name,revision,digest}与shots=[{id,seq,position,excerpt,excerpt_truncated}]、offset、next_offset、total。position为board数组中的0基位置；seq只展示，选中及传输只用真实ID。摘录由镜头描述/台词组合，UTF-8完整字符截断且≤256字节，明确truncated；不出图片/Extra/ProtocolFields。分页响应最多100项。版本/digest变化409；跨作品/不存在board404，非成员403。页码不自动附新版本。

本轮用已有scoped ListStoryboards读取，按board原顺序截取响应页，不改共享Store/MySQL查询。这是响应分页，不声称实现新的数据库游标；整份digest计算必然读取当前board文字。分页/摘录不是可直接发送正文，正式resolve仍执行8项/16/32KiB及最终96KiB规则。

## 交互与两种证据

父弹窗每个分镜行有“选择镜头”，打开子弹窗勾选并跨页保留ID。整份与局部在父草稿里按kind+board ID互斥，一份board只占一项引用。子弹窗空选不可应用，只能取消；不得把空shot_ids序列化成整份。按position排序后回到父弹窗，先正式预览才允许“使用这些引用”。chip显示集/引擎与已选镜头范围。

full evidence与range digest分开：父目录行或首次选择保留的board_evidence只用于目录分页；resolve返回的snapshot.digest只用于该精确范围发送。预览后父草稿保留client-only board_evidence/shot_rows/range_label，API发送严格投影既有selector，不传这些界面辅助字段。

重开修改已有范围使用原full evidence，不能拿range摘要当full token，也不能用新目录证据悄悄给旧IDs补签。若缺原full evidence，必须显式清空后重新勾选；若版本失效，显示409，可显式返回父级重新读目录并清空该board选择，再由用户重新选择/预览。不得自动保留旧范围并套新证据。

打开范围编辑、修改勾选、取消、关闭父/子弹窗及切作品/会话，都使旧预览无效；各请求有操作序号，迟到成功/error/finally均不能覆盖新状态或清busy。父弹窗编辑期间旧preview请求也不能恢复“使用”资格。会话key变化时即使组件未重挂也重置操作代数。

## 边界与验证

新增业务assistant_reference_shots.go/test，新增HTTP screenplay_assistant_reference_shots.go/test；仅在registerAssistantReferenceRoutes注册。前端只改独立references API/Picker并新增ComicAssistantShotReferencePicker.jsx及新Chrome回归。原prompt、生产、计费、迁移、TOS、最终Tx及其他dirty文件不改。

先红后绿：大board整份>16KiB仍可分页，局部可resolve；分页/ID/全量证据/UTF-8摘录白名单与来源失效；父子真实Chrome选择→跨页→返回→预览→应用→重开修改/取消；整份互斥、空范围、200ID限制、旧预览与跨会话迟到隔离。旧六类/整份与原会话回归继续通过。最终交owned/hash/review，由父统一提交构建与4001验收。
