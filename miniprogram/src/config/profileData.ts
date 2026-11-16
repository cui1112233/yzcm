export type PublicMemberCenter = {
  member?: Record<string, any>;
  team?: { name?: string; id?: string };
  usage?: { day?: Record<string, any>; month?: Record<string, any>; recent?: any[] };
};

export type ProfileFacts = {
  displayName?: string;
  username?: string;
  role?: string;
  teamName?: string;
  avatarUrl?: string;
  bio?: string;
  phone?: string;
  email?: string;
  teamTitle?: string;
  apiEnabled?: boolean;
  scopes?: string[];
  monthlyTokenUsage?: number;
  monthlyTokenLimit?: number | null;
};

export function formatTokenAmount(value: number | null | undefined) {
  if (value === null) return "不限额";
  if (value === undefined) return "服务端未返回";
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function optionalString(value: unknown) {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function optionalNumber(value: unknown) {
  const number = typeof value === "number" ? value : typeof value === "string" && value.trim() ? Number(value) : Number.NaN;
  return Number.isFinite(number) ? number : undefined;
}

export function monthlyTokenUsage(center: PublicMemberCenter | null | undefined) {
  return optionalNumber(center?.usage?.month?.totalTokens);
}

export function profileFacts(center: PublicMemberCenter | null | undefined): ProfileFacts {
  const member = center?.member;
  const scopes = Array.isArray(member?.apiScopes) && member.apiScopes.every((scope: unknown) => typeof scope === "string")
    ? member.apiScopes
    : undefined;

  return {
    displayName: optionalString(member?.displayName),
    username: optionalString(member?.username),
    role: optionalString(member?.role),
    teamName: optionalString(member?.team?.name) || optionalString(center?.team?.name),
    avatarUrl: optionalString(member?.avatarUrl),
    bio: optionalString(member?.bio),
    phone: optionalString(member?.phone),
    email: optionalString(member?.email),
    teamTitle: optionalString(member?.teamTitle),
    apiEnabled: typeof member?.apiEnabled === "boolean" ? member.apiEnabled : undefined,
    scopes,
    monthlyTokenUsage: monthlyTokenUsage(center),
    monthlyTokenLimit: member?.monthlyTokenLimit === null ? null : optionalNumber(member?.monthlyTokenLimit),
  };
}

export function profileScopes(center: PublicMemberCenter | null | undefined) {
  return profileFacts(center).scopes || [];
}

export function profileTokenLimit(center: PublicMemberCenter | null | undefined) {
  return profileFacts(center).monthlyTokenLimit;
}
