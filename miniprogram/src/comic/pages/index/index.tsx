import Taro from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import BrandHeader from "../../../components/BrandHeader";
import AppIcon, { type AppIconName } from "../../../components/AppIcon";
import { ThemedPage } from "../../../theme";
import "../../styles/comic.scss";

const TOOLS: Array<{ path: string; title: string; description: string; icon: AppIconName }> = [
  { path: "/comic/pages/member-center/index", title: "会员中心", description: "查看身份、团队和剩余额度", icon: "crown" },
  { path: "/comic/pages/usage-stats/index", title: "用量统计", description: "今天和这个月用了多少", icon: "chart" },
  { path: "/comic/pages/tts/index", title: "独立配音", description: "输入文字，生成一段语音", icon: "volume" },
];

export default function ComicToolkitPage() {
  return (
    <ThemedPage className="mp-page">
      <BrandHeader eyebrow="账号与工具" title="会员与工具" showBack />
      <Text className="mp-title">会员、额度与用量</Text>
      <Text className="mp-subtitle">查看身份和剩余额度，需要时也能单独把文字变成语音。</Text>

      {TOOLS.map((item) => (
        <Button
          key={item.path}
          className="mp-secondary"
          style={{ display: "flex", alignItems: "center", gap: "16rpx", textAlign: "left" }}
          onClick={() => { void Taro.navigateTo({ url: item.path }); }}
        >
          <AppIcon name={item.icon} size={22} />
          <View style={{ flex: 1, minWidth: 0 }}>
            <Text className="mp-row-title">{item.title}</Text>
            <Text className="mp-row-description">{item.description}</Text>
          </View>
          <AppIcon name="chevron-right" size={16} />
        </Button>
      ))}
    </ThemedPage>
  );
}
