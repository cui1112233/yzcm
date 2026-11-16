import { View } from "@tarojs/components";
import Taro from "@tarojs/taro";
import { useEffect, useState, type PropsWithChildren } from "react";

/** Reserve exactly the fixed dock height, including wrapped text and safe area. */
export default function OperationDock({ children }: PropsWithChildren) {
  const [height, setHeight] = useState(128);
  useEffect(() => {
    let active = true;
    const update = (value: number) => { if (active && Number.isFinite(value) && value > 0) setHeight(value); };
    const measure = () => {
      if (typeof Taro.createSelectorQuery === "function") {
        const query = Taro.createSelectorQuery();
        const page = Taro.getCurrentInstance?.().page;
        const scoped = page ? query.in(page) : query;
        scoped.select(".mp-batch-operation-dock").boundingClientRect(rect => {
          if (rect && !Array.isArray(rect)) update(rect.height);
        }).exec();
      } else if (typeof document !== "undefined") {
        update(document.querySelector(".mp-batch-operation-dock")?.getBoundingClientRect().height || 0);
      }
    };
    if (typeof Taro.nextTick === "function") Taro.nextTick(measure); else measure();
    Taro.onWindowResize?.(measure);
    return () => { active = false; Taro.offWindowResize?.(measure); };
  }, [children]);
  return <>
    <View className="mp-batch-dock-spacer" aria-hidden style={{ height: `${height + 16}px` }} />
    <View className="mp-batch-operation-dock">{children}</View>
  </>;
}
