import { View } from "@tarojs/components";

/**
 * 吸底操作条是 position:fixed，webview 渲染下页面容器的 padding-bottom 不生效
 * （开发者工具会告警 scroll-view padding 被忽略）。这里用与吸底条等高的占位块垫在
 * 滚动内容末尾，保证最后一张卡不会被切掉。漫画分包所有带吸底条的页面共用。
 */
export const COMIC_STICKY_SPACER_HEIGHT = "calc(200rpx + env(safe-area-inset-bottom))";

export default function ComicStickySpacer() {
  return <View className="comic-sticky-spacer" aria-hidden="true" style={{ height: COMIC_STICKY_SPACER_HEIGHT }} />;
}
