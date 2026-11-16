import Taro, { useDidShow } from "@tarojs/taro";
import { View } from "@tarojs/components";
import { createContext, type CSSProperties, type PropsWithChildren, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from "react";
import { uiPalette, uiVariables } from "./ui/tokens";
import { nativeTabIcons } from "./ui/nativeTabIcons";
import { revealClipRadius, revealRadius } from './ui/themeReveal';
import './ui/themeReveal.scss';
import LiquidGlassMaterial from './ui/LiquidGlassMaterial';
import DynamicGlassCanvas from './ui/DynamicGlassCanvas';
import { GLASS_CHANGE, GLASS_PREFERENCE, liquidEnabled, liquidPageKey } from './ui/liquidGlass';
import './ui/liquidGlass.scss';

export const MOBILE_THEME_STORAGE_KEY = "yzcm.mobile.theme";

export type MobileTheme = "dark" | "light";
export type MobileThemePreference = MobileTheme | "system";

type ThemeContextValue = {
  theme: MobileTheme;
  preference: MobileThemePreference;
  setTheme: (theme: MobileThemePreference) => void;
  revealTheme?: (theme: MobileTheme, origin: {x:number;y:number;scroll:number}) => void;
  cancelReveal?: () => void;
  previewScroll?: number;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);
const themeSubscribers = new Set<(theme: MobileTheme) => void>();

export function readMobileThemePreference(): MobileThemePreference {
  const value = Taro.getStorageSync(MOBILE_THEME_STORAGE_KEY);
  return value === "light" || value === "system" ? value : "dark";
}
export function readMobileTheme(): MobileTheme {
  const preference = readMobileThemePreference();
  if (preference !== "system") return preference;
  try { return Taro.getAppBaseInfo?.().theme === "light" ? "light" : "dark"; }
  catch { return "dark"; }
}

export function themeClassName(theme: MobileTheme) {
  return `mp-theme mp-theme-${theme}`;
}

export function applyMobileTheme(theme: MobileTheme) {
  const light = theme === "light";
  const palette = uiPalette(theme);
  const backgroundColor = palette.canvas;
  void Taro.setNavigationBarColor({
    frontColor: light ? "#000000" : "#ffffff",
    backgroundColor,
  });
  void Taro.setBackgroundColor({ backgroundColorTop: backgroundColor, backgroundColorBottom: backgroundColor });
  // WeChat rejects tab-bar APIs on ordinary pages such as the batch factory.
  // Keep native chrome updates global, but scope tab-bar updates to actual tab roots.
  const tabRoutes = new Set(["pages/workspace/index", "pages/tasks/index", "pages/functions/index", "pages/profile/index"]);
  const pages = Taro.getCurrentPages?.() || [];
  const currentRoute = pages.length ? pages[pages.length - 1]?.route : undefined;
  if (currentRoute && tabRoutes.has(currentRoute)) {
    void Taro.setTabBarStyle?.({ color: palette.textMuted, selectedColor: palette.accent, backgroundColor: palette.surface, borderStyle: light ? "white" : "black" });
    nativeTabIcons[theme].forEach(([iconPath, selectedIconPath], index) => {
      void Taro.setTabBarItem?.({ index, iconPath, selectedIconPath });
    });
  }
}

export function saveMobileTheme(preference: MobileThemePreference) {
  Taro.setStorageSync(MOBILE_THEME_STORAGE_KEY, preference);
  const theme = preference === "system" ? readMobileTheme() : preference;
  applyMobileTheme(theme);
  themeSubscribers.forEach((subscriber) => subscriber(theme));
}

function subscribeToMobileTheme(subscriber: (theme: MobileTheme) => void) {
  themeSubscribers.add(subscriber);
  return () => {
    themeSubscribers.delete(subscriber);
  };
}

export function MobileThemeProvider({ children }: PropsWithChildren) {
  const value = useMobileTheme();
  return <ThemeContext.Provider value={value}><View className={themeClassName(value.theme)} style={uiVariables(value.theme) as CSSProperties}>{children}</View></ThemeContext.Provider>;
}

export function useMobileTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  const [theme, setTheme] = useState<MobileTheme>(readMobileTheme);
  const [preference, setPreference] = useState<MobileThemePreference>(readMobileThemePreference);

  useEffect(() => {
    // A themed preview must never update native chrome or subscribe a second time.
    if (context) return;
    applyMobileTheme(theme);
    const unsubscribe = subscribeToMobileTheme(value => { setTheme(value); setPreference(readMobileThemePreference()); });
    const onSystem = () => { if (readMobileThemePreference() === "system") { const value = readMobileTheme(); setTheme(value); applyMobileTheme(value); } };
    Taro.onThemeChange?.(onSystem);
    return () => { unsubscribe(); Taro.offThemeChange?.(onSystem); };
  }, []);

  const localValue = useMemo(() => ({ theme, preference, setTheme: saveMobileTheme }), [theme, preference]);
  return context || localValue;
}

