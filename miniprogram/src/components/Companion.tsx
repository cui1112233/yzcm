import Taro, { useDidHide, useDidShow } from '@tarojs/taro';
import { Image, View, type ITouchEvent } from '@tarojs/components';
import { memo, useEffect, useRef, useState } from 'react';
import { latestFrame } from '../ui/latestFrame';
import { boundPosition, releasePose, edgePeekPosition } from './companionMotion';
import { cardDrop, type CardSide } from './companionCard';
import cardHead from '../assets/companion/card-head.png';
import idle from '../assets/companion/idle.png';
import walk from '../assets/companion/walk.png';
import sleep from '../assets/companion/sleep.png';
import think from '../assets/companion/think.png';
import candy from '../assets/companion/candy.png';
import dance from '../assets/companion/dance.png';
import peace from '../assets/companion/peace.png';
import peek from '../assets/companion/peek.png';
import './CompanionFrames.scss';
import './Companion.scss';

const sources = { idle, walk, sleep, think, candy, dance, peace, peek };
type Pose = keyof typeof sources;
const sequence: Pose[] = ['idle', 'walk', 'think', 'candy', 'dance', 'sleep'];
const walkDuration = 2400;
const labels: Record<Pose, string> = {idle:'小家伙陪你开始',walk:'小家伙在散步',sleep:'嘘，小家伙睡着了',think:'在想吃点什么呢',candy:'接住一颗糖！',dance:'一起摇一摇',peace:'耶，今天也加油',peek:'偷偷看你一眼'};

