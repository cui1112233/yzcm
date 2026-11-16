import Taro, { useDidShow, useLoad } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../components/BrandHeader";
import AmbientStarfield from "../../components/AmbientStarfield";
import { profileFacts, type PublicMemberCenter } from "../../config/profileData";
import { apiRequest } from "../../services/api";
import { ThemedPage, useMobileTheme, type MobileThemePreference } from "../../theme";
import AccountManagementView from "./AccountManagementView";
import ProfileEditor from "./ProfileEditor";
import type { AccountSection } from "../../services/memberAccount";
import { resolveCreatorLevel } from "../../services/creatorLevel";
import ManagedAccountsPanel from "./ManagedAccountsPanel";
import TeamGovernancePanel from "./TeamGovernancePanel";
import { apiUrl } from "../../config/runtime";

type ModelGroups = Record<"text" | "image" | "video", any[] | undefined>;

const TITLES: Record<string, string> = {
  profile: "个人资料",
  memberCenter: "会员中心",
  apiConfig: "API 配置",
  usage: "用量与制作",
  appearance: "主题与外观",
  general: "通用设置",
  level: "我的等级",
  security: "账号安全",
  governance: "联合治理",
  team: "组员管理",
  accounts: "账号与角色",
  adminConsole: "管理后台",
};

const SERVER_DETAIL_KEYS = new Set(["profile", "memberCenter", "apiConfig", "usage", "level"]);
const LOCAL_DETAIL_KEYS = new Set(["appearance", "general"]);

function displayFact(value: string | number | null | undefined) {
  return value === null ? "不限额" : value === undefined ? "服务端未返回" : String(value);
}

function modelEntries(input: any): any[] | undefined {
  if (Array.isArray(input)) return input;
  if (Array.isArray(input?.models)) return input.models;
  if (Array.isArray(input?.entries)) return input.entries;
  return undefined;
}

