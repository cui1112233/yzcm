# 作品助手 UTF-8 TXT/MD 附件设计

2026-11-28；本文件仅供父任务预检，尚未实现。承接已批准D02总计划、9bce3fbf文字引用与6527逐镜头范围引用。父报告6527已过真实4001接口/Edge/归档/attempts=0，运行仍为6527；本轮只读HEAD为9672270e3a60ba3fc56e7b6e8abf7f1f8ccc2206，保留其他对话提交，不回退。没有读取真实密钥、连接DB/4001/TOS、调用模型或修改产品。

## 交付范围

第一片只做UTF-8 TXT/MD：上传一个真实文件、私有TOS原件、MySQL中不可变解析文字、会话范围内预览/选择、通过既有有界文字请求发送、最终与消息同事务发布并记录每次引用。原件≤64KiB，单项快照JSON≤16KiB，总引用8项/32KiB、实际provider完整JSON≤96KiB；不提高任何现有预算。

只读会话模式及模型发送通道关闭都不阻止合法资料准备；发送仍需要原generate与实际通道资格。权限口径明确如下：项目read允许读取可见的已发布附件及本人draft预览；新上传/重试准备要求read且edit或generate之一，会话/父会话/作品仍须活跃；纯read成员不新增存储对象，但可选择/预览已可读资料。readonly会话模式仅禁止作品修改，不等于成员grant.read-only。若此准备权限取舍需要调整，在父预检时决定，不能实现中自行扩大。

不接DOCX、PDF、图片、多模态、OCR、Skills、Auto、文件系统扫描或任意URL抓取。原件下载和解析文字都通过受保护服务；不显示没有支撑的控件。上传/解析/预览不调用模型；TOS本身有存储/请求成本，受控对象通过不冒充真实TOS通过。

## TOS来源调查与当前阻塞层

本节只检查代码，不读取任何配置文件值、环境值或密钥。

- config/config.go:148-157从QIANTIE_NOVEL_FETCH_TOS_*和QIANTIE_AGENT_ASSET_TOS_*读取endpoint/region/bucket/凭据；:183-212逐字段回退Merge→NovelFetch→AgentAsset。它是启动配置，未发现从后台UserConfig热读TOS并覆盖cfg的resolver。
- app/app.go:329按AgentAsset TOS字段构造素材TOS；缺省可能退回本地agent-assets对象，不代表文档TOS可用。:428只有cfg.AgentAssetTOSBucket非空时创建novelcreation.NewTOSDocumentStore并ConfigureDocuments。
- novelcreation/materials.go:103的uploadAvailable只依据文档对象store是否注入。父已经核实当前4001没有cfg读取的TOS env，GET小说documents/availability为false；这与构造链一致，本轮没有重新请求接口。
- SettingsPage.jsx:272、403的“存储设置”对应storageRoot及productionRetentionDays；httpapi/storage_legacy.go:22从UserConfig读取本地目录，:166注册本地list/restore/save-media，不是TOS云配置。UserConfig.preferenceOnlyPatch在user_config.go:323也只列这些偏好。
- screenplay_subject_upload.go:48-60从API集成配置取volcanoAccessKeyId等，用于真人素材上传服务/图床；并没有TOS endpoint/region/bucket完整合同，不能借用其凭据拼造本片对象store。
- 新片可在明确注入时复用既有服务器AgentAsset TOS配置组与TOSDocumentStore低层读写类型，但必须明确为TOS实例，不能把可能为local/migrating的AgentAssetObjects直接当TOS可用。未查到后台另一个已接入该文档store的合法TOS设置服务，不声称“UI配置了所以App已注入”。

父补充核对：本地.env/.env.local不存在，.env.v88-review的V88_MERGE_TOS组为空，当前进程cfg TOS环境为空；这是父提供的配置状态，本轮没有读取文件值或进程环境。父已单独询问现有私有配置位置，不要求用户在聊天发密钥。

若现有组不能供私有附件使用，允许最小新增QIANTIE_ASSISTANT_ATTACHMENT_TOS_{ENDPOINT,REGION,BUCKET,ACCESS_KEY_ID,SECRET_ACCESS_KEY}专用启动字段及app单独注入。专用字段只给附件private adapter，不写回Merge/NovelFetch/AgentAsset组，不激活全局媒体worker、扫描或迁移。缺任何必要字段则准确不可用；不从UI或真人上传配置拼装。

