import Taro, { useDidShow } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { readAccountSession, hasAccountSession } from "../../services/session";
import { createSessionGate, getSessionErrorMessage, resolveSessionGate } from "../../services/sessionGate";
import { apiRequest } from "../../services/api";
import { latestHistoryEntry, type HistoryEntry } from "../../services/normalizers";
import BrandHeader from "../../components/BrandHeader";
import WorkspaceHomeHeader from "../../components/WorkspaceHomeHeader";
import AppIcon, { type AppIconName } from "../../components/AppIcon";
import { getModule, moduleRoute, type ModuleKey } from "../../config/modules";
import { ThemedPage } from "../../theme";
import { isLocalAccountLogin } from "../../config/localAuth";

const ACTIVE_MODULE_KEY = "yzcm.mobile.activeModule";
const workspaceShortcuts: Array<{ key: ModuleKey; title: string; note: string; icon: AppIconName }> = [
  { key: "script", title: "剧本生成", note: "从小说到剧本与分镜", icon: "file-pen-line" },
  { key: "batch", title: "批量工厂", note: "导入、配置与批量制作", icon: "factory" },
  { key: "panel", title: "小说面板", note: "查看人物、场景和分镜", icon: "notebook-tabs" },
];

function historyTitle(entry: HistoryEntry) {
  return entry.preview || entry.title || "服务端未返回标题";
}

function historyTimestamp(entry: HistoryEntry) {
  return entry.createdAt || entry.created_at;
}

