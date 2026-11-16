import type { PublicMemberCenter } from "../../config/profileData";
import { profileFacts, type ProfileFacts } from "../../config/profileData";
import { readMemberProfile } from "../../services/memberProfile";

export type MemberCenterView = {
  facts: ProfileFacts;
  quota: { used?: number; limit?: number | null; percent?: number };
};

function hasMember(value: PublicMemberCenter | null | undefined) {
  return Boolean(value?.member && typeof value.member === "object" && !Array.isArray(value.member));
}

export function toMemberCenterView(center: PublicMemberCenter): MemberCenterView {
  const facts = profileFacts(center);
  const used = facts.monthlyTokenUsage;
  const limit = facts.monthlyTokenLimit;
  const percent = typeof used === "number" && typeof limit === "number" && limit > 0
    ? Math.min(100, Math.round((used / limit) * 100))
    : undefined;
  return { facts, quota: { used, limit, percent } };
}

export async function readMemberCenterView(): Promise<MemberCenterView> {
  const center = await readMemberProfile();
  if (!hasMember(center)) throw new Error("服务端未返回有效的会员资料");
  return toMemberCenterView(center);
}