因此先完成合同和未配置禁上传的实现验证，不问用户key，不更换存储配置。真实TOS完成条件仍是后续有授权配置后原件Put/Open/hash及私有ACL/受限policy核验回执；不能为验收改用本地文件并标TOS成功。

## 原件私有性的有限合同

随机Key、不返回URL、以及用户界面隐藏对象，都不能证明私有。现novelcreation/document_tos.go:30的Put只有Bucket/Key/ContentType，不可直接拿来承诺本片原件private。

已查本机github.com/volcengine/ve-tos-golang-sdk/v2@v2.9.8源码：tos/type.go:484的PutObjectBasicInput在:496有ACL字段，编码X-Tos-Acl；tos/enum/enum.go:6定义ACLPrivate。GetObjectACLInput/Output和GetBucketACLInput/Output在type.go:90-115，ClientV2方法在acl.go:131、165；GetBucketPolicyV2在policy.go:141，NoSuchBucketPolicy在codes/codes.go:71。这里只读SDK代码，没有调用TOS。

采用小型附件专用private适配，不改变其他模块的Put/ACL：新novelcreation/document_tos_private.go可复用现有配置/对象类型，提供明确的PutPrivate与受限隐私核验；app附件适配只能调用此能力。可使用附件专用SDK client将自动重试设为0并设置短超时，不修改其他业务client策略。

首片只支持可明确判定的保守合同，不做通用bucket policy授权引擎：bucket ACL必须仅owner（CanonicalUser ID逐项精确匹配响应Owner ID），没有AllUsers/AuthenticatedUsers或其他主体grant；bucket policy明确不存在（精确NoSuchBucketPolicy，不把一般404/403当不存在），或已解析为没有任何Statement的空policy。任何非空策略即使看起来只给可信用户，此片也标为未支持/未能确认，不能猜安全。读取无权、超时、公开bucket/策略、继承状态未知均准确不可用；不自动修改bucket ACL/policy，不把现有共享媒体桶改私有。

每次实际写入前在事务外核验namespace隐私，再以ACL=enum.ACLPrivate写原件；Open核hash及GetObjectACL证实owner-only显式私有（匹配Owner ID，三个documented optional flags允许省略；显式null/错类型/true及未知字段仍拒绝，不得授予其他主体）后才能ready，准备阶段不发公开链接。浏览器始终只通过同源鉴权代理下载。调用时检查不能阻止存储管理员日后改桶策略，报告/界面不承诺永久绝对保证；上传/下载操作会再次检查当前私有合同，无法确认就停止原件操作。

availability分别返回configured、privacy_state(unknown/private/unavailable)和upload_available；未知不启用上传。可增加受权POST attachments/storage/check仅做namespace ACL/policy读取，给准备者明确“检查附件存储”入口，不调用模型、不更改桶；当前未配置时此入口也准确不可用。检查结果不是永久授权，上传前仍重核。所有ACL/policy/Put/Open/Get网络均在SQL事务外，每次上传/重试/原件下载的总deadline30秒（隐私单次检查最多10秒并受同一总deadline），无外层自动重试；SDK专用重试0，显式用户重试沿同一意图。取消/超时不能通过新一层一分钟重试拖延或复活旧ready。

## server-only ReferenceScope

现LoadAssistantReferenceView只有owner/project，CheckAssistantReferencesInTx只有owner/project/refs，均不得直接读取新附件。新附件不能被加入全作品无scope View后随便Describe/Resolve。

新增ReferenceScope，内部字段为projectOwnerUsername、projectID、effectiveUsername、sessionID及独立可信actualActor审计字段，全部不可JSON解码；由HTTP服务器在认证后按作品和会话解析构造，owner/project/effective/session必填非空、session显式；actualActor未知仅允许按完整业务scope只读同session published，准备/本人draft/持久发送必须有可信actualActor。effectiveUsername严格等于handle的BridgeIdentity.Username，与现会话可见性同语义；不是项目owner、模型配置owner或费用subject的替代。原始实际操作者/代操作发起人另存actualActorUsername（来自可信AccountCenterIdentity.Actor.Username），不能替代effectiveUsername做会话授权；两者不是费用subject的新别名。draft另以“有效上传者+可信实际上传者”双重匹配收窄可见性，published后以原同session授权共享。实际身份未知时不猜owner或effective为actual：不创建/读取他人draft，只能按完整有效scope读取已经同会话发布的资料；准备和发送的持久原登录guard不满足则拒绝。

