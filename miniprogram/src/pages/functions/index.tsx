import Taro, { useDidShow } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useState } from "react";
import BrandHeader from "../../components/BrandHeader";
import AppIcon from "../../components/AppIcon";
import { getModule, moduleRoute, MODULES, type ModuleKey } from "../../config/modules";
import { applyMobileTheme, readMobileTheme, ThemedPage } from "../../theme";
import "./functions-studio.scss";
import "./functions-glass.scss";

const ACTIVE_MODULE_KEY = "yzcm.mobile.activeModule";

const GROUPS = [
  { group: "创作", title: "从一个故事开始", caption: "写成剧本，画成漫画", number: "01" },
  { group: "生产", title: "让故事成为作品", caption: "内容管理与视频生产", number: "02" },
  { group: "助手", title: "创作搭档", caption: "多一点灵感与帮助", number: "03" },
] as const;
const COPY: Record<ModuleKey, string> = {
  script: "原文提取 · 剧本与分镜", fetch: "小说获取 · 改文与提交",
  comic: "粘贴正文，画风与画幅一次选好", panel: "原文、人物与分镜管理",
  shuihuo: "漫剧创作与批量生产",
  batch: "统一配置，执行或定时生产", agent: "与 AI 助手一起探索想法",
  toolkit: "会员中心、用量与独立配音",
};

export default function FunctionsPage() {
  const [activeKey, setActiveKey] = useState<ModuleKey>(() => getModule(String(Taro.getStorageSync(ACTIVE_MODULE_KEY) || "script")).key);
  useDidShow(() => {
    applyMobileTheme(readMobileTheme());
    setActiveKey(getModule(String(Taro.getStorageSync(ACTIVE_MODULE_KEY) || "script")).key);
  });

  const selectModule = (key: ModuleKey) => {
    Taro.setStorageSync(ACTIVE_MODULE_KEY, key);
    setActiveKey(key);
    void Taro.navigateTo({ url: moduleRoute(key) });
  };

  return (
    <ThemedPage className="mp-page mp-primary-page mp-functions-page functions-studio functions-glass">
      <BrandHeader title="功能" />
      <View className="tools-heading">
        <Text className="tools-eyebrow">CREATIVE STUDIO</Text>
        <Text className="tools-title">把灵感，变成作品。</Text>
        <Text className="tools-subtitle">从找到好故事，到完成下一部作品。</Text>
      </View>
      <View className="tools-glass-feature liquid-glass--secondary" data-glass-role="secondary">
        <Text className="tools-glass-feature-label">YOUR NEXT STORY</Text>
        <Text className="tools-glass-feature-title">好故事，从这里开始</Text>
        <Text className="tools-glass-feature-description">找到灵感，写下新的可能。</Text>
        <View className="tools-glass-feature-ring" aria-hidden="true" />
      </View>
      {GROUPS.map(section => (
        <View className={`tools-section tools-section-${section.number}`} key={section.group}>
          <View className="tools-section-heading">
            <Text className="tools-section-number">{section.number}</Text>
            <Text className="tools-section-title">{section.title}</Text>
            <Text className="tools-section-caption">{section.caption}</Text>
          </View>
          <View className="tools-entries">
          {MODULES.filter(item => item.group === section.group).map((item) => {
            const active = activeKey === item.key;
            return (
              <Button
                className={`tools-entry tools-entry-${item.key} ${item.key === "script" ? "liquid-glass--primary" : "liquid-glass--secondary"}${active ? " is-recent" : ""}`}
                data-glass-role={item.key === "script" ? "primary" : "secondary"}
                hoverClass="tools-entry-pressed"
                key={item.key}
                onClick={() => selectModule(item.key)}
              >
                <View className="tools-entry-icon">
                  <AppIcon name={item.icon} size={23} />
                </View>
                <View className="tools-entry-copy liquid-glass-copy">
                  <View className="tools-entry-titleline">
                    <Text className="tools-entry-title">{item.label}</Text>
                    {active ? <Text className="tools-recent">上次使用</Text> : null}
                  </View>
                  <Text className="tools-entry-description">{COPY[item.key]}</Text>
                </View>
                <View className="tools-entry-arrow">
                  <AppIcon name="chevron-right" size={16} />
                </View>
              </Button>
            );
          })}
          </View>
        </View>
      ))}
      <View className="tools-footnote"><View /><Text>好故事，值得被看见</Text><View /></View>
    </ThemedPage>
  );
}