export default function ProfileDetailPage() {
  const [key, setKey] = useState("loading");
  const version = useRef(0);
  const [data, setData] = useState<PublicMemberCenter | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const { theme, preference, setTheme } = useMobileTheme();
  const [models, setModels] = useState<ModelGroups>({ text: undefined, image: undefined, video: undefined });
  const [modelsError, setModelsError] = useState("");
  const [apiConfig, setApiConfig] = useState<{ canManageApi?: boolean; managedBy?: string } | null>(null);

  useLoad((params) => setKey(params?.key || "profile"));

  const supportedServerDetail = SERVER_DETAIL_KEYS.has(key);
  const localDetail = LOCAL_DETAIL_KEYS.has(key);
  const load = () => {
    const current = ++version.current;
    if (!supportedServerDetail) {
      setLoading(false);
      setError("");
      return;
    }

    setLoading(true);
    setError("");
    setModelsError("");
    void apiRequest<PublicMemberCenter>("/api/member/me")
      .then(async (result) => {
        if (current !== version.current) return;
        setData(result);
        if (key !== "apiConfig") return;
        const settled = await Promise.allSettled([
          apiRequest<any>("/api/models?kind=text"),
          apiRequest<any>("/api/models?kind=image"),
          apiRequest<any>("/api/shuihuo-production/models"),
          apiRequest<any>("/api/config"),
        ]);
        const groups: ModelGroups = { text: undefined, image: undefined, video: undefined };
        if (current !== version.current) return;
        (["text", "image", "video"] as const).forEach((kind, index) => {
          const response = settled[index];
          if (response.status === "fulfilled") groups[kind] = modelEntries(response.value);
        });
        setModels(groups);
        const config = settled[3];
        setApiConfig(config.status === "fulfilled" ? { canManageApi: config.value?.canManageApi, managedBy: config.value?.managedBy } : null);
        if (settled.some((response) => response.status === "rejected")) setModelsError("部分模型目录暂不可用");
      })
      .catch((cause) => { if (current === version.current) setError(cause?.message || "账号资料加载失败"); })
      .finally(() => { if (current === version.current) setLoading(false); });
  };

  useDidShow(load);
  useEffect(() => { load(); return () => { version.current += 1; }; }, [key]);

  const title = TITLES[key] || "个人中心";
  const facts = profileFacts(data);
  const level = resolveCreatorLevel(data);
  const memberAvailable = Boolean(data?.member && typeof data.member === "object" && !Array.isArray(data.member));
  const saveTheme = (next: MobileThemePreference) => { setTheme(next); Taro.showToast({ title: "设置已保存", icon: "success" }); };

  const serverLoading = supportedServerDetail && loading;
  const serverUnavailable = !loading && supportedServerDetail && !memberAvailable;
  return <ThemedPage className="mp-page mp-detail-page mp-profile-detail-page"><AmbientStarfield /><BrandHeader eyebrow="个人中心" title={title} showBack />
    <View className="mp-detail-content">
      {key === "security" ? <AccountManagementView section={key as AccountSection} /> : null}
      {key === "governance" ? <TeamGovernancePanel /> : null}
      {key === "team" || key === "accounts" ? <ManagedAccountsPanel key={key} section={key} /> : null}
      {key === "adminConsole" ? <View className="mp-card"><Text className="mp-row-title">移动账号管理</Text><Button className="mp-primary" onClick={() => Taro.navigateTo({ url: "/pages/profile-detail/index?key=accounts" })}>账号与角色</Button><Button className="mp-secondary" onClick={() => Taro.navigateTo({ url: "/pages/profile-detail/index?key=governance" })}>团队治理</Button><Text className="mp-row-description">系统预设发布、审批和服务配置仍在网页版管理后台操作。</Text></View> : null}
      {serverLoading ? <View className="mp-card mp-detail-state"><Text className="mp-row-title">正在读取资料</Text><Text className="mp-row-description">当前页面只展示服务端返回的数据。</Text></View> : null}
      {serverUnavailable ? <View className="mp-card mp-detail-state"><Text className="mp-row-title">无法读取账号资料</Text><Text className="mp-error">{error || "服务端未返回账号资料"}</Text><Button className="mp-primary" onClick={load}>重新读取</Button></View> : null}
      {memberAvailable && !serverLoading && key === "profile" ? <ProfileEditor key={data?.member?.username} center={data!} onSaved={setData} /> : null}
      {memberAvailable && (key === "memberCenter" || key === "usage") ? <>
        <Text className="mp-title">{title}</Text><Text className="mp-subtitle">数据来自当前账号中心。</Text>
        <View className="mp-card liquid-glass--secondary" data-glass-role="secondary">
          {key === "memberCenter" ? <><Text className="mp-row-title">账号</Text><Text className="mp-row-description">{facts.username ? `@${facts.username}` : "服务端未返回"}</Text><Text className="mp-row-title mp-detail-gap">当前角色</Text><Text className="mp-row-description">{displayFact(facts.role)}</Text><Text className="mp-row-title mp-detail-gap">所属团队</Text><Text className="mp-row-description">{displayFact(facts.teamName)}</Text></> : <><Text className="mp-row-title">本月 Token</Text><Text className="mp-row-description">{displayFact(facts.monthlyTokenUsage)}</Text><Text className="mp-row-title mp-detail-gap">月度额度</Text><Text className="mp-row-description">{displayFact(facts.monthlyTokenLimit)}</Text></>}
        </View>
        <View className="mp-card"><Text className="mp-row-title">本月消耗 · {displayFact(data?.usage?.month?.totalTokens)} Token</Text><Text className="mp-row-description">AI 服务：{["dev", "manager"].includes(String(facts.role).toLowerCase()) || facts.apiEnabled === true ? "已启用" : facts.apiEnabled === false ? "未授权" : "服务端未返回"}</Text><Text className="mp-row-description">月度额度：{displayFact(facts.monthlyTokenLimit)}</Text></View>
        {key === "usage" ? <><View className="mp-card"><Text className="mp-row-title">今日用量</Text><Text className="mp-row-description">{displayFact(data?.usage?.day?.totalTokens)} Token · {displayFact(data?.usage?.day?.calls)} 次调用</Text><Text className="mp-row-description">本月调用：{displayFact(data?.usage?.month?.calls)} 次 · 输出 {displayFact(data?.usage?.month?.outputTokens)} Token</Text></View><View className="mp-card"><Text className="mp-row-title">最近调用</Text>{Array.isArray(data?.usage?.recent) ? data.usage.recent.length ? data.usage.recent.slice(0, 12).map((item, index) => <View className="mp-row mp-setting-row" key={item.id || index}><Text>{item.feature || "模型调用"} · {item.model || "默认模型"}</Text><Text>{displayFact(item.totalTokens)} Token</Text></View>) : <Text>暂无最近调用</Text> : <Text>服务端未返回调用记录</Text>}</View></> : null}
      </> : null}
      {memberAvailable && key === "level" ? <View className="mp-card"><Text className="mp-row-title">{level ? `Lv.${level.level} · ${level.name}` : "服务端未返回等级或用量"}</Text>{level ? <><Text className="mp-row-description">成长值 {level.current} · {level.percent}%</Text><Text className="mp-row-description">{level.source === "backend" ? "等级数据来自公网账号" : "成长值按公网本月 Token 活跃度展示"}</Text></> : null}<Text className="mp-row-description">模型权限：{facts.scopes?.join("、") || "按账号授权"}</Text></View> : null}
      {memberAvailable && key === "apiConfig" ? <>
        <Text className="mp-title">API 配置</Text><Text className="mp-subtitle">权限和模型目录以当前账号服务端返回为准。</Text>
        <View className="mp-card"><Text className="mp-row-title">{apiConfig ? apiConfig.canManageApi === true ? "当前账号可管理模型服务" : "模型服务由团队托管" : "服务端未返回托管状态"}</Text><Text className="mp-row-description">托管账号：{apiConfig?.managedBy ? `@${apiConfig.managedBy}` : "服务端未返回"}</Text><Text className="mp-row-description">密钥和复杂模型配置使用公网完整配置页。</Text><Button onClick={() => Taro.setClipboardData({ data: apiUrl("/api-config") })}>复制公网 API 配置链接</Button></View>
        <View className="mp-card"><Text className="mp-row-title">当前角色</Text><Text className="mp-row-description">{displayFact(facts.role?.toUpperCase())}</Text><Text className="mp-row-title mp-detail-gap">AI 服务</Text><Text className="mp-row-description">{facts.apiEnabled === true ? "已启用" : facts.apiEnabled === false ? "未启用" : "服务端未返回"}</Text><Text className="mp-row-title mp-detail-gap">授权能力</Text><Text className="mp-row-description">{facts.scopes === undefined ? "服务端未返回" : facts.scopes.length ? facts.scopes.join("、") : "服务端返回空范围"}</Text></View>
        <Text className="mp-section-caption">当前账号模型目录</Text>
        {modelsError ? <Text className="mp-error">{modelsError}</Text> : null}
        {([['text', '文本模型'], ['image', '图片模型'], ['video', '视频模型']] as const).map(([kind, label]) => <View className="mp-card mp-model-card" key={kind}><Text className="mp-row-title">{label}</Text>{models[kind] === undefined ? <Text className="mp-row-description">服务端未返回模型目录</Text> : models[kind].length ? models[kind].map((model: any, index) => <Text className="mp-row-description" key={model?.id || model?.key || index}>{model?.name || model?.label || model?.displayName || model?.id || model?.key || "服务端未返回模型名称"} · {model?.enabled === true ? "可用" : model?.enabled === false ? "已停用" : "服务端未返回状态"}</Text>) : <Text className="mp-row-description">服务端返回空列表</Text>}</View>)}
      </> : null}
      {localDetail && key === "appearance" ? <><Text className="mp-title">主题与外观</Text><Text className="mp-subtitle">仅保存在当前设备。</Text><View className="mp-card"><Text className="mp-row-title">当前主题：{theme === "light" ? "浅色" : "深色"}{preference === "system" ? " · 跟随系统" : ""}</Text>{(["dark", "light", "system"] as const).map(value => <Button key={value} className={preference === value ? "mp-primary" : "mp-secondary"} onClick={() => saveTheme(value)}>{value === "system" ? "跟随系统" : value === "light" ? "浅色" : "深色"}</Button>)}</View></> : null}
      {localDetail && key === "general" ? <><Text className="mp-title">通用设置</Text><Text className="mp-subtitle">仅保存在当前设备。</Text><View className="mp-card"><Text className="mp-row-title">移动端显示与交互偏好</Text><Text className="mp-row-description">此页不展示或修改服务端账号、权限和接口配置。</Text></View></> : null}
      {memberAvailable && error ? <View className="mp-detail-error"><Text className="mp-error">{error}</Text><Button className="mp-primary" onClick={load}>重新读取</Button></View> : null}
    </View>
  </ThemedPage>;
}
