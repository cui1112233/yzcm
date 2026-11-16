import type { AppIconName } from "../components/AppIcon";

export type ModuleKey = "script" | "fetch" | "comic" | "panel" | "shuihuo" | "agent" | "batch" | "toolkit";

export type CreatorModule = {
  key: ModuleKey;
  label: string;
  description: string;
  icon: AppIconName;
  group: "创作" | "生产" | "助手";
};

export const MODULES: CreatorModule[] = [
  { key: "script", label: "剧本生成", description: "小说 → 剧本 / 分镜", icon: "file-pen-line", group: "创作" },
  { key: "fetch", label: "小说获取", description: "获取 / 改文 / 提交", icon: "book-open", group: "创作" },
  { key: "comic", label: "漫画生成", description: "故事 → 资产 → 分镜 → 分页出图", icon: "sparkles", group: "创作" },
  { key: "panel", label: "小说面板", description: "原文 / 人物 / 分镜 / 结果", icon: "notebook-tabs", group: "生产" },
  { key: "shuihuo", label: "水货生产", description: "创作漫剧 / 批量生产", icon: "clapperboard", group: "生产" },
  { key: "agent", label: "Agent 工作区", description: "AI 创作助手", icon: "bot", group: "助手" },
  { key: "batch", label: "批量工厂", description: "统一配置 / 立即执行 / 定时生产", icon: "factory", group: "生产" },
  { key: "toolkit", label: "会员与工具", description: "会员中心 / 用量统计 / 独立配音", icon: "crown", group: "助手" },
];

export const DEFAULT_MODULE: ModuleKey = "script";

export function getModule(key: string) {
  return MODULES.find((item) => item.key === key) || MODULES[0];
}

export function isModuleKey(key: string): key is ModuleKey {
  return MODULES.some((item) => item.key === key);
}

export function moduleRoute(key: ModuleKey) {
  if (key === "toolkit") return "/comic/pages/index/index";
  if (key === "comic") return "/comic/pages/comic-new/index";
  return key === "batch" ? "/pages/batch/index" : `/pages/module-detail/index?key=${key}`;
}
