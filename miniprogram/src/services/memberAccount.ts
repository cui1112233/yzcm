import { apiRequest } from "./api";
export type AccountSection = "security" | "team" | "governance" | "accounts";
const PATHS = { security: "/api/member/security", team: "/api/member/team", governance: "/api/member/team/governance", accounts: "/api/account-admin/accounts" };
export function validAccountReceipt(value: any) {
  if (!value || typeof value !== "object" || Array.isArray(value) || value.ok === false || value.error) throw new Error(String(value?.message || value?.error || "服务端未确认操作，请刷新核对"));
  return value;
}
export async function readAccountSection(section: AccountSection) {
  const value = validAccountReceipt(await apiRequest<any>(PATHS[section]));
  const valid = section === "security" ? typeof value.mfa?.enabled === "boolean" && Array.isArray(value.sessions) : Array.isArray(value[{ team: "members", governance: "teams", accounts: "accounts" }[section]]);
  if (!valid) throw new Error("服务端未返回有效的账号管理数据");
  return value;
}
export async function updateManagedMember(section: "team" | "accounts", username: string, patch: Record<string, unknown>) {
  const path = section === "team" ? "/api/member/team/members" : "/api/account-admin/accounts";
  validAccountReceipt(await apiRequest(`${path}/${encodeURIComponent(username)}`, { method: "PATCH", data: patch }));
  const readback = await readAccountSection(section);
  const entry = (section === "team" ? readback.members : readback.accounts).find((item: any) => item.username === username);
  if (!entry || Object.entries(patch).some(([key, value]) => JSON.stringify(entry[key]) !== JSON.stringify(value))) throw new Error("账号修改读回不一致，请刷新核对");
  return readback;
}
export async function changeAccountPassword(currentPassword: string, newPassword: string) {
  if (!currentPassword || newPassword.length < 8) throw new Error("请输入当前密码和至少 8 位的新密码");
  const result = validAccountReceipt(await apiRequest("/api/member/security/password", { method: "POST", data: { currentPassword, newPassword } }));
  if (result.changed !== true) throw new Error("服务端未确认密码修改，请勿重复提交");
  return result;
}
export async function createManagedAccount(section: "team" | "accounts", values: { username: string; displayName: string; password: string; role: string; apiScopes: string[] }) {
  const username = values.username.trim(), displayName = values.displayName.trim();
  if (!username || !displayName || values.password.length < 8 || !["member", "manager"].includes(values.role)) throw new Error("请填写账号、名称和至少 8 位密码，并选择角色");
  const data = section === "team" ? { username, displayName, password: values.password, role: "member", apiEnabled: Boolean(values.apiScopes.length), apiScopes: values.apiScopes } : { username, displayName, password: values.password, role: values.role, monthlyTokenLimit: null };
  validAccountReceipt(await apiRequest(section === "team" ? "/api/member/team/members" : PATHS.accounts, { method: "POST", data }));
  const readback = await readAccountSection(section);
  if (!(section === "team" ? readback.members : readback.accounts).some((entry: any) => entry.username === username && entry.displayName === displayName)) throw new Error("新账号读回未确认，请刷新核对，勿重复创建");
  return readback;
}
export async function setManagedMemberAccess(username: string, active: boolean, scopes: string[]) {
  const root = `/api/member/team/members/${encodeURIComponent(username)}`;
  validAccountReceipt(await apiRequest(`${root}/status`, { method: "POST", data: { active } }));
  validAccountReceipt(await apiRequest(`${root}/api-scopes`, { method: "PUT", data: { scopes } }));
  const readback = await readAccountSection("team");
  const member = readback.members.find((entry: any) => entry.username === username);
  if (!member || member.active !== active || JSON.stringify([...(member.apiScopes || [])].sort()) !== JSON.stringify([...scopes].sort())) throw new Error("权限读回不一致，请刷新核对");
  return readback;
}
export async function resetManagedMemberPassword(username: string, password: string) {
  if (password.length < 8) throw new Error("新密码至少 8 位");
  return validAccountReceipt(await apiRequest(`/api/member/team/members/${encodeURIComponent(username)}/reset-password`, { method: "POST", data: { password } }));
}
