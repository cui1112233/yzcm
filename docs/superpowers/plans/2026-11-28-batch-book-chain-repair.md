# 批量测书链路修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复已确认的敏感回写、分类门禁、AI复用及配置执行缺口，并统一更新本地4001。

**Architecture:** 复用现有Go处理与队列，不加第二套词库、不加评分或无限重试。测书保留真实分类失败，工厂默认策略不变；有效正文和已确认上传记录复用。大文件由root统一整合不同工作者的窄修改，测试在独立Git归档执行。

**Tech Stack:** Go、React、node:test、现有MySQL/Redis/TOS及Go embed构建。

## Global Constraints

- 基线c8885274，最终写入v88；按用户要求不新建分支，不重置或覆盖无关脏文件，不推送、不生产部署。
- 用户已授权单书真实全流程包括上传；当前凭据接管尚未返回，开发与隔离验证可先继续，不操作浏览器凭据。
- 原始正文及旧AI1保留，不直接修改真实词库/书正文作为修复手段。
- AI最多20版本，保持明确所选版本及既有运行快照；明确空选择不提交。
- 分类只判断缺失标签，合法标签直接复用；失败保留安全原因，显式重试才重新判。不新增置信度门禁，不删除工厂的默认行为。
- 不增加自动重试次数；原文敏感AI按开关执行，AI文案仍先规则处理再保存；仅生成不自动上传。
- 报告未确认的旧词库字段兼容、历史词库变化自动重处理和真实联合分类效率，不扩大为本批额外重构。

## Task 1: 敏感处理正确回写与真实结果（F1–F3）

Files: novelfetchworkshop/sensitive.go及必要新定位helper；httpapi/novel_fetch_workshop.go敏感处理区、novel_fetch_rules_body_safety.go；相关新回归。

- [ ] 将归档探针改为正确行为断言，实际RED：长行范围外文字保留、同词多次命中应用、重叠/多规则/中文定位、无定位不能假成功。
- [ ] 实现精确片段定位/写回，维护已存在分组、显式删除、禁用范围语义。
- [ ] 手动敏感AI修复尊重本次明确请求；模型解析错误和条目失败阻止ready保存，逐版本返回真实失败。
- [ ] 保留原始正文和旧版本；普通处理若按既有策略本地降级，明确标注降级来源，不伪造AI成功。
- [ ] GREEN相关规则/敏感/正文持久化测试，root核对差异后整合。

## Task 2: 测书分类与生成共同门禁（F4–F6）

Files: httpapi/novel_fetch_classification_reservation.go、novel_fetch_workshop.go worker段、novel_fetch_execution_admission.go及相关测试；必要小helper。

- [ ] RED：测书失败不补女频/默认风格、敏感关闭仍能判断风格、单步生成分类缺失不产生改文调用、有效标签不重判。
- [ ] 测书调用ResolveGender/ResolveStyle真实结果，保留工厂ClassifyBook默认策略。
- [ ] 分类provider独立于敏感开关；单步生成沿用同一分类准备/门禁，并保留不上传语义及有效快照。
- [ ] 失败原因可见、不会后台无限重判；保持付费调用前预约/不确定结果人工保护。
- [ ] GREEN分类、执行准入、重试/停止回归。

## Task 3: AI计划与有效成果复用（F7、F10）

Files: novelfetchworkshop/rewrite.go及必要新helpers；httpapi有效AI候选helper、worker复用检查；相关测试。

- [ ] RED：off不注入参考、按风格挑选兼容参考、旧非法正文不再当版本已齐、已确认历史成果不被覆盖。
- [ ] 尊重opening/high模式及已有策略配置；不改变未修改的用户提示词和账号配置。
- [ ] 统一未确认AI的有效性判断，生成/恢复与上传口径一致；明确失败版本允许在显式生成/重试时重做。
- [ ] 不自动重生已确认上传版本，不删除旧成果再试；模型失败保留旧正文。
- [ ] GREEN改文请求/校验/持久化、有效AI候选及相关上传检查。

## Task 4: 明确模型、明确版本和数量（F8、F9、F11）

Files: frontend/NovelFetchPage.jsx与小设置helper/test；httpapi/novel_fetch_workshop.go选择归一化、web_submit_service.go/预览版本入口；AI数量相关测试。

- [ ] RED：不可用的既选模型保持原选择并明确提示，不能自动替成第一个再保存。
- [ ] RED：明确空版本不触发上传；网站API没有传版本时仍可沿用其历史默认行为，不能混淆显式空选择。
- [ ] 版本候选支持ai1–ai20，别名规范化、上界拒绝与请求20实际20统一；保留现有默认选择，不把原1版本自动扩大为20。
- [ ] GREEN前端设置和版本选择测试、空选择队列到上传替身、数量及回执去重相关测试。

## Task 5: 整合、复核与统一更新

- [ ] 各工作者用独立归档测试，只交付窄patch；root统一整合共享大文件，保留所有无关修改。
- [ ] 每批先RED后GREEN，交叉核对实现与本计划，再提交v88明确路径。
- [ ] 干净Git快照运行NFW/storage/app及HTTP全回归、相关前端测试；构建React和Go embed。
- [ ] 隔离完整二进制验收：无外部服务调用，账号/模型保护、保存后读回、空选择和关键原文保护。
- [ ] 确认实际4001和空闲队列，记录正文及发布记录指纹、回退程序；本地切换后核对SHA、前端入口、原有数据。
- [ ] 只在用户归还凭据页面后继续实际单书分类、AI2、指定上传与网站回读；开发验收不冒充真实上传。
- [ ] 更新验收报告，列明已修复、测试/运行身份及剩余外部条件。
