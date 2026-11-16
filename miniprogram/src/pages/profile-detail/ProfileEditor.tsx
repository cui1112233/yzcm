import { Button, Image, Input, Text, Textarea, View } from "@tarojs/components";
import Taro from "@tarojs/taro";
import { useEffect, useRef, useState } from "react";
import type { PublicMemberCenter } from "../../config/profileData";
import { apiUrl } from "../../config/runtime";
import AppIcon from "../../components/AppIcon";
import { editableProfile, saveMemberProfile, uploadProfileAvatar, type ProfileFields } from "../../services/profileEditor";
import "./profile-editor.scss";
import { sessionRevision } from "../../services/sessionBoundary";

export default function ProfileEditor({ center, onSaved }: { center: PublicMemberCenter; onSaved: (center: PublicMemberCenter) => void }) {
  const [fields, setFields] = useState(() => editableProfile(center));
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const locked = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const member = center.member || {};
  const change = (key: keyof ProfileFields, value: string) => setFields(current => ({ ...current, [key]: value }));
  const perform = async (kind: string, action: () => Promise<PublicMemberCenter>) => {
    if (locked.current) return;
    locked.current = true; setBusy(kind); setError("");
    try {
      const result = await action();
      if (!mounted.current) return;
      onSaved(result);
      if (kind === "save") setFields(editableProfile(result));
      void Taro.showToast({ title: kind === "save" ? "个人资料已保存并核对" : "头像已更新并核对", icon: "success" });
    } catch (cause: any) {
      if (mounted.current && !String(cause?.errMsg || "").includes("cancel")) setError(cause?.message || cause?.errMsg || "操作失败，请重新读取核对");
    } finally { locked.current = false; if (mounted.current) setBusy(""); }
  };
  const avatar = () => perform("avatar", async () => {
    const session = sessionRevision();
    const picked = await Taro.chooseImage({ count: 1, sizeType: ["compressed"], sourceType: ["album", "camera"] });
    const path = picked.tempFilePaths[0];
    if (session !== sessionRevision()) throw new Error("账号已切换，请重新选择头像");
    if (!path) throw new Error("未选择图片");
    if (picked.tempFiles?.[0]?.size > 2 * 1024 * 1024) throw new Error("头像不能超过 2MB");
    const encoded = await new Promise<string>((resolve, reject) => Taro.getFileSystemManager().readFile({ filePath: path, encoding: "base64", success: result => resolve(String(result.data)), fail: reject }));
    if (session !== sessionRevision()) throw new Error("账号已切换，已取消头像上传");
    const type = encoded.startsWith("iVBOR") ? "png" : encoded.startsWith("/9j/") ? "jpeg" : encoded.startsWith("UklGR") ? "webp" : "unsupported";
    return uploadProfileAvatar(`data:image/${type};base64,${encoded}`, member.username);
  });
  return <View className="mp-profile-editor">
    <View className="mp-card mp-edit-identity">
      <Button className="mp-edit-avatar-button" aria-label="更换头像" disabled={Boolean(busy)} onClick={() => void avatar()}>
        {member.avatarUrl ? <Image className="mp-edit-avatar-image" src={apiUrl(member.avatarUrl)} mode="aspectFill" /> : <Text>{String(member.displayName || member.username || "?").slice(0, 1).toUpperCase()}</Text>}
        <View className="mp-edit-camera"><AppIcon name="camera" size={15} /></View>
      </Button>
      <View className="mp-row-copy"><Text className="mp-row-title">{member.displayName || member.username}</Text><Text className="mp-row-description">@{member.username}</Text>{busy === "avatar" ? <Text className="mp-row-description">正在更新头像…</Text> : null}</View>
    </View>
    <View className="mp-card mp-edit-form">
      {([{ key: "displayName", label: "显示名称", max: 40 }, { key: "phone", label: "手机号", max: 32 }, { key: "email", label: "邮箱", max: 120 }, { key: "teamTitle", label: "团队职务", max: 60 }] as const).map(field => <View key={field.key} className="mp-edit-field">
        <Text className="mp-edit-label">{field.key === "displayName" ? <Text className="mp-edit-required">* </Text> : null}{field.label}</Text>
        <Input className="mp-input" aria-label={field.label} value={fields[field.key]} maxlength={field.max} disabled={Boolean(busy)} onInput={event => change(field.key, event.detail.value)} />
      </View>)}
      <View className="mp-edit-field"><Text className="mp-edit-label">个人简介</Text><Textarea className="mp-input mp-edit-bio" aria-label="个人简介" value={fields.bio} maxlength={240} disabled={Boolean(busy)} onInput={event => change("bio", event.detail.value)} /><Text className="mp-edit-count">{fields.bio.length} / 240</Text></View>
      {error ? <Text className="mp-error">{error}</Text> : null}
      <Button className="mp-primary mp-edit-save" disabled={Boolean(busy)} loading={busy === "save"} onClick={() => void perform("save", () => saveMemberProfile(fields, member.username))}>保存个人资料</Button>
    </View>
  </View>;
}
