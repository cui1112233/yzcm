import type { AppIconName } from "../components/AppIcon";

export type ProfileItem = {
  key: string;
  title: string;
  description: string;
  icon: AppIconName;
};

export type ProfileSection = {
  key: string;
  title: string;
  note?: string;
  items: ProfileItem[];
};

const PROFILE_ITEMS: Record<string, ProfileItem> = {
  profile: { key: "profile", title: "个人资料", description: "头像、名称、联系方式与简介", icon: "user-round" },
  memberCenter: { key: "memberCenter", title: "会员中心", description: "身份、团队、额度与 AI 服务", icon: "crown" },
  security: { key: "security", title: "账号安全", description: "密码、MFA 与登录会话", icon: "shield-check" },
  governance: { key: "governance", title: "联合治理", description: "跨团队协作与成员治理", icon: "users-round" },
  apiConfig: { key: "apiConfig", title: "API 配置", description: "模型服务与 API 配置状态", icon: "key-round" },
  usage: { key: "usage", title: "用量与制作", description: "Token、调用与内容制作统计", icon: "chart" },
  team: { key: "team", title: "组员管理", description: "团队成员、权限与额度", icon: "users-round" },
  accounts: { key: "accounts", title: "账号与角色", description: "全局账号与角色授权", icon: "users-round" },
  adminConsole: { key: "adminConsole", title: "管理后台", description: "进入公网管理端", icon: "shield-check" },
};

function canManageAccount(member?: { role?: unknown }) {
  return ["dev", "manager"].includes(String(member?.role || "").toLowerCase());
}

function canAccessAdmin(member?: { role?: unknown }) {
  return String(member?.role || "").toLowerCase() === "dev";
}

export function buildProfileSections(member?: { role?: unknown }): ProfileSection[] {
  const sections: ProfileSection[] = [
  {
    key: "profile",
    title: "个人资料",
    note: "账号中心数据",
    items: [
      PROFILE_ITEMS.profile,
      PROFILE_ITEMS.memberCenter,
      PROFILE_ITEMS.security,
      ...(canManageAccount(member) ? [PROFILE_ITEMS.governance] : []),
    ],
  },
  {
    key: "service",
    title: "服务",
    items: [
      PROFILE_ITEMS.apiConfig,
      PROFILE_ITEMS.usage,
      ...(canManageAccount(member) ? [PROFILE_ITEMS.team] : []),
    ],
  }];

  if (canAccessAdmin(member)) {
    sections.push({ key: "developer", title: "开发者工具", items: [PROFILE_ITEMS.accounts, PROFILE_ITEMS.adminConsole] });
  }

  return sections;
}

export const PROFILE_SECTIONS = buildProfileSections();
