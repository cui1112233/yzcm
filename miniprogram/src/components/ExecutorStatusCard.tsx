import { Button, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { hasAccountSession } from "../services/session";
import { readExecutorSummary, type ExecutorSummary } from "../services/executors";

export default function ExecutorStatusCard() {
  const [summary, setSummary] = useState<ExecutorSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const version = useRef(0);

  const load = () => {
    if (!hasAccountSession()) return;
    const current = ++version.current;
    setLoading(true);
    setError("");
    void readExecutorSummary()
      .then((result) => { if (current === version.current) setSummary(result); })
      .catch((cause) => { if (current === version.current) { setSummary(null); setError(cause?.message || "执行器状态读取失败"); } })
      .finally(() => { if (current === version.current) setLoading(false); });
  };

  useEffect(() => { load(); return () => { version.current += 1; }; }, []);

  const state = loading ? "正在读取" : error ? "无法读取" : !summary ? "服务端未返回" : summary.onlineCount > 0 ? "在线" : summary.paired ? "离线" : "未配对";
  return <View className="mp-card">
    <View className="mp-row" style={{ minHeight: "0" }}>
      <Text className="mp-row-title">执行器</Text>
      <Text className="mp-row-description">{state}</Text>
    </View>
    {summary ? <Text className="mp-row-description">在线 {summary.onlineCount} 台 / 共 {summary.paired} 台</Text> : null}
    {error ? <Text className="mp-error">{error}</Text> : null}
    <Button className="mp-secondary" loading={loading} disabled={loading} onClick={load}>刷新状态</Button>
  </View>;
}
