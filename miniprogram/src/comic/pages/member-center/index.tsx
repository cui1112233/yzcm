import { useDidShow } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import { formatTokenAmount } from "../../../config/profileData";
import { ThemedPage } from "../../../theme";
import { readMemberCenterView, type MemberCenterView } from "../../services/memberCenter";

function display(value: string | number | null | undefined) {
  if (value === null) return "不限额";
  if (value === undefined) return "服务端未返回";
  return String(value);
}

export default function MemberCenterPage() {
  const [view, setView] = useState<MemberCenterView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const version = useRef(0);

  const load = () => {
    const current = ++version.current;
    setLoading(true);
    setError("");
    void readMemberCenterView()
      .then((result) => { if (current === version.current) setView(result); })
      .catch((cause) => { if (current === version.current) { setView(null); setError(cause?.message || "会员资料读取失败"); } })
      .finally(() => { if (current === version.current) setLoading(false); });
  };

  useDidShow(load);
  useEffect(() => { load(); return () => { version.current += 1; }; }, []);

  const facts = view?.facts;
  const role = facts?.role ? facts.role.toUpperCase() : "服务端未返回";
  const service = facts?.apiEnabled === true ? "已启用" : facts?.apiEnabled === false ? "未启用" : "服务端未返回";
  const limit = view?.quota.limit;

  return (
    <ThemedPage className="mp-page">
      <BrandHeader eyebrow="会员" title="会员中心" showBack />
      <Text className="mp-title">会员中心</Text>
      <Text className="mp-subtitle">只读展示当前账号的服务端数据。</Text>
      {loading ? <View className="mp-card"><Text className="mp-row-title">正在读取会员资料</Text><Text className="mp-row-description">最多等待 15 秒。</Text></View> : null}
      {!loading && error ? <View className="mp-card"><Text className="mp-row-title">无法读取会员资料</Text><Text className="mp-error">{error}</Text><Button className="mp-primary" onClick={load}>重新读取</Button></View> : null}
      {!loading && view ? <>
        <View className="mp-card">
          <Text className="mp-row-title">身份</Text>
          <Text className="mp-row-description">名称：{display(facts?.displayName)}</Text>
          <Text className="mp-row-description">账号：{facts?.username ? `@${facts.username}` : "服务端未返回"}</Text>
          <Text className="mp-row-description">角色：{role}</Text>
          <Text className="mp-row-description">团队归属：{display(facts?.teamName)}</Text>
        </View>
        <View className="mp-card">
          <Text className="mp-row-title">额度</Text>
          <Text className="mp-row-description">本月已用：{view.quota.used === undefined ? "服务端未返回" : `${formatTokenAmount(view.quota.used)} Token`}</Text>
          <Text className="mp-row-description">月度额度：{limit === undefined ? "服务端未返回" : formatTokenAmount(limit)}</Text>
          <Text className="mp-row-description">{view.quota.percent === undefined ? "额度占比：服务端未返回" : `额度占比：${view.quota.percent}%`}</Text>
        </View>
        <View className="mp-card">
          <Text className="mp-row-title">AI 服务</Text>
          <Text className="mp-row-description">状态：{service}</Text>
          <Text className="mp-row-description">授权能力：{facts?.scopes === undefined ? "服务端未返回" : facts.scopes.length ? facts.scopes.join("、") : "服务端返回空范围"}</Text>
        </View>
      </> : null}
    </ThemedPage>
  );
}