export function ThemedPage({ className, variant, animateTheme = false, children }: PropsWithChildren<{ className: string; variant?: "studio"; animateTheme?: boolean }>) {
  const value = useMobileTheme();
  const liquid = liquidEnabled(className);
  const pageClasses = className.split(/\s+/);
  const nativeProfileTab = pageClasses.includes('mp-profile-page') && !liquid;
  const nativeTabRoot = pageClasses.includes('mp-profile-page') || pageClasses.includes('mp-primary-page');
  useDidShow(() => {
    if (nativeTabRoot) void Taro.showTabBar?.({ animation: false });
  });
  const [reducedGlass, setReducedGlass] = useState(() => Taro.getStorageSync(GLASS_PREFERENCE) === true);
  const refreshGlass = () => setReducedGlass(Taro.getStorageSync(GLASS_PREFERENCE) === true);
  useDidShow(refreshGlass);
  useEffect(() => { Taro.eventCenter?.on(GLASS_CHANGE, refreshGlass); return () => { Taro.eventCenter?.off(GLASS_CHANGE, refreshGlass); }; }, []);
  type Reveal = {next:MobileTheme;x:number;y:number;scroll:number;radius:number;ready:boolean};
  const [reveal,setReveal]=useState<Reveal|null>(null);
  const pending=useRef<Reveal|null>(null);
  const origin=useRef<{x:number;y:number}|null>(null);
  const timer=useRef<ReturnType<typeof setTimeout>>();
  const finish=()=>{
    const current=pending.current;if(!current)return;
    pending.current=null;clearTimeout(timer.current);value.setTheme(current.next);setReveal(null);
  };
  useEffect(()=>()=>{clearTimeout(timer.current);pending.current=null;},[]);
  const revealTheme=(next:MobileTheme,point:{x:number;y:number;scroll:number})=>{
    if(pending.current||next===value.theme)return;
    if(!animateTheme||Taro.getStorageSync('yzcm.mobile.reduceMotion')===true){value.setTheme(next);return;}
    try {
      const info=Taro.getSystemInfoSync();
      if(next==='dark'||!origin.current)origin.current={x:point.x,y:point.y};
      const center=origin.current!;
      const nextReveal={...center,next,scroll:point.scroll,radius:revealRadius(center.x,center.y,info.windowWidth,info.windowHeight),ready:false};
      pending.current=nextReveal;setReveal(nextReveal);
      // Match the reference: render the first clipped frame before starting the transition.
      Taro.nextTick(()=>setReveal(current=>current===nextReveal?{...current,ready:true}:current));
      // Fallback for platforms that omit transitionend; repeated taps stay locked.
      timer.current=setTimeout(finish,650);
    }catch{value.setTheme(next);}
  };
  const baseTheme=value.theme;
  const baseValue={...value,theme:baseTheme,revealTheme,cancelReveal:finish};
  const previewValue={...value,theme:reveal?.next ?? value.theme,setTheme:()=>{},previewScroll:reveal?.scroll};
  return <ThemeContext.Provider value={baseValue}>
    <View className={`${className} ${themeClassName(baseTheme)}${nativeProfileTab ? ' mp-profile-ordinary' : ''}${liquid ? ` mp-liquid${reducedGlass ? ' liquid-reduced' : ''}` : ''}`} style={uiVariables(baseTheme, variant) as CSSProperties}>
      {liquid ? <LiquidGlassMaterial theme={baseTheme} reduced={reducedGlass} /> : null}
      {liquid && !reducedGlass ? <DynamicGlassCanvas theme={baseTheme} reduced={false} pageKey={liquidPageKey(className)} /> : null}
      {children}
    </View>
    {reveal?<View id="mp-theme-reveal" className={`mp-theme-reveal${reveal.ready?' is-ready':''}`} aria-hidden="true" style={{'--reveal-x':`${reveal.x}px`,'--reveal-y':`${reveal.y}px`,'--reveal-radius':`${revealClipRadius(reveal.next,reveal.ready,reveal.radius)}px`} as CSSProperties} onTransitionEnd={e=>{if(e.target.id===e.currentTarget.id)finish();}} catchMove>
      <ThemeContext.Provider value={previewValue}><View className={`${className} mp-theme-reveal-page ${themeClassName(reveal.next)}${nativeProfileTab ? ' mp-profile-ordinary' : ''}${liquid ? ` mp-liquid${reducedGlass ? ' liquid-reduced' : ''}` : ''}`} style={{...uiVariables(reveal.next,variant),'--preview-scroll':`${reveal.scroll}px`} as CSSProperties}>
        {liquid ? <LiquidGlassMaterial theme={reveal.next} reduced={reducedGlass} /> : null}
        {liquid && !reducedGlass ? <DynamicGlassCanvas theme={reveal.next} reduced={false} pageKey={liquidPageKey(className)} /> : null}
        <View className="mp-theme-reveal-content" style={{transform:`translateY(${-reveal.scroll}px)`}}>{children}</View>
      </View></ThemeContext.Provider>
    </View>:null}
  </ThemeContext.Provider>;
}

export function withThemedPage<Props extends object = Record<string, never>>(Page: (props: Props) => ReactNode) {
  return function ThemedPageWrapper(props: Props) {
    return <ThemedPage className="mp-theme-page"><Page {...props} /></ThemedPage>;
  };
}
