import Taro from "@tarojs/taro";
import { Button, Input, Picker, Switch, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { sessionRevision } from "../../services/sessionBoundary";
import { createManagedAccount, readAccountSection, resetManagedMemberPassword, setManagedMemberAccess, updateManagedMember } from "../../services/memberAccount";

const scopeChoices = [["text", "文本"], ["image", "生图"], ["tts", "配音"], ["video", "视频"]];
export default function ManagedAccountsPanel({ section }: { section: "team" | "accounts" }) {
  const [rows, setRows] = useState<any[]>([]), [allowed, setAllowed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<any>(null), [passwordTarget, setPasswordTarget] = useState("");
  const [username, setUsername] = useState(""), [name, setName] = useState(""), [password, setPassword] = useState(""), [role, setRole] = useState("member"), [active, setActive] = useState(true), [scopes, setScopes] = useState<string[]>([]);
  const epoch = useRef(0), lock = useRef(false), unknown = useRef(false);
  const accept = (value: any) => setRows(section === "team" ? value.members : value.accounts);
  const load = async () => {
    if (lock.current) return;
    const version = ++epoch.current; setBusy(true); setError(""); setAllowed(false);
    try {
      const center = await apiRequest<any>("/api/member/me");
      if (version !== epoch.current) return;
      const canManage = section === "accounts" ? center?.member?.role === "dev" : ["dev", "manager"].includes(center?.member?.role);
      if (!canManage) throw new Error(section === "accounts" ? "仅 DEV 可管理账号与角色" : "当前账号无组员管理权限");
      const value = await readAccountSection(section);
      if (version === epoch.current) { accept(value); setAllowed(true); unknown.current = false; }
    } catch (cause: any) { if (version === epoch.current) setError(cause.message); }
    finally { if (version === epoch.current) setBusy(false); }
  };
  useEffect(() => { setEditing(null); setPassword(""); void load(); return () => { epoch.current++; }; }, [section]);
  const open = (row: any) => {
    setEditing(row); setUsername(row.username || ""); setName(row.displayName || ""); setPassword(""); setRole(row.role || "member"); setActive(row.active !== false); setScopes(row.apiScopes?.includes("*") ? scopeChoices.map(([value]) => value) : row.apiScopes || []); setPasswordTarget("");
  };
  const submit = async () => {
    if (lock.current || !allowed) return;
    if (unknown.current) { setError("上次操作未确认，请先重新读取并核对，勿重复提交"); return; }
    if (!passwordTarget && (!name.trim() || (!editing?.username && (!username.trim() || password.length < 8)))) { setError("请填写账号、显示名称和至少 8 位初始密码"); return; }
    if (passwordTarget && password.length < 8) { setError("新密码至少 8 位"); return; }
    lock.current = true; setBusy(true); setError(""); setNotice(""); const version = epoch.current, session = sessionRevision();
    try {
      const confirm = await Taro.showModal({ title: "确认账号操作", content: passwordTarget ? `重置 @${passwordTarget} 的密码？` : `保存 @${editing?.username || username} 的账号与权限设置？` });
      if (!confirm.confirm || version !== epoch.current || session !== sessionRevision()) return;
      unknown.current = true;
      let result: any;
      if (passwordTarget) await resetManagedMemberPassword(passwordTarget, password);
      else if (!editing.username) result = await createManagedAccount(section, { username, displayName: name, password, role, apiScopes: scopes });
      else if (section === "team") {
        await updateManagedMember(section, editing.username, { displayName: name.trim() });
        result = await setManagedMemberAccess(editing.username, active, scopes);
      } else result = await updateManagedMember(section, editing.username, { displayName: name.trim(), role, active });
      if (version !== epoch.current) return;
      if (result) accept(result);
      setNotice(passwordTarget ? "服务端已确认重置请求；密码不回显" : "已保存并读回核对");
      unknown.current = false; setEditing(null); setPasswordTarget(""); setPassword("");
    } catch (cause: any) { if (version === epoch.current) { setError(`${cause.message}。请重新读取核对后再操作`); setPassword(""); } }
    finally { lock.current = false; if (version === epoch.current) setBusy(false); }
  };
  return <View>
    {error ? <Text className="mp-error">{error}</Text> : null}{notice ? <Text className="mp-row-description">{notice}</Text> : null}
    <Button disabled={busy} loading={busy} onClick={() => void load()}>重新读取并核对</Button>
    {allowed ? <><Button disabled={busy} onClick={() => open({})}>{section === "team" ? "添加组员" : "添加账号"}</Button>
      {rows.map(row => <View className="mp-card" key={row.username}><Text className="mp-row-title">{row.displayName || row.username} · @{row.username}</Text><Text className="mp-row-description">{row.role} · {row.active === false ? "停用" : "正常"} · {row.apiScopes?.join("、") || "未授权能力"}</Text>{row.role !== "dev" ? <Button disabled={busy} onClick={() => open(row)}>管理账号</Button> : null}{section === "team" && row.role === "member" ? <Button disabled={busy} onClick={() => { setEditing(null); setPasswordTarget(row.username); setPassword(""); }}>重置密码</Button> : null}</View>)}
      {!rows.length ? <Text>当前列表为空</Text> : null}
      {editing || passwordTarget ? <View className="mp-card"><Text className="mp-row-title">{passwordTarget ? `重置 @${passwordTarget} 密码` : editing.username ? `编辑 @${editing.username}` : "创建账号"}</Text>
        {!passwordTarget ? <><Input disabled={busy || Boolean(editing.username)} maxlength={128} className="mp-input" value={username} placeholder="账号" onInput={e => setUsername(e.detail.value)} /><Input disabled={busy} maxlength={256} className="mp-input" value={name} placeholder="显示名称" onInput={e => setName(e.detail.value)} />{section === "accounts" ? <Picker disabled={busy} range={["MEMBER", "MANAGER"]} value={role === "manager" ? 1 : 0} onChange={e => setRole(Number(e.detail.value) ? "manager" : "member")}><Text>角色：{role.toUpperCase()}</Text></Picker> : null}{editing.username ? <View><Text>允许登录</Text><Switch disabled={busy} checked={active} onChange={e => setActive(e.detail.value)} /></View> : null}{section === "team" ? scopeChoices.map(([value, label]) => <View key={value}><Text>{label}</Text><Switch disabled={busy} checked={scopes.includes(value)} onChange={e => setScopes(current => e.detail.value ? [...current.filter(item => item !== value), value] : current.filter(item => item !== value))} /></View>) : null}</> : null}
        {passwordTarget || !editing?.username ? <Input disabled={busy} password maxlength={1024} className="mp-input" value={password} placeholder="至少 8 位密码" onInput={e => setPassword(e.detail.value)} /> : null}
        <Button disabled={busy} loading={busy} onClick={() => void submit()}>确认保存</Button><Button disabled={busy} onClick={() => { setEditing(null); setPasswordTarget(""); setPassword(""); }}>取消</Button>
      </View> : null}
    </> : null}
  </View>;
}
