import Taro, { useDidHide, useDidShow, usePageScroll } from '@tarojs/taro';
import { Image, Text, View } from '@tarojs/components';
import { useEffect, useRef, useState } from 'react';
import { useMobileTheme } from '../theme';
import Companion from './Companion';
import { greetingAt } from './companionMotion';
import art from '../assets/companion/home-art-glass.png';
import artDark from '../assets/companion/home-art-glass-dark.png';
import './WorkspaceHomeHeader.scss';
import { latestFrame } from '../ui/latestFrame';

export default function WorkspaceHomeHeader() {
  const { theme, setTheme, revealTheme, cancelReveal, previewScroll } = useMobileTheme();
  const [now, setNow] = useState(() => new Date());
  const [scroll, setScroll] = useState(0);
  const rawScroll = useRef(0);
  const headerScroll = useRef(0);
  const [scrollFrame] = useState(() => latestFrame<number>(setScroll));
  const interval = useRef<ReturnType<typeof setInterval>>();
  const [metrics] = useState(() => {
    try {const info=Taro.getSystemInfoSync(), capsule=Taro.getMenuButtonBoundingClientRect();return {nav:Math.max((info.statusBarHeight||24)+48,(capsule?.bottom||0)+8),scale:info.windowWidth/750};}
    catch {return {nav:72,scale:.5};}
  });
  const stop = () => {if(interval.current)clearInterval(interval.current);interval.current=undefined;scrollFrame.cancel();};
  const start = () => {stop();if(previewScroll!==undefined)return;const tick=()=>{const next=new Date();setNow(previous=>Math.floor(previous.getTime()/60000)===Math.floor(next.getTime()/60000)?previous:next);};tick();interval.current=setInterval(tick,15000);};
  useEffect(() => {start();return stop;}, []);
  useDidHide(()=>{stop();cancelReveal?.();}); useDidShow(start);
  usePageScroll(e=>{
    if(previewScroll!==undefined)return;
    rawScroll.current=e.scrollTop;
    // Once collapsed, further document scrolling cannot change the header.
    const next=Math.min(Math.max(0,e.scrollTop),374*metrics.scale);
    if(next!==headerScroll.current){headerScroll.current=next;scrollFrame.push(next);}
  });
  const currentScroll=previewScroll??scroll;
  const p = Math.min(1, Math.max(0, currentScroll / (374*metrics.scale)));
  const switchTheme=()=>{
    const next=theme==='dark'?'light':'dark';
    if(!revealTheme){setTheme(next);return;}
    Taro.createSelectorQuery().select('.home-theme-switch').boundingClientRect(rect=>{
      if(!rect||Array.isArray(rect)){setTheme(next);return;}
      revealTheme(next,{x:rect.left+rect.width/2,y:rect.top+rect.height/2,scroll:rawScroll.current});
    }).exec();
  };
  const time = `${String(now.getHours()).padStart(2,'0')}:${String(now.getMinutes()).padStart(2,'0')}`;
  const date = `${now.getMonth()+1}月${now.getDate()}日 星期${'日一二三四五六'[now.getDay()]}`;
  return <>
    <View className="home-hero" style={{top:`${metrics.nav}px`,clipPath:`inset(0 0 ${p*374}rpx 0)`}}>
      <Image className="home-hero-art" src={theme==='dark'?artDark:art} mode="scaleToFill" />
      <View className="home-hero-copy" style={{opacity:Math.max(0,1-p*1.6),transform:`translateY(${-p*20}px)`}}>
        <Text className="home-eyebrow">小小灵感 · 大大创作</Text>
        <Text className="home-headline">把文字，{ '\n' }变成下一幕。</Text>
        <Text className="home-caption">今天，也和小家伙一起开工。</Text>
      </View>
      <View className="home-theme-switch" role="button" ariaLabel={theme==='dark'?'切换浅色模式':'切换深色模式'} onClick={switchTheme}><Text>{theme==='dark'?'☾':'☀'}</Text></View>
    </View>
    <View className={`home-status-card liquid-glass--primary ${p===1?'is-stuck':''}`} data-glass-role="primary">
      <View><Text className="home-clock">{time}</Text><Text className="home-date">{date}</Text></View>
      <View className="home-greeting"><Text>{greetingAt(now.getHours())}</Text><Text className="home-companion-caption">小家伙陪你开始</Text></View>
      <Companion parked={p>.8} preview={previewScroll!==undefined} />
    </View>
  </>;
}
