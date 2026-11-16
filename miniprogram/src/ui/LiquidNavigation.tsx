import Taro, { useDidShow } from '@tarojs/taro';
import { Button, Text, View } from '@tarojs/components';
import { useEffect, useRef, useState } from 'react';
import AppIcon, { type AppIconName } from '../components/AppIcon';
const tabs: { route: string; label: string; icon: AppIconName }[] = [
  { route: 'pages/workspace/index', label: '工作区', icon: 'home' },
  { route: 'pages/tasks/index', label: '任务', icon: 'folder-clock' },
  { route: 'pages/functions/index', label: '功能', icon: 'notebook-tabs' },
  { route: 'pages/profile/index', label: '我的', icon: 'user-round' },
];
function currentTab() {
  const pages = Taro.getCurrentPages?.() || [];
  const route = pages[pages.length - 1]?.route || '';
  return tabs.some(tab => tab.route === route) ? route : '';
}
export default function LiquidNavigation() {
  const [route, setRoute] = useState(currentTab);
  const [ready, setReady] = useState(false);
  const pending = useRef(false);
  const show = () => {
    const active = currentTab(); setRoute(active); pending.current = false;
    if (active) void Taro.hideTabBar({ animation: false }).then(() => setReady(true)).catch(() => setReady(false));
  };
  useDidShow(show);
  useEffect(show, []);
  // Non-tab routes never hide or duplicate native navigation.
  if (!route || !ready) return null;
  return <View className="liquid-navigation" ariaLabel="主导航">
    {tabs.map(tab => <Button className={`liquid-nav-item${tab.route === route ? ' is-selected' : ''}`} hoverClass="liquid-nav-pressed" ariaLabel={`${tab.label}${tab.route === route ? '，已选中' : ''}`} key={tab.route} onClick={() => {
      if (pending.current || tab.route === route) return;
      pending.current = true;
      void Taro.switchTab({ url: `/${tab.route}` }).catch(() => { void Taro.showToast({ title: '页面未能打开，请重试', icon: 'none' }); }).finally(() => { pending.current = false; });
    }}><AppIcon name={tab.icon} size={21} /><Text>{tab.label}</Text></Button>)}
  </View>;
}