function Companion({ parked = false, preview = false }: { parked?: boolean; preview?: boolean }) {
  const [pose, setPose] = useState<Pose>('idle');
  const [x, setX] = useState(210);
  const [direction, setDirection] = useState(1);
  const [held, setHeld] = useState(false);
  const [visible, setVisible] = useState(true);
  const [reduced, setReduced] = useState(false);
  const [cardSide, setCardSide] = useState<CardSide | null>(null);
  const [dragY, setDragY] = useState(0);
  const [dragFrame] = useState(() => latestFrame<{x:number;y:number;direction:number}>(next => {
    setX(next.x);setDragY(next.y);setDirection(next.direction);
  }));
  const width = useRef(320);
  const position = useRef(210);
  const turn = useRef(0);
  const walkArrival = useRef<'idle' | 'peek'>('peek');
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const cardVisits = useRef(0);
  const press = useRef<{x:number; y:number; start:number; dy:number; moved:boolean} | null>(null);
  const clear = () => { if (timer.current) clearTimeout(timer.current); timer.current = undefined; };
  const move = (next: number) => {
    const target = boundPosition(next, width.current);
    setDirection(target >= position.current ? 1 : -1);
    position.current = target; setX(target);
  };
  useEffect(() => {
    try {const w = Taro.getSystemInfoSync().windowWidth; width.current = w * 680 / 750; move(width.current - 90);} catch { /* Host fallback stays within a 320px card. */ }
    // No platform-independent reduced-motion API is available in WeChat.
    // Persisted preference can be set by the host; CSS also honours the OS query.
    setReduced(Taro.getStorageSync('yzcm.mobile.reduceMotion') === true);
    return () => {clear();dragFrame.cancel();};
  }, []);
  useDidHide(() => {clear();dragFrame.cancel();setVisible(false);setHeld(false);setDragY(0);setPose(current => current === 'walk' ? 'idle' : current);press.current=null;});
  useDidShow(() => setVisible(true));
  useEffect(() => {if (parked) {dragFrame.cancel();setCardSide(null);setDragY(0);setHeld(false);setPose(current => current === 'walk' || current === 'peek' ? 'idle' : current);move(width.current - 90);press.current=null;}}, [parked]);
  useEffect(() => {
    clear();
    if (!visible || held || parked || reduced || preview) return;
    timer.current = setTimeout(() => {
      // End stepping at the same instant the position transition arrives.
      if (pose === 'walk') {setPose(walkArrival.current);return;}
      if (pose === 'peek') {walkArrival.current='idle';setPose('walk');move(width.current / 2);return;}
      if (cardSide) {walkArrival.current='idle';setCardSide(null);setPose('walk');move(width.current / 2);return;}
      const next = sequence[++turn.current % sequence.length]; setPose(next);
      if (next === 'idle') {
        const side: CardSide = (['top', 'left', 'top', 'right'] as const)[cardVisits.current++ % 4];
        setPose('idle');setCardSide(side);
        move(side === 'left' ? -8 : side === 'right' ? width.current - 64 : width.current / 2 - 36);
      }
      if (next === 'walk') {
        walkArrival.current='peek';
        move(edgePeekPosition(position.current > width.current / 2 ? 0 : width.current, width.current));
      }
    }, cardSide ? 8500 : pose === 'sleep' ? 18000 : pose === 'think' ? 14000 : pose === 'walk' ? walkDuration : 6500);
    return clear;
  }, [pose, visible, held, parked, reduced, cardSide, preview]);
  const finish = () => {
    if (!press.current) return;
    dragFrame.flush();
    const {moved, dy} = press.current; press.current = null; setHeld(false);setDragY(0);
    if (moved) {const side=cardDrop(position.current,dy,width.current);setCardSide(side);setPose(side ? 'idle' : releasePose(position.current, width.current));return;}
    if (cardSide) {setCardSide(null);setPose('peace');return;}
    setPose(pose === 'peace' ? 'candy' : 'peace');
  };
  const edgeDocked = !parked && !cardSide && !held && pose === 'peek';
  const displayX = edgeDocked ? edgePeekPosition(x, width.current) : x;
  return <View className={`home-companion-stage ${parked ? 'is-parked' : ''}`}>
    <View className={`home-companion ${held ? 'is-held' : ''} ${reduced || preview ? 'is-reduced' : ''} ${!visible ? 'is-hidden' : ''} ${cardSide ? `is-card card-${cardSide}` : ''}`} catchMove role="button" ariaLabel={cardSide ? '小家伙藏在状态卡后，扒着边缘探头，拖动可以拉出来' : `${labels[pose]}，点按打招呼，按住拖动`} style={{transform:`translate3d(${parked?0:displayX}px,${dragY}px,0)`, transitionDuration: held || edgeDocked ? '0ms' : `${walkDuration}ms`}}
      onTouchStart={e => {const touch=(e as ITouchEvent).touches?.[0];if(!touch)return;clear();position.current=displayX;setX(displayX);press.current={x:touch.clientX,y:touch.clientY,start:displayX,dy:0,moved:false};setHeld(true);}}
      onTouchMove={e => {const touch=(e as ITouchEvent).touches?.[0];if(!press.current || parked || !touch)return;const dx=touch.clientX-press.current.x,dy=touch.clientY-press.current.y;if(!press.current.moved&&Math.hypot(dx,dy)>5){press.current.moved=true;setCardSide(null);}if(Math.abs(dx)>5)setPose('walk');press.current.dy=dy;const target=boundPosition(press.current.start+dx,width.current),direction=target>=position.current?1:-1;position.current=target;dragFrame.push({x:target,y:Math.max(-70,Math.min(110,dy)),direction});}}
      onTouchEnd={finish} onTouchCancel={() => {dragFrame.cancel();press.current=null;setHeld(false);setDragY(0);setCardSide(null);setPose('idle');}}>
      {cardSide ? <View className="home-card-peek">
        <View className="home-card-head"><Image src={cardHead} mode="scaleToFill" /></View>
        <View className="home-card-paws"><View /><View /></View>
      </View> :
      <View className="home-companion-facing" style={{transform: `scaleX(${pose === 'walk' ? direction : pose === 'peek' && x < 8 ? -1 : 1})`}}>
        <View className="home-companion-window"><Image className={`home-companion-strip companion-${pose} ${!visible ? 'is-paused' : ''}`} src={sources[pose]} mode="scaleToFill" /></View>
      </View>}
    </View>
  </View>;
}

// Header scroll/clock updates must not rerender the sprite and touch handlers.
export default memo(Companion);
