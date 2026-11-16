import Taro from "@tarojs/taro";
import { Button, Input, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { sessionConfirmation } from "../../services/sessionConfirmation";
import { AccountSection, changeAccountPassword, readAccountSection, updateManagedMember, validAccountReceipt } from "../../services/memberAccount";

export default function AccountManagementView({ section }: { section: AccountSection }) {
  const [data, setData] = useState<any>(null); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const [password, setPassword] = useState(""); const [newPassword, setNewPassword] = useState(""); const [code, setCode] = useState("");
  const [mfaSetup, setMfaSetup] = useState<any>(null); const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [selected, setSelected] = useState<any>(null); const [displayName, setDisplayName] = useState(""); const [quota, setQuota] = useState("");
  const version = useRef(0); const mutation = useRef(false);
  const load = async () => {
    const current = ++version.current; setBusy(true); setError("");
    try { const result = await readAccountSection(section); if (current === version.current) setData(result); }
    catch (cause: any) { if (current === version.current) { setData(null); setError(cause.message || "读取失败"); } }
    finally { if (current === version.current) setBusy(false); }
  };
  useEffect(() => { setData(null); setSelected(null); setPassword(""); setNewPassword(""); setCode(""); setMfaSetup(null); setRecoveryCodes([]); void load(); return () => { version.current += 1; }; }, [section]);
  const run = async (operation: () => Promise<unknown>, success: string, refresh = true) => {
    if (mutation.current) return; mutation.current = true;
    const current = version.current; setBusy(true); setError("");
    try {
      await operation(); if (current !== version.current) return;
      setPassword(""); setNewPassword(""); setCode("");
      Taro.showToast({ title: success, icon: "success" });
      if (refresh) await load();
    } catch (cause: any) { if (current === version.current) setError(cause.message || "操作失败，请刷新核对"); }
    finally { mutation.current = false; if (current === version.current) setBusy(false); }
  };
  const confirm = async (content: string) => (await sessionConfirmation(() => Taro.showModal({ title: "确认账号操作", content }))).confirm;
  const mfaOperation = async (kind: "setup" | "enable" | "disable" | "recovery-codes") => {
    const current = version.current;
    if (kind !== "setup" && !(await confirm(kind === "disable" ? "关闭二步验证会降低账号安全性，继续？" : kind === "recovery-codes" ? "旧恢复码将全部失效，继续？" : "确认启用验证器二步验证？"))) return;
    if (current !== version.current) return;
    void run(async () => {
      const result = validAccountReceipt(await apiRequest(`/api/member/security/mfa/${kind}`, { method: "POST", data: kind === "enable" ? { code } : kind === "setup" ? { currentPassword: password } : { currentPassword: password, code } }));
      if (current !== version.current) return;
      if (kind === "setup") { if (!result.secret || !result.otpauthUri) throw new Error("服务器未返回有效验证器设置"); setMfaSetup(result); }
      else { setMfaSetup(null); if (Array.isArray(result.recoveryCodes)) setRecoveryCodes(result.recoveryCodes); }
    }, kind === "setup" ? "请在验证器中添加" : "操作已提交，请核对状态", kind !== "setup");
  };
  const members = section === "team" ? data?.members : section === "accounts" ? data?.accounts : [];
  const editMember = (member: any) => { setSelected(member); setDisplayName(member.displayName || ""); setQuota(String(member.monthlyTokenLimit ?? "")); };
  const saveMember = async () => {
    if (!selected || !["team", "accounts"].includes(section)) return;
    const monthlyTokenLimit = Number(quota); if (!Number.isFinite(monthlyTokenLimit) || monthlyTokenLimit < 0) { setError("请输入有效的非负额度"); return; }
    const current = version.current;
    void run(async () => { const result = await updateManagedMember(section as "team" | "accounts", selected.username, { displayName, monthlyTokenLimit }); if (current !== version.current) return; setData(result); setSelected(null); }, "已保存并核对", false);
  };
  return <View>
    {busy ? <Text className="mp-row-description">正在同步账号管理…</Text> : null}{error ? <Text className="mp-error">{error}</Text> : null}<Button className="mp-secondary" disabled={busy} onClick={load}>重新读取</Button>
    {section === "security" && data ? <>
      <View className="mp-card"><Text className="mp-row-title">登录设备</Text>{[...(data.sessions || []), ...(data.persistentSessions || [])].map((session, index) => <View className="mp-row mp-setting-row" key={session.id || index}><Text className="mp-row-description">{session.current ? "本次登录 · " : ""}{session.browser || "设备"} {session.os || ""} · {session.ipHint || "未提供地址"} · {session.issuedAt || ""}</Text></View>)}<Button disabled={busy} onClick={async () => { if (await confirm("退出其他设备的登录会话？")) void run(async () => { const result = validAccountReceipt(await apiRequest("/api/member/security/sessions/revoke-others", { method: "POST" })); if (typeof result.revoked !== "number") throw new Error("服务端未确认撤销数量"); }, "其他设备会话已撤销"); }}>退出其他设备</Button></View>
      <View className="mp-card"><Text className="mp-row-title">密码与二步验证</Text><Text className="mp-row-description">MFA：{data.mfa.enabled ? "已启用" : "未启用"}；恢复码剩余 {data.mfa.recoveryCodesRemaining ?? "未返回"} 个。</Text><Input className="mp-input" password value={password} placeholder="当前密码" onInput={(event) => setPassword(event.detail.value)} /><Input className="mp-input" password value={newPassword} placeholder="新密码，至少 8 位" onInput={(event) => setNewPassword(event.detail.value)} /><Button disabled={busy} onClick={async () => { if (await confirm("修改当前账号密码，并退出其他登录设备？")) void run(() => changeAccountPassword(password, newPassword), "密码已修改"); }}>修改密码</Button>
        <Input className="mp-input" value={code} placeholder="验证器动态码或恢复码" onInput={(event) => setCode(event.detail.value)} />
        {!data.mfa.enabled ? <Button disabled={busy} onClick={() => mfaOperation("setup")}>设置验证器</Button> : <><Button disabled={busy} onClick={() => mfaOperation("disable")}>关闭二步验证</Button><Button disabled={busy} onClick={() => mfaOperation("recovery-codes")}>更新恢复码</Button></>}
        {mfaSetup ? <View><Text className="mp-row-description">在验证器中手动添加以下密钥，输入动态码后启用。请勿分享。</Text><Text>{mfaSetup.secret}</Text><Button disabled={busy} onClick={() => mfaOperation("enable")}>验证并启用</Button><Button onClick={() => setMfaSetup(null)}>取消</Button></View> : null}
        {recoveryCodes.length ? <View><Text className="mp-row-description">一次性恢复码仅本次显示，请安全保存。</Text>{recoveryCodes.map((value) => <Text key={value}>{value}</Text>)}<Button onClick={() => setRecoveryCodes([])}>我已保存并关闭</Button></View> : null}
        <Text className="mp-row-description">设备 Passkey 管理请使用网页版账号安全页；小程序不支持浏览器 WebAuthn 注册。</Text>
      </View>
    </> : null}
    {(section === "team" || section === "accounts") && data ? <>
      {(members || []).map((member: any) => <View className="mp-card" key={member.username}><Text className="mp-row-title">{member.displayName || member.username} · @{member.username}</Text><Text className="mp-row-description">{member.role} · {member.active === true ? "启用" : member.active === false ? "停用" : "未返回状态"} · 月额度 {member.monthlyTokenLimit ?? "未返回"}</Text><Text className="mp-row-description">已授权：{Array.isArray(member.apiScopes) ? member.apiScopes.join("、") || "无" : "未返回"}</Text><Button disabled={busy} onClick={() => editMember(member)}>编辑名称与额度</Button></View>)}
      {!members?.length ? <Text className="mp-row-description">当前账号可管理的成员列表为空。</Text> : null}
      {selected ? <View className="mp-card"><Text className="mp-row-title">编辑 @{selected.username}</Text><Input className="mp-input" value={displayName} placeholder="显示名称" onInput={(event) => setDisplayName(event.detail.value)} /><Input className="mp-input" type="number" value={quota} placeholder="月度 Token 额度" onInput={(event) => setQuota(event.detail.value)} /><Button disabled={busy} onClick={saveMember}>保存并核对</Button><Button onClick={() => setSelected(null)}>取消</Button></View> : null}
    </> : null}
    {section === "governance" && data ? (data.teams || []).map((team: any) => <GovernanceRow key={team.manager?.username || team.managerUsername} team={team} disabled={busy} onSave={(username, limit) => run(async () => { validAccountReceipt(await apiRequest(`/api/member/team/governance/${encodeURIComponent(username)}`, { method: "PATCH", data: { monthlyTokenLimit: limit } })); const readback = await readAccountSection("governance"); const found = readback.teams.find((item: any) => (item.manager?.username || item.managerUsername) === username); if (!found || Number(found.monthlyTokenLimit ?? found.policy?.monthlyTokenLimit) !== limit) throw new Error("团队额度读回不一致，请刷新核对"); }, "团队额度已保存并核对")} />) : null}
  </View>;
}

function GovernanceRow({ team, disabled, onSave }: { team: any; disabled: boolean; onSave: (username: string, limit: number) => void }) {
  const [limit, setLimit] = useState(String(team.monthlyTokenLimit ?? team.policy?.monthlyTokenLimit ?? ""));
  useEffect(() => setLimit(String(team.monthlyTokenLimit ?? team.policy?.monthlyTokenLimit ?? "")), [team]);
  const username = team.manager?.username || team.managerUsername;
  return <View className="mp-card"><Text className="mp-row-title">{team.team?.name || username}</Text><Text className="mp-row-description">负责人 @{username} · 团队总额度</Text><Input className="mp-input" type="number" value={limit} onInput={(event) => setLimit(event.detail.value)} /><Button disabled={disabled || !limit || !Number.isFinite(Number(limit)) || Number(limit) < 0} onClick={() => onSave(username, Number(limit))}>保存团队额度</Button></View>;
}
