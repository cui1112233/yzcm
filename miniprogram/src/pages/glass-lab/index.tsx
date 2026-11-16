import { Canvas, Text, View } from '@tarojs/components';
import Taro, { useDidHide, useDidShow, usePageScroll } from '@tarojs/taro';
import { useEffect, useRef, useState } from 'react';
import LiquidGlassMaterial from '../../ui/LiquidGlassMaterial';
import { drawLens } from './refraction2d';
import './index.scss';

export default function GlassLab() {
  const [clicks,setClicks]=useState(0), [enabled,setEnabled]=useState(true), [error,setError]=useState('');
  const enabledRef=useRef(true), phase=useRef(0), scroll=useRef(0), touchX=useRef(0), active=useRef(true);
  const renderer=useRef<{request:()=>void;stop:()=>void}>();
  usePageScroll(e=>{scroll.current=e.scrollTop;renderer.current?.request();});
  useDidHide(()=>{active.current=false;renderer.current?.stop();});
  useDidShow(()=>{active.current=true;renderer.current?.request();});
  useEffect(()=>{
    let disposed=false;
    Taro.nextTick(()=>Taro.createSelectorQuery().select('#glass-lab-canvas').fields({node:true,size:true}).exec(results=>{
      if(disposed)return;
      try{
        const box=results?.[0]; if(!box?.node)throw new Error('未取得原生 Canvas 节点');
        const node=box.node,c=node.getContext('2d');
        node.width=Math.round(box.width);node.height=Math.round(box.height);
        const source=Taro.createOffscreenCanvas({type:'2d',width:node.width,height:node.height});
        const wall=source.getContext('2d') as any;
        if(!c||!wall)throw new Error('当前设备不支持同层 Canvas 2D');
        let frame:number|undefined;
        const draw=()=>{
          frame=undefined;if(disposed||!active.current)return;
          const w=node.width,h=node.height,g=wall.createLinearGradient(0,0,0,h);
          g.addColorStop(0,'#e5f2ed');g.addColorStop(1,'#70afbf');wall.fillStyle=g;wall.fillRect(0,0,w,h);
          const ribbon=(color:string,width:number)=>{
            wall.beginPath();for(let y=0;y<=h;y+=2){const x=w*(.30+.24*Math.sin((y-scroll.current)/h*2.4+phase.current));if(!y)wall.moveTo(x,y);else wall.lineTo(x,y);}
            wall.strokeStyle=color;wall.lineWidth=width;wall.stroke();
          };
          ribbon('#599bb2',48);ribbon('#e7fff4',21);c.drawImage(source,0,0);
          drawLens(c,source,{x:22,y:151,width:w-44,height:118},enabledRef.current);
        };
        const request=()=>{if(!disposed&&active.current&&frame===undefined)frame=node.requestAnimationFrame(draw);};
        const stop=()=>{if(frame!==undefined)node.cancelAnimationFrame(frame);frame=undefined;};
        renderer.current={request,stop};request();
      }catch(cause:any){setError(cause.message||'实时折射初始化失败');}
    }));
    return()=>{disposed=true;renderer.current?.stop();renderer.current=undefined;};
  },[]);
  return <View className="glass-lab">
    <View className="glass-lab-intro">实时折射 · 同层 Canvas 2D 验证</View>
    <LiquidGlassMaterial theme="light" reduced={false}/>
    <View className="glass-lab-stage">
      <Canvas type="2d" id="glass-lab-canvas" className="glass-lab-canvas"
        onTouchStart={e=>{touchX.current=e.touches[0]?.x||0;}}
        onTouchMove={e=>{const x=e.touches[0]?.x||0;phase.current+=(x-touchX.current)/150;touchX.current=x;renderer.current?.request();}}/>
      <View className="glass-lab-heading">文字应始终清晰可见</View>
      <View className="glass-lab-hint">左右拖动空白区域，观察卡片边缘光带</View>
      <View className="glass-lab-card" onClick={()=>setClicks(v=>v+1)}>
        <Text className="glass-lab-title">点击玻璃卡片</Text>
        <Text className="glass-lab-copy">{`已响应 ${clicks} 次 · 原生文字与点击`}</Text>
      </View>
    </View>
    <View className="glass-lab-controls"><View className="glass-lab-control" onClick={()=>{enabledRef.current=!enabledRef.current;setEnabled(enabledRef.current);renderer.current?.request();}}>{enabled?'关闭折射作对比':'开启实时折射'}</View></View>
    <Text className="glass-lab-note">{error||'验证装饰背景采样折射，不采样业务文字。每帧直接绘制，不导出图片。尚需真机预览验证性能。'}</Text>
    <View style={{height:'420px',padding:'24px'}}><Text>上下滑动检查折射同步。</Text></View>
  </View>;
}
