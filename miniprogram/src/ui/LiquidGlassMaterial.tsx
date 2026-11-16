import { Image, View } from '@tarojs/components';
import darkWall from '../assets/glass/wallpaper-dark.png';
import lightWall from '../assets/glass/wallpaper-light.png';

// Pre-rendered wallpaper avoids a native full-screen canvas above WXML.
export default function LiquidGlassMaterial({ theme, reduced }: { theme: 'light' | 'dark'; reduced: boolean }) {
  return <View className={`liquid-wall liquid-wall-${theme}`} aria-hidden="true">
    {!reduced ? <Image src={theme === 'dark' ? darkWall : lightWall} mode="scaleToFill" /> : null}
  </View>;
}