default旧路径明确映射为服务器常量default再构造完整scope；不是底层见空session就猜default。新附件端点/查询必须显式传session_id。构造只绑定身份，读取与最终事务仍重新检查实际权限，不把一次构造当永久授权凭据。原登录、真实actualActor/team/subject、代操作与费用守卫全部保留。

新增scope-aware解析服务，保留旧View的六类/shot合同：

| 阶段 | scope来源与核对 | 附件规则 |
|---|---|---|
| 附件可用性/列表/详情/下载 | 从认证Bridge用户+Resolve得到owner/project，再查显式session可见性、祖先、归档 | 只查同scope；draft匹配有效上传者及可信actual上传者；published仅同session当前可读 |
| 既有引用目录 | 服务器构造scope，旧View仍只返回六类；单独追加scoped附件元数据 | 不向旧chapters/team/general AuthoringState投影私有附件列表 |
| resolve预览 | body仍只有session_id与selector，服务器构造scope | attachment_text由独立repo加载真实ready文字；客户端正文/key/URL/owner均不可信 |
| default发送前 | 路由明确default、有效Bridge用户名；read/generate与原登录检查不变 | 用scoped解析服务读本轮；旧无附件请求仍走旧路径 |
| session发送前 | 路由session+有效Bridge用户名；Begin后核Scope与req.Owner/Project/Actor/SessionID完全相同 | Begin前解析预算，Begin后重读；失效按原未发送释放，不创建付费尝试 |
| 最终sameTx | 锁住项目/作者/会话和祖先后，使用原捕获scope，先原登录/成员/权限guard | 不能从snapshot/uploader猜scope；检查row可见性、ready、内容版本/hash、原件验证记录并持锁到提交 |

缺scope的旧LoadView/Describe/Resolve/CheckInTx继续拒绝attachment_text，不提供兼容猜测。旧六类不因缺新scope而失效。需要共享预算时，将既有selector预算逻辑抽为内部“解析回调+统一限额”函数：旧View只用旧Describe，新ScopedResolver显式分派附件repo，不能复制一套宽松budget或把附件字段放进无scope View。

服务接线避免修改冻结的MySQLStore/MemoryStore结构：在新增Go文件为MySQLStore提供可选AttachmentRepositoryProvider方法（访问其包内db），测试用嵌入Store的独立适配器提供内存repo；不往Store必选接口加方法，不通过包级全局对象或HTTP请求体偷传服务。上传对象service单独由app/router注入，解析/最终保存只依赖repo，网络不进入最终事务。

## 文件字节与不可变证据

POST multipart只接session_id、request_key、file，各字段大小和文件数量严格限定。建议外层96KiB，单file最多64KiB+1检测超限，不落任意本地路径。文件名仅保留规范basename、UTF-8且≤191字节；拒绝NUL/空名/未知扩展名，服务器对象Key不使用文件名路径。

按原始字节计算raw_sha256；校验UTF-8，仅去可识别的UTF-8 BOM，不TrimSpace正文、不改前导缩进/段落/CRLF/Markdown代码块。空白判空可单独Trim检查，但存储和发送保留文字。拒绝非法编码、NUL/明显二进制；TXT/MD均是数据，HTML/链接/代码不执行、不远程展开。解析规则版本固定utf8-bom-v1，text_sha256针对实际保留文字。原字节与文字hash不同是允许且可追溯的。

构造真实ID、filename、content_version=1及附件证据后，先按最终snapshot规范JSON校验16KiB；HTML/反斜线等JSON转义也算字节。超过在TOS Put之前拒绝。ready后的resolve及最终provider仍重新核8/16/32/96KiB，不静默截断。

新snapshot kind=attachment_text；可增加omitempty附件证据块，含raw/text SHA256、parser_version、非密钥namespace ID及内容版本，正文为实际发送文字。旧六类的新证据块为nil并omitempty，不能改变既有digest字节合同。metadata列表不含全文，详情必须读真实解析文字，不能重演ListDocuments仅元数据空Content签摘要的问题。

内容不可原位替换；重传不同字节产生新key/新ID。content_version与state_revision分开：前者签不可变文字，后者CAS上传/失败/ready/发布状态。首次共享不抬高内容版本，不让同一ready附件在第二次显式引用时莫名过期。

## 幂等、存储意图与命名空间

