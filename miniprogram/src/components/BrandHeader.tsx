import { Image, Text, View } from "@tarojs/components";
import Taro from "@tarojs/taro";
import brandLogoLight from "../assets/brand-logo-black.png";
import brandLogoDark from "../assets/brand-logo-white.png";
import AppIcon from "./AppIcon";

type BrandHeaderProps = {
  title: string;
  eyebrow?: string;
  showBack?: boolean;
  onBack?: () => void;
};

function navigationStyle() {
  const fallbackStatusBarHeight = 24;
  const fallbackHeaderHeight = 48;

  try {
    const statusBarHeight = Taro.getSystemInfoSync().statusBarHeight || fallbackStatusBarHeight;
    const capsule = Taro.getMenuButtonBoundingClientRect();
    const capsuleBottom = typeof capsule?.bottom === "number" ? capsule.bottom + 8 : 0;
    const minHeight = Math.max(statusBarHeight + fallbackHeaderHeight, capsuleBottom);

    return { minHeight: `${minHeight}px`, paddingTop: `${statusBarHeight}px` };
  } catch {
    return { minHeight: `${fallbackStatusBarHeight + fallbackHeaderHeight}px`, paddingTop: `${fallbackStatusBarHeight}px` };
  }
}

export async function returnFromHeader() {
  try {
    if (Taro.getCurrentPages().length > 1) {
      await Taro.navigateBack();
      return;
    }
  } catch {
    // If the host cannot provide a usable page stack, return to the primary Tab.
  }

  await Taro.switchTab({ url: "/pages/workspace/index" });
}

export default function BrandHeader({ title, showBack = false, onBack }: BrandHeaderProps) {
  return (
    <View className="mp-brand-header" style={navigationStyle()}>
      {showBack ? <View className="mp-brand-back" onClick={() => { if (onBack) onBack(); else void returnFromHeader(); }}><AppIcon name="arrow-left" size={20} /></View> : null}
      <View className="mp-brand-lockup">
        <Image className="mp-brand-logo mp-brand-logo-dark" src={brandLogoDark} mode="aspectFit" />
        <Image className="mp-brand-logo mp-brand-logo-light" src={brandLogoLight} mode="aspectFit" />
        <Text className="mp-brand-page-title">{title}</Text>
      </View>
    </View>
  );
}
