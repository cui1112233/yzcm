import type { PublicMemberCenter } from "../config/profileData";
import { apiRequest } from "./api";

export type ProfileFields = { displayName: string; phone: string; email: string; teamTitle: string; bio: string };
export function editableProfile(center: PublicMemberCenter): ProfileFields {
  const member = center.member || {};
  return { displayName: member.displayName || "", phone: member.phone || "", email: member.email || "", teamTitle: member.teamTitle || "", bio: member.bio || "" };
}
export async function saveMemberProfile(input: ProfileFields, username: string) {
  const fields = Object.fromEntries(Object.entries(input).map(([key, value]) => [key, value.trim()])) as ProfileFields;
  if (!fields.displayName || fields.displayName.length > 40) throw new Error("显示名称须为 1–40 个字符");
  if (fields.phone.length > 32 || fields.email.length > 120 || fields.teamTitle.length > 60 || fields.bio.length > 240) throw new Error("资料超过允许长度");
  await apiRequest("/api/member/profile", { method: "PATCH", data: fields });
  const result = await apiRequest<PublicMemberCenter>("/api/member/me");
  if (result.member?.username !== username || Object.entries(fields).some(([key, value]) => (result.member?.[key] || "") !== value)) throw new Error("资料读回不一致，请重新读取核对，勿重复保存");
  return result;
}
export async function uploadProfileAvatar(avatarDataUrl: string, username: string) {
  if (!/^data:image\/(png|jpeg|webp);base64,[A-Za-z0-9+/=]+$/.test(avatarDataUrl)) throw new Error("请选择 PNG、JPG 或 WebP 图片");
  if (avatarDataUrl.split(",")[1].length > Math.ceil(2 * 1024 * 1024 / 3) * 4) throw new Error("头像不能超过 2MB");
  const saved = await apiRequest<PublicMemberCenter>("/api/member/avatar", { method: "POST", data: { avatarDataUrl } });
  const result = await apiRequest<PublicMemberCenter>("/api/member/me");
  if (!saved.member?.avatarUrl || result.member?.username !== username || result.member?.avatarUrl !== saved.member.avatarUrl) throw new Error("头像读回未确认，请重新读取核对");
  return result;
}