request_key由客户端为当前文件草稿生成并保持重试稳定，但不授予读取/发布权限。服务器计算真实文件hash，不信任前端声明。最小建议unique(uploader_effective_username,uploader_actual_actor_username,request_key_sha256)，每条记录不可变绑定owner/project/session/有效及实际uploader/raw hash/规范文件名及namespace；同一有效上传者的同key若scope/hash/文件名不同，返回一般冲突，不返回旧scope内容。不同用户不能仅凭key找回彼此记录；所有读取仍按完整scope查ID。raw request_key无需明文落库。

原记录/ID/objectKey固定，重试不能悄悄新建第二份。前端切作品、会话或替换文件必须换key；相同文件可用新的key表示明确的新上传，不以全站文件hash去重导致私有文件相互可见。

先有界读字节/解码/预算 → 短MySQL事务登记preparing意图、namespace、固定Key与工作token/lease → 事务外Put → 同一store实例Open原件核大小和raw SHA256 → 新短事务复查原登录/有效用户、作品/会话活跃、工作token/CAS后ready。不能在TOS往返持项目/作者/会话锁；也不使用现有handle(edit)的长锁包住上传网络。TOS成功而DB失败仍有意图可发现，不当成不存在重传。

TOS未知结果保留preparing/failed诊断和原Key，只有显式重试沿原记录核对，不把未知当未提交。首次实现不自动批删旧素材、不自动跑队列或模型。草稿移除chip只去选择，不冒充已物理删除原件。

新增小型BoundAttachmentObjectStore适配，不改全站存储：从启动配置规范化endpoint/region/bucket及adapter类型生成非密钥namespace SHA256，记录到意图。对象Key与namespace必须同时传给Put/Open/重试；当前配置namespace不匹配则拒绝，不在新桶用旧Key回源/新写第二份。凭据只留内存；同namespace的凭据轮换可继续，绝不持久明文AccessKey/Secret。

可复用novelcreation.DocumentObjectStore/TOSDocumentStore的底层读写，通过app层小适配转换类型并固定BindingID；不让screenplay领域直接调用小说资料HTTP/业务表，也不放宽agentworkspace的image/video/audio类型。

ready文字已由原件hash验证后不可变，后续文字预览可用DB中该版本，不偷偷换桶取原件；namespace当前不可用时原件下载/未完成上传重试返回存储不可用，不另写新桶。已有ready文字是否可发送，依据同scope ready/验证记录与内容hash，不依赖在持锁期间再次联网；界面区分“已验证文字可预览”与“原件当前不可取”。

## draft可见性与每次消息关联

记录的授权uploader是有效会话用户，实际上传操作者另存。draft在共享会话里也仅这对上传主体匹配时私有可读；成员不能在全作品目录看到别人的未发送文件。read-only预览既不发布draft也不改变业务内容。首次成功消息引用才可将附件变成同会话可读，不能提前在预览或模型等待时共享。

当前AssistantMessage没有messageID，不假设已有字段。文件-bearing用户消息新增可选server reference_operation_id（由服务器一次聊天请求只随机生成一次，仅赋给该轮真实user消息，非前端providedID，与内部请求token及上传request_key不同）；旧消息省略，模型历史投影明确剥离该字段及旧References全文。新会话可私下关联原request token，不能把活动token作为公开消息ID返回。default也由服务器为该次发送生成独立operation ID，不拿作者revision/前端索引猜ID。

新增关联表，以owner/project/session/operation_id/attachment_id唯一；保存本轮内容版本、snapshot digest、有效发起人及可选独立审计发起人。一个附件可有多个operation ID，允许同一有权会话后续多条消息显式引用。首次共享标记first_published_at仅在第一次成功关联时设置；以后每次增加独立关联，不把附件当一次消费品。

最终同事务完成：原资格guard → 原六类目标检查与锁（保持顺序） → 按ID排序锁附件 → 确认scope/可见资格/ready/content_version/raw/text hash/验证记录 → 插入本轮关联、必要时首次发布 → 保存带同一server operation ID的用户消息/助手回复/方案。任一失败全部回滚，后续schema/网络清理不能使已完成业务自动重放。

所有新目标路径采用既有项目→作者→会话/祖先→原登录保护→旧目标→附件的固定顺序。上传完成、重试、附件失效同样先取得相应项目/会话保护后再附件行，不能持附件锁再反向请求旧目标。真正TOS访问绝不在该保存事务内。

