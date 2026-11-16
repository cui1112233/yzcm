import { Button, Text, View } from "@tarojs/components";
import Taro, { useDidShow } from "@tarojs/taro";
import { useEffect, useRef, useState } from "react";
import { API_ORIGIN_CONFIGURATION_ERROR, isApiOriginConfigured } from "../../config/runtime";
import { apiRequest } from "../../services/api";
import { deriveTaskPageState } from "../../services/taskPageState";
import { filterTaskEntries, loadTaskSummary, taskDestination, type AggregatedTask, type TaskFilter, type TaskSummary } from "../../services/taskSummary";
import BrandHeader from "../../components/BrandHeader";
import AmbientStarfield from "../../components/AmbientStarfield";
import AppIcon, { type AppIconName } from "../../components/AppIcon";
import { ThemedPage } from "../../theme";

const FILTERS: TaskFilter[] = ["全部", "进行中", "失败", "已完成"];

function statusLabel(entry: AggregatedTask) {
  if (entry.status === "running") return "进行中";
  if (entry.status === "failed") return "失败";
  if (entry.status === "completed") return "已完成";
  return entry.rawStatus ? `未识别：${entry.rawStatus}` : "状态未返回";
}

function formatTimestamp(value: string | undefined) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function taskIcon(entry: AggregatedTask): AppIconName {
  if (entry.source === "agent") return "bot";
  if (entry.source === "fetch") return "book-open";
  return "file-pen-line";
}

export default function TasksPage() {
  const [summary, setSummary] = useState<TaskSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [configurationError, setConfigurationError] = useState("");
  const [filter, setFilter] = useState<TaskFilter>("全部");
  const requestVersion = useRef(0);
  useEffect(() => () => { requestVersion.current += 1; }, []);

  const load = () => {
    const version = ++requestVersion.current;
    if (!isApiOriginConfigured()) {
      setConfigurationError(API_ORIGIN_CONFIGURATION_ERROR);
      setLoading(false);
      setRefreshing(false);
      return;
    }

    setConfigurationError("");
    setLoading(!summary);
    setRefreshing(Boolean(summary));
    void loadTaskSummary(apiRequest)
      .then((result) => { if (version === requestVersion.current) setSummary(result); })
      .catch(() => { if (version === requestVersion.current) setSummary(null); })
      .finally(() => {
        if (version !== requestVersion.current) return;
        setLoading(false);
        setRefreshing(false);
      });
  };

  useDidShow(load);

  const pageState = deriveTaskPageState({ loading, summary, configurationError });
  const filtered = summary ? filterTaskEntries(summary.entries, filter) : [];
  const failedSources = summary?.sources.filter((source) => source.state === "error") || [];

  return <ThemedPage className="mp-page mp-primary-page mp-tasks-page">
    <AmbientStarfield />
    <BrandHeader eyebrow="生产追踪台" title="任务" />
    <View className="mp-task-heading">
      <View><Text className="mp-task-heading-title">任务</Text><Text className="mp-task-heading-copy">剧本、Agent、小说获取、批量工厂与水货生产分别读取服务端状态。</Text></View>
      <Button className="mp-task-refresh" aria-label="刷新任务" loading={refreshing} disabled={loading} onClick={load}><AppIcon name="refresh-ccw" size={18} /></Button>
    </View>
    {pageState.kind === "configuration-error" ? <View className="mp-card mp-task-state"><Text className="mp-row-title">服务端未配置</Text><Text className="mp-error">{pageState.message}</Text></View> : null}
    {pageState.kind === "loading" ? <View className="mp-card mp-task-state"><Text className="mp-row-title">正在读取任务</Text><Text className="mp-row-description">正在分别读取各个真实服务端来源。</Text></View> : null}
    {pageState.kind === "all-error" ? <View className="mp-card mp-task-state"><Text className="mp-row-title">暂时无法读取任务</Text><Text className="mp-row-description">所有任务来源均未返回可展示数据。</Text><Button className="mp-primary" onClick={load}>重新读取</Button></View> : null}
    {summary && pageState.kind !== "configuration-error" && pageState.kind !== "all-error" ? <>
      <View className="mp-task-filters">{FILTERS.map((item) => <Text key={item} className={filter === item ? "mp-task-filter mp-task-filter-active" : "mp-task-filter"} onClick={() => setFilter(item)}>{item}</Text>)}</View>
      <View className="mp-section">
        <View className="mp-section-head"><View><Text className="mp-section-kicker">ACTIVITY</Text><Text className="mp-section-title">任务动态</Text></View><Text className="mp-section-link">{filtered.length} 条</Text></View>
        {failedSources.map((source) => <View className="mp-task-source-error" key={source.source}><View><Text className="mp-row-title">{source.sourceLabel} 暂不可用</Text><Text className="mp-row-description">{source.error}</Text></View><Button className="mp-task-source-retry" onClick={load}>重试</Button></View>)}
        <View className="mp-task-list liquid-glass--list">
          {filtered.map((entry, index) => <View className="mp-task-card mp-task-card-new" onClick={() => { void Taro.navigateTo({ url: taskDestination(entry) }); }} key={`${entry.source}-${entry.id || entry.title || index}`}><View className="mp-task-source-icon"><AppIcon name={taskIcon(entry)} size={20} /></View><View className="mp-row-copy"><Text className="mp-row-title">{entry.title || "服务端未返回标题"}</Text><Text className="mp-row-description">{entry.sourceLabel}{entry.createdAt ? ` · ${formatTimestamp(entry.createdAt)}` : ""}</Text>{entry.progress ? <Text className="mp-task-progress">视频 {entry.progress.completed}/{entry.progress.total}</Text> : null}</View><Text className={entry.status === "failed" ? "mp-status-pill mp-status-failed" : entry.status === "running" ? "mp-status-pill mp-status-running" : entry.status === "unknown" ? "mp-status-pill mp-status-unknown" : "mp-status-pill"}>{statusLabel(entry)}</Text></View>)}
          {pageState.kind === "empty" || (pageState.kind === "content" && !filtered.length) ? <View className="mp-task-empty"><AppIcon name="circle-dashed" size={28} className="mp-task-empty-icon" /><Text className="mp-row-title">{pageState.kind === "empty" ? "暂无真实任务记录" : "暂无符合筛选条件的真实任务"}</Text><Text className="mp-row-description">服务端返回新任务后会显示在这里。</Text></View> : null}
        </View>
      </View>
    </> : null}
  </ThemedPage>;
}
