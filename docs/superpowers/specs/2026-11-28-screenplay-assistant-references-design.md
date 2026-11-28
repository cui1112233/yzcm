# 作品助手显式文字引用

2026-11-28，承接已批准总计划D02与assistant-reference-next-brief-20261128.md；本轮已授权实施，不再新增确认关卡。基线HEAD e76e3efc32113db9f135ee46db95ec6e0bb0c171。

## 范围与选择

交付当前作品的project_source、document、chapter_source、chapter_script、storyboard、asset_text六种文字引用。选择、预览、模型发送共用服务端解析器。只接受kind/id、预期revision/digest及明确shot_ids；不接受owner、正文、URL、path。正文只接受当前活动版本；分镜绑定真实board ID与版本，镜头范围必须都属于该board。资产仅白名单文字，不携带Extra或图像。

比较三种做法：只拼显示名不能证明模型读到内容；客户端拼全文无法可靠校验权限版本；本轮选择服务器按ID读取并保存快照。初版不做通用文件目录或独立附件表，随现有消息JSON保存omitempty references字段，旧记录无需迁移。

## 预算与持久化

最多8项，每项规范JSON不超过16KiB，引用数组规范JSON合计不超过32KiB；system和UserPrompt全文JSON转义后真实UTF-8字节合计不超过96KiB。问题最多32KiB。超限在创建付费发送尝试和调用供应商之前拒绝，无静默截断。显式引用优先预留预算，其余既有自动摘要继续可截断。保存真实发送文字、版本、内容hash与展示名；历史快照不自动在下一轮展开。

每项必须匹配服务器目录发出的预期digest，且有revision的种类也校验revision。章节无独立版本，以指定文字字段hash作为证据。发送前重读，独立会话Begin后再重读，任何变化拒绝并释放未发送占位。返回后与保存同事务再次核对全部依赖；来源改变首版拒绝旧回复，保留原费用核账，不自动补签方案、不重发。引用不扩展写权限或增加模型轮次。

## 权限与兼容

目录/预览先查当前作品read，再查选中会话可见性及所有父会话，归档禁止新增引用。default仍是虚拟入口，使用旧作者行。旧不带references请求保持原路径与行为。带references的default保存使用项目、作者与目标行锁，并在同一事务校验实时generate权限和引用证据；独立会话复用原请求token、最终sameTx原登录/成员/权限守卫，再组合引用校验，专用回执通道开关保持OFF。不得回退普通生成器。

引用内容是数据，后端prompt说明它不能改写系统权限。URL等文字即使在作品内容中出现也不自动下载，不执行附件内指令。其他会话消息及全局个人素材不可引用。

## 界面

“作品内容 / @资产”打开真实服务器目录；勾选、预览、移除只影响当前作品+会话草稿。每次发送捕获selector副本；切换会话、旧回包不能清空新草稿。历史只显示已发送快照，用户可展开查看；不暗示读了图片。新会话发送关闭仍允许目录/预览，保留禁发提示。

## 实施顺序与验收

1. 先写失败用例：六种真实读取、跨作品/失效、白名单、范围、8项/16/32KiB及JSON转义预算。
2. 实现共用解析器和消息快照；增加同事务引用依赖核对与default受保护保存。
3. 连接目录、预览、新旧聊天，捕获实际TextRequest而不绕过新会话专用渠道；验证超限无模型调用、未发送清理与历史不重展。
4. 独立前端API及选择器，按会话隔离草稿，浏览器测试覆盖真实交互和迟到回包。
5. 跑定向Go/前端测试及构建检查；记录独立review包、owned SHA256与冻结25文件核验。无commit、无数据库、无4001、无远端、无付费模型动作。

冻结表中所有25文件完全不动。所有其他dirty文件先检查，只有owned清单中的分配文件可改。本次外部文件上传、图片、多模态、TOS、OCR、Skills、Auto和自动子代理均待办。

## 最终请求预算补充

实际模型配置解析后的model、messages、temperature、max_tokens、response_format完整JSON序列化也受96KiB约束。内部RequestByteLimit仅服务器设置，HTTP解码不接受。default解析一次并捕获同一个provider用于发送；独立会话在解析真实provider后、ReserveSessionAttempt之前拒绝超限，后续发送沿原provider和原claim资格复查，不增加重试。分镜范围可提交shot_ids，第一次预览可使用目录的完整分镜digest，返回范围digest供实际发送复用。

## 独立审查P2修正：default后续回合保留引用历史

有引用快照的default历史，原始Messages完整持久保存；CompactAssistantMessages只生成本轮模型视图，不再写回覆盖原始记录。后续请求即使没有references也适用。模型视图仍折叠历史，并清除历史References全文，快照不注入系统摘要。历史从未有引用的旧default保持原持久折叠合同。无需附件表或迁移。

## 活动正文的真实读取合同

ListDocuments可以只返回版本元数据，不能据列表中的空Content签出引用。引用View只对active版本按已认证owner/project及真实document ID调用GetDocument取全文，生成Text/Bytes/digest；inactive不额外读取，全文读取失败直接拒绝，不回退元数据空正文。最终事务仍按同一真实正文与严格摘要核对。