default新增SaveAssistantReferencesScoped，校scope与AuthoringState目标一致；旧SaveAssistantReferences仅保留六类并拒新kind。独立会话在原FinishAssistantRequestGuarded的服务器guard中组合原commitGuard与scoped附件检查/关联发布，同一个tx写历史；不能另开tx发布。原session token/paidattempt/send-once/费用unknown与原登录撤销语义完全不变。专用新会话发送通道仍OFF、旧回执审计未完成，附件不承诺当前未知模型回包可自动恢复，也不为附件打开旧回执绑定或任何模型权限。

## HTTP合同（本轮只设计）

建议新增：

- GET projects/{id}/assistant/attachments/availability?session_id=...：返回configured/upload_available/prepare_allowed、原因及64KiB/16KiB限额，不出Key或凭据；缺TOS配置则禁上传，现有六类/shot仍可用。configured不冒充真实网络验证。
- POST同base/storage/check：受相同准备权限保护，限时只读核验namespace私有合同；未配置/无权/未知/公开时不可上传，无桶策略写入。
- GET同base?session_id=...：本人draft+本会话已发布的ready元数据，有界分页，不返回全作品文件列表。
- POST同base：multipart session_id/request_key/file，准备成功返回服务器附件ID、content_version、digest与明确状态。失败/未知能通过受限GET按本人request key摘要回读原意图，不新建对象。
- GET同base/{attachment_id}?session_id=...：有权详情/实际解析文字；GET其/content同源重新核权并在事务外有界读绑定原件、网络结束后发正文前再短核权限，正文下载强制attachment且不直接执行HTML/Markdown。
- POST同base/{attachment_id}/retry：显式session_id、expected_state_revision、原request_key；只能原scope/uploader+同namespace。先Open固定原Key验证：已有完整原件则恢复验证，不再次Put；明确对象不存在时要求multipart重附同hash原文件后才Put同Key；未知读取失败不当不存在。request_key和expected_state_revision以multipart字段提交，文件可选，仍受64KiB和外层限额；不自动跑模型。

现references/resolve可接受attachment_text selector，但必须经server scope-aware服务；body无scope对象/actor/owner。旧六类及分页镜头接口原合同不改。暂不新增通用文件编辑/移动/全局共享/目录扫描与公开URL。

## 文件分配及迁移方案

当前源码未找到8800125/8800126声明；8800124为138。候选8800125仅作为本次设计分配，实施前再次搜索，尚未读任何数据库迁移表。

两表名固定为screenplay_assistant_text_attachments与screenplay_assistant_text_attachment_messages，不复用小说文档表；新迁移先create附件再关联，状态/ID/用户名字段按既有MySQL约定。

新增：

- screenplay/assistant_reference_scope.go/test：server-only类型、缺scope与错actor/session失败合同；不混审计身份。
- screenplay/assistant_text_attachments.go/test、assistant_text_attachments_mysql.go/test：字节/文字/幂等、阶段、scope可见性、关联、多次引用；新增MySQLStore可选repo provider方法放本文件，不改冻结MySQLStore结构。
- screenplay/assistant_text_attachment_references.go及mysql/test：独立scoped解析/最终检查/发布关联，旧无scope View仍拒新kind。
- storage/screenplay_assistant_text_attachments_schema.go/test：两个新表与向前Migration导出ScreenplayAssistantTextAttachmentMigrations。app.Build在现有聚合后append此新列表，避免修改冻结storage/screenplay_schema.go注册表，也不改8800121-124。
- novelcreation/document_tos_private.go/test：新增附件专用private能力/短deadline/零自动重试构造，原document_tos.go Put与其他模块行为不改；仅接受有限可确认bucket ACL/policy合同。
- app/screenplay_attachment_objects.go/test：小型private/namespace适配，配置构造/匹配不打印凭据。
- httpapi/screenplay_assistant_text_attachments.go/test：独立上传、availability、详情、原件与重试，新的HTTP scope工厂。
- frontend/shared/api/comicScreenplayAssistantTextAttachments.js/test，user/pages/ComicAssistantTextAttachmentPicker.jsx及Chrome回归：独立文件准备/进度/错误/预览/重试/当前会话草稿。

必要的小范围已有文件（预检通过后才分配）：config/config.go及其测试仅在需要专用附件配置时增加独立字段；app/app.go、httpapi/router.go增加明确service依赖与独立路由；authoring_service.go给消息加omitempty operation ID；assistant_context_references_mysql.go的default新增scoped保存入口及对应测试；assistant_context_references.go抽共享预算但不加入无scope附件列表；screenplay_assistant_references.go目录/预览连接scoped repo；screenplay_authoring_routes.go的default带附件分支、screenplay_assistant_session_chat.go的服务器scope与原final guard连接；ComicScreenplayAuthoringPanels/useComicAssistantSessions/独立refs API或Picker仅接附件草稿/实际selector。只对附件消息加operation ID，旧无附件路径不改变。

