# 个人中心设计图归档

本目录是 2026-11-26 个人中心方案的统一视觉上下文。实施与验收时先读正式设计文档：

- `docs/superpowers/specs/2026-11-26-personal-center-energy-roles-design.md`
- `docs/superpowers/plans/2026-11-26-personal-center-rollout.md`

## approved：已确认的页面方向

1. `01-account-overview.png`：账户总览、能量与快捷入口。
2. `02-profile-compact.png`：紧凑个人资料；身份图标放在名字旁边。
3. `03-account-security.png`：密码、MFA、Passkey、邮箱和设备安全。
4. `04-team-management.png`：本团队成员、能量消耗、切换账号和管理动作。
5. `05-all-team-consumption.png`：开发创始与开发者查看全部团队消费；余额逐团队展示。
6. `06-api-manager-preset-modal.png`：管理者点击“自定义预设”后才打开 YZCM 模型库。
7. `07-accounts-by-team.png`：账号与角色按团队分组展开。

这些图片确定视觉和布局方向，文字、权限和数据口径以正式设计文档为准。特别注意：

- 正式身份名称只有“开发创始、开发者、管理者、组员”。图中出现的旧称或错误日期不能进入实现。
- 管理者的模型选择弹窗只负责选择 YZCM 模型；每个模型的 Key 输入框仍直接显示在弹窗外的模型卡片里。
- API 配置必须保留本地 4001 当前真实页面已有的平台预设、能力分组、自动保存、额度刷新和附加配置。
- 不允许照图做虚假数据、假按钮、合并团队余额或第二套钱包。

## references：用户原始参考和真实功能截图

- `01-brand-home-energy.png`：品牌 Logo、能量释放和深色动态背景。
- `02-glass-identity-ribbon.png`：通透玻璃与彩色能量带材质。
- `03-vip-icon-inspiration.png`：小型立体身份图标的占位关系，不照搬 V 图案。
- `04-dynamic-energy-background.png`：动态背景与能量粒子方向。
- `05-current-preset-modal.png`：当前真实“自定义预设”弹窗。
- `06-current-inline-key.png`：当前真实模型卡片内直接填写 Key。
- `07-current-add-custom-model.png`：当前真实“添加自定义模型”，仅开发创始和获授权开发者保留。
- `08-role-icon-shape-study-old-labels.png`：只参考品牌衍生图标形态；其中旧身份名称全部作废。

## 不使用的旧稿

`docs/design/2026-11-26-profile-center-energy-command-v2.png` 和 `docs/design/2026-11-26-profile-center-security-v3.png` 是早期黑金方案，不属于最终玻璃视觉，不得作为实现依据。
