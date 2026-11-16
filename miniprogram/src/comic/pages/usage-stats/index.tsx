import { useDidShow } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import { formatTokenAmount } from "../../../config/profileData";
import { ThemedPage } from "../../../theme";
import { readUsageStats, type UsageStatsView } from "../../services/usageStats";

function tokens(value: number | undefined) {
  return value === undefined ? "服务端未返回" : `${formatTokenAmount(value)} Token`;
}

function calls(value: number | undefined) {
  return value === undefined ? "服务端未返回" : `${formatTokenAmount(value)} 次`;
}

// 数字卡直接内联渲染，避免测试与真机对子组件展开方式的差异。
function metricStyle() { return { flex: "1 1 45%", margin: "0" }; }

export default function UsageStatsPage() {
  const [view, setView] = useState<UsageStatsView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const version = useRef(0);

  const load = () => {
    const current = ++version.current;
    setLoading(true);
    setError("");
    void readUsageStats()
      .then((result) => { if (current === version.current) setView(result); })
      .catch((cause) => { if (current === version.current) { setView(null); setError(cause?.message || "用量读取失败"); } })
      .finally(() => { if (current === version.current) setLoading(false); });
  };

  useDidShow(load);
  useEffect(() => { load(); return () => { version.current += 1; }; }, []);

  return (
    <ThemedPage className="mp-page">
      <BrandHeader eyebrow="统计" title="用量与制作" showBack />
      <Text className="mp-title">用量与制作</Text>
      <Text className="mp-subtitle">只读展示账号中心返回的 Token 与调用记录。</Text>
      {loading ? <View className="mp-card"><Text className="mp-row-title">正在读取用量</Text><Text className="mp-row-description">最多等待 15 秒。</Text></View> : null}
      {!loading && error ? <View className="mp-card"><Text className="mp-row-title">无法读取用量</Text><Text className="mp-error">{error}</Text><Button className="mp-primary" onClick={load}>重新读取</Button></View> : null}
      {!loading && view ? <>
        <Text className="mp-section-caption">今日</Text>
        <View style={{ display: "flex", flexWrap: "wrap", gap: "12px" }}>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">Token</Text><Text className="mp-row-title">{tokens(view.day.totalTokens)}</Text></View>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">调用次数</Text><Text className="mp-row-title">{calls(view.day.calls)}</Text></View>
        </View>
        <Text className="mp-section-caption">本月</Text>
        <View style={{ display: "flex", flexWrap: "wrap", gap: "12px" }}>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">Token</Text><Text className="mp-row-title">{tokens(view.month.totalTokens)}</Text></View>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">调用次数</Text><Text className="mp-row-title">{calls(view.month.calls)}</Text></View>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">输出 Token</Text><Text className="mp-row-title">{tokens(view.month.outputTokens)}</Text></View>
          <View className="mp-card" style={metricStyle()}><Text className="mp-row-description">预估费用</Text><Text className="mp-row-title">{view.month.estimatedCost === undefined ? "服务端未返回" : `${formatTokenAmount(view.month.estimatedCost)} ${view.month.costCurrency || ""}`.trim()}</Text></View>
        </View>
        <View className="mp-card">
          <Text className="mp-row-title">最近调用</Text>
          {view.recent.length ? view.recent.slice(0, 20).map((item, index) => (
            <View className="mp-row" key={item.id || index} style={{ minHeight: "56px" }}>
              <View className="mp-row-copy">
                <Text className="mp-row-title">{item.feature || "模型调用"} · {item.model || "默认模型"}</Text>
                <Text className="mp-row-description">{item.createdAt || "服务端未返回时间"}</Text>
              </View>
              <Text className="mp-row-description">{item.totalTokens === undefined ? "服务端未返回" : `${formatTokenAmount(item.totalTokens)} Token`}</Text>
            </View>
          )) : <Text className="mp-row-description">暂无最近调用</Text>}
        </View>
      </> : null}
    </ThemedPage>
  );
}