冻结25文件全部不分配。尤其不改model.go/mysql_store.go/memory_store.go/storage/screenplay_schema.go/screenplay_workflow_register.go/comicScreenplayWorkflow.js。通过新增optional capability和app单独迁移注册避开它们。shot目录/whole/range证据和原来的最终source锁不改，计费/模型通道文件不分配。

## 验收及未完成边界

先受控测试、再父统一精确构建/实库/4001，不真实调用TOS或模型来掩盖合同缺陷。

1. UTF-8 BOM/中文/首尾空行/MD缩进与代码块/CRLF保留；64KiB±1和最终JSON16KiB±1；无效编码/NUL/空文件/二进制/伪字段拒绝。超限发生在TOS写前和模型/attempt前。
2. 同key同scope/hash回读同ID；换scope/hash/文件名冲突；跨用户不能找回；取消/断线/失败重试只沿原对象。首次Put前意图已提交，慢对象存储时另一会话及项目读取不被长事务堵住。
3. Put成功但Open错误/不同大小/hash不ready；DB写失败保留意图；namespace改变不得读错桶或新写第二份；同namespace凭据轮换仍可操作。显式X-Tos-Acl=private、owner-only ACL与缺失/空policy分别证实；403/普通404/public/未知策略一概禁用，未知不能冒充安全。无配置availability=false，不调用local对象假装TOS。
4. scope贯穿每个入口及最终事务。缺scope、伪owner/actor/session、跨项目、其他人draft、跨私聊/父归档、撤权再授、原登录/代操作失效均拒绝。read-only/关闭通道能合法准备/预览，但不能绕generate/原发送资格。
5. 真SQL先未变更正向成功，再测试共享标记/关联/历史/方案同事务回滚及两种真实锁等待次序。至少同一附件两轮成功消息有两个server operation ID/两条关联，无第二次TOS对象写入，旧消息完整快照不丢。
6. 真实provider受控捕获仅本轮实际文字，无TOS URL/key/其他私聊/审计token/旧附件全文。原8/16/32/96预算、old default连续无refs历史保护、shot分页/范围10项回归保持。
7. Chrome真实文件选择→准备→验证→预览→使用→回读；关闭重开/切会话/旧上传与预览回包不串草稿、不提前共享、不重试付费模型。服务不可用显示明确原因，不能用假ready或可点击空控件验收。

当前真实TOS尚未注入是运行层限制，不阻止先完成可执行合同及失败关闭测试；也不能据此把真实原件上传标完成。只有父预检通过后才写产品。DOCX/图片/OCR/Skills/Auto与D02整体继续待办。


## 2026-11-28真实只读回验后的ACL合同更正

父在同源真实只读检查中确认私有bucket响应只有Owner/Grants；policy精确404 NoSuchBucketPolicy。原实现额外要求BucketAclDelivered字段存在false，造成正常响应被拒。此为兼容错误，原失败日志保留，不改桶或凭据。

[官方GetBucketACL示例](https://docs.volcengine.com/docs/torch-object-storage/get-bucket-acl?lang=en)使用Owner与Grants，不包含BucketAclDelivered；父核对[GetObjectACL](https://docs.volcengine.com/docs/torch-object-storage/get-object-acl?lang=en)的IsDefault只在true返回及private样例省略BucketOwnerEntrusted/IsDefault，与[PutBucketACL默认false合同](https://docs.volcengine.com/docs/torch-object-storage/put-bucket-acl?lang=en)一致。仅这三个documented optional flags允许省略，仍用原始JSON区分省略与显式null/错类型/true。Owner/完整Grants、owner-only CanonicalUser/FULL_CONTROL、无/空policy、对象owner一致、private写头与原件hash验证全部继续必需；未知字段、缺Owner/Grants、额外主体仍拒绝。不把任意SDK零值当私有证据。

新增官方/真实脱敏Owner+Grants正向用例先RED，最小修正后GREEN；原missing-flags场景保留并改为符合API语义的正向，原null/错类型/true/未知/额外授权负向不删除。真实TOS重验由父独立负责，本实施者不获取凭据或调用TOS。
