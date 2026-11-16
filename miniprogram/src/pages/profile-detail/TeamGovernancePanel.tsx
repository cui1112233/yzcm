import Taro from "@tarojs/taro";
import { Button, Picker, Switch, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { validAccountReceipt } from "../../services/memberAccount";
import { sessionRevision } from "../../services/sessionBoundary";

const scopeNames = [["text", "文本"], ["image", "生图"], ["tts", "配音"], ["video", "视频"]];
export default function TeamGovernancePanel() {
  const [teams, setTeams] = useState<any[]>([]), [teamId, setTeamId] = useState(""), [members, setMembers] = useState<any[]>([]), [editing, setEditing] = useState<any>(null);
  const [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const version = useRef(0), lock = useRef(false), uncertain = useRef(false);
  const readMembers = async (id: string) => {
    const value = validAccountReceipt(await apiRequest(`/api/team-admin/teams/${encodeURIComponent(id)}/members`));
    if (!Array.isArray(value.members)) throw new Error("服务端未返回团队成员列表");
    return value.members;
  };
  const load = async () => {
    if (lock.current) return;
    const ticket = ++version.current; setBusy(true); setError(""); setMembers([]); setEditing(null);
    try {
      const value = validAccountReceipt(await apiRequest("/api/team-admin/teams"));
      if (!Array.isArray(value.teams)) throw new Error("服务端未返回可治理团队");
      const id = String(value.teams.find((row: any) => String(row.team?.id) === teamId)?.team?.id || value.teams[0]?.team?.id || "");
      const rows = id ? await readMembers(id) : [];
      if (ticket === version.current) { setTeams(value.teams); setTeamId(id); setMembers(rows); uncertain.current = false; }
    } catch (cause: any) { if (ticket === version.current) setError(cause.message); }
    finally { if (ticket === version.current) setBusy(false); }
  };
  useEffect(() => { void load(); return () => { version.current++; }; }, []);
  const select = async (index: number) => {
    if (busy || lock.current) return;
    const id = String(teams[index]?.team?.id || ""), ticket = ++version.current;
    setTeamId(id); setMembers([]); setEditing(null); setBusy(true); setError("");
    try { const rows = await readMembers(id); if (ticket === version.current) { setMembers(rows); uncertain.current = false; } }
    catch (cause: any) { if (ticket === version.current) setError(cause.message); }
    finally { if (ticket === version.current) setBusy(false); }
  };
  const save = async () => {
    if (!editing || lock.current) return;
    if (uncertain.current) { setError("上次修改结果未确认，请重新读取核对后再操作"); return; }
    lock.current = true; setBusy(true); setError(""); const ticket = version.current, session = sessionRevision();
    try {
      const confirmed = await Taro.showModal({ title: "修改团队成员", content: `保存 @${editing.username} 的登录状态及模型权限？` });
      if (!confirmed.confirm || ticket !== version.current || session !== sessionRevision()) return;
      uncertain.current = true;
      const root = `/api/team-admin/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(editing.username)}`;
      validAccountReceipt(await apiRequest(`${root}/status`, { method: "POST", data: { active: editing.active } }));
      validAccountReceipt(await apiRequest(`${root}/api-scopes`, { method: "PUT", data: { scopes: editing.apiScopes } }));
      const rows = await readMembers(teamId), found = rows.find((row: any) => row.username === editing.username);
      if (!found || found.active !== editing.active || JSON.stringify([...(found.apiScopes || [])].sort()) !== JSON.stringify([...editing.apiScopes].sort())) throw new Error("成员权限读回不一致");
      if (ticket === version.current) { setMembers(rows); setEditing(null); uncertain.current = false; }
    } catch (cause: any) { if (ticket === version.current) setError(`${cause.message}，请重新读取核对`); }
    finally { lock.current = false; if (ticket === version.current) setBusy(false); }
  };
  return <View>{error ? <Text className="mp-error">{error}</Text> : null}<Button disabled={busy} loading={busy} onClick={() => void load()}>重新读取团队与成员</Button>
    <Picker disabled={busy} range={teams.map(row => row.team?.name || row.manager?.username || row.team?.id)} value={Math.max(0, teams.findIndex(row => String(row.team?.id) === teamId))} onChange={e => void select(Number(e.detail.value))}><View className="mp-picker-field"><Text>{teams.find(row => String(row.team?.id) === teamId)?.team?.name || "选择团队"}</Text></View></Picker>
    {members.filter(row => row.role === "member").map(row => <View className="mp-card" key={row.username}><Text className="mp-row-title">{row.displayName || row.username} · @{row.username}</Text><Text>{row.active === false ? "停用" : "正常"} · {row.apiScopes?.join("、") || "无模型权限"}</Text><Button disabled={busy} onClick={() => setEditing({ ...row, active: row.active !== false, apiScopes: row.apiScopes?.includes("*") ? scopeNames.map(([key]) => key) : row.apiScopes || [] })}>管理登录与模型权限</Button></View>)}
    {!busy && !members.length ? <Text>此团队未返回成员</Text> : null}
    {editing ? <View className="mp-card"><Text className="mp-row-title">@{editing.username}</Text><View><Text>允许登录</Text><Switch disabled={busy} checked={editing.active} onChange={e => setEditing({ ...editing, active: e.detail.value })} /></View>{scopeNames.map(([key, label]) => <View key={key}><Text>{label}</Text><Switch disabled={busy} checked={editing.apiScopes.includes(key)} onChange={e => setEditing({ ...editing, apiScopes: e.detail.value ? [...editing.apiScopes.filter((v: string) => v !== key), key] : editing.apiScopes.filter((v: string) => v !== key) })} /></View>)}<Button disabled={busy} onClick={() => void save()}>保存并核对</Button><Button disabled={busy} onClick={() => setEditing(null)}>取消</Button></View> : null}
  </View>;
}