export default function WorkspacePage() {
  const localLogin = isLocalAccountLogin();
  const initialGate = createSessionGate(false);
  const [status, setStatus] = useState(initialGate.status);
  const [error, setError] = useState(initialGate.error);
  const readVersion = useRef(0);
  useEffect(() => () => { readVersion.current += 1; }, []);
  const [activeModule, setActiveModule] = useState(() => getModule(String(Taro.getStorageSync(ACTIVE_MODULE_KEY) || "script")));
  const [history, setHistory] = useState<HistoryEntry | undefined>(undefined);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState("");

  const loadHistory = () => {
    const version = readVersion.current;
    setHistoryLoading(true);
    setHistoryError("");
    void apiRequest<unknown>("/api/history")
      .then((payload) => { if (version === readVersion.current) setHistory(latestHistoryEntry(payload)); })
      .catch((cause) => {
        if (version !== readVersion.current) return;
        setHistory(undefined);
        setHistoryError(cause?.message || "历史记录加载失败");
      })
      .finally(() => { if (version === readVersion.current) setHistoryLoading(false); });
  };

  const connectSession = () => {
    const version = ++readVersion.current;
    setError("");
    setStatus("checking");
    setHistory(undefined);
    setActiveModule(getModule(String(Taro.getStorageSync(ACTIVE_MODULE_KEY) || "script")));
    if (!hasAccountSession()) {
      setStatus("binding-required");
      return;
    }
    void readAccountSession().then((session) => {
      if (version !== readVersion.current) return;
      const gate = resolveSessionGate(session);
      setStatus(gate.status);
      if (gate.status === "ready") loadHistory();
    }).catch((cause) => {
      if (version !== readVersion.current) return;
      const next = resolveSessionGate(cause);
      setError(getSessionErrorMessage(cause));
      setStatus(next.status);
    });
  };

  useDidShow(connectSession);

  const openModule = (key: ModuleKey) => {
    Taro.setStorageSync(ACTIVE_MODULE_KEY, key);
    setActiveModule(getModule(key));
    void Taro.navigateTo({ url: moduleRoute(key) });
  };

  return <ThemedPage className="mp-page mp-primary-page mp-workspace-home" animateTheme><BrandHeader title="工作区" />
    <WorkspaceHomeHeader />
    {status === "checking" ? <View className="mp-inline-state"><Text>正在同步你的创作工作台…</Text></View> : null}
    {status === "error" ? <View className="mp-card mp-session-state"><Text className="mp-row-title">暂时无法验证当前账号</Text><Text className="mp-error">{error || "登录验证失败，请重试"}</Text><Button className="mp-primary" onClick={connectSession}>重新连接</Button><Button className="mp-secondary" onClick={() => void Taro.navigateTo({ url: "/pages/bind/index" })}>账号登录</Button></View> : null}
    {status === "binding-required" ? <View className="mp-card mp-session-state"><Text className="mp-row-title">{localLogin ? "请登录本地账号" : "登录已有创作账号"}</Text><Text className="mp-row-description">{localLogin ? "登录后读取本地服务的创作内容。" : "使用网页版账号密码，同步作品、配置和任务。"}</Text><Button className="mp-primary" onClick={() => void Taro.navigateTo({ url: "/pages/bind/index" })}>账号登录</Button></View> : null}
    {status === "ready" ? <View className="mp-section"><View className="mp-section-head"><View><Text className="mp-section-kicker">QUICK START</Text><Text className="mp-section-title">快速开始</Text></View><View className="mp-section-link mp-section-link-action" onClick={() => Taro.switchTab({ url: "/pages/functions/index" })}><Text>全部功能</Text><AppIcon name="chevron-right" size={14} /></View></View><View className="mp-workspace-actions">{workspaceShortcuts.map((item) => <View className="mp-workspace-action liquid-glass--secondary" data-glass-role="secondary" key={item.key} onClick={() => openModule(item.key)}><View className="mp-workspace-action-icon"><AppIcon name={item.icon} size={22} /></View><View className="liquid-glass-copy"><Text className="mp-row-title">{item.title}</Text><Text className="mp-row-description">{item.note}</Text></View><AppIcon name="chevron-right" size={18} className="mp-chevron" /></View>)}</View>
      <View className="mp-section-head mp-workspace-recent-head"><View><Text className="mp-section-kicker">CONTINUE</Text><Text className="mp-section-title">继续工作</Text></View></View>
      {historyLoading ? <View className="mp-workspace-state"><Text className="mp-row-title">正在读取最近记录</Text><Text className="mp-row-description">正在读取服务端剧本历史。</Text></View> : null}
      {!historyLoading && historyError ? <View className="mp-workspace-state"><View><Text className="mp-row-title">历史记录暂不可读取</Text><Text className="mp-error">{historyError}</Text></View><Button className="mp-profile-retry" onClick={loadHistory}>重试</Button></View> : null}
      {!historyLoading && !historyError && history ? <View className="mp-workspace-current liquid-glass--secondary" data-glass-role="secondary" onClick={() => history.id ? void Taro.navigateTo({ url: `${moduleRoute("script")}&historyId=${encodeURIComponent(history.id)}` }) : setHistoryError("历史记录缺少 ID，无法恢复")}><View><View className="mp-status-dot"><View className="mp-status-indicator" /><Text>服务端最近记录</Text></View><Text className="mp-workspace-current-title">{historyTitle(history)}</Text>{historyTimestamp(history) ? <Text className="mp-row-description">{historyTimestamp(history)}</Text> : <Text className="mp-row-description">服务端未返回创建时间</Text>}</View><View className="mp-workspace-current-arrow"><Text>继续</Text><AppIcon name="chevron-right" size={15} /></View></View> : null}
      {!historyLoading && !historyError && !history ? <View className="mp-workspace-empty liquid-glass--secondary" data-glass-role="secondary"><Text className="mp-row-title">暂无可继续的服务端创作记录</Text><Text className="mp-row-description">创建剧本后，最近记录会显示在这里。</Text></View> : null}
      <View className="mp-workspace-local liquid-glass--secondary" data-glass-role="secondary" onClick={() => openModule(activeModule.key)}><View><Text className="mp-workspace-local-label">本机上次打开</Text><Text className="mp-workspace-current-title">{activeModule.label}</Text><Text className="mp-row-description">{activeModule.description}</Text></View><View className="mp-workspace-current-arrow"><Text>打开</Text><AppIcon name="chevron-right" size={15} /></View></View>
    </View> : null}</ThemedPage>;
}
