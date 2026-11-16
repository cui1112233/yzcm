import Taro, { useDidHide, useDidShow, usePageScroll } from '@tarojs/taro';
import { Canvas, Image, View } from '@tarojs/components';
import { useEffect, useRef, useState } from 'react';
import { useMobileTheme } from '../../theme';
import { refractGlass, type GlassRect, type PixelBuffer } from './glassOptics';

type NativeCanvas = { width: number; height: number; getContext: (type: '2d') => CanvasRenderingContext2D };
type CanvasField = GlassRect & { node?: NativeCanvas };
type Renderer = { wallpaper: PixelBuffer; canvas: NativeCanvas; rect: GlassRect; scale: number };

function paintWallpaper(ctx: CanvasRenderingContext2D, width: number, height: number, dark: boolean) {
  const gradient = ctx.createLinearGradient(0, 0, width, height);
  (dark ? ['#193746', '#285668', '#122f46'] : ['#eaf4f0', '#bfdfdf', '#78aebf']).forEach((color, i) => gradient.addColorStop(i / 2, color));
  ctx.fillStyle = gradient; ctx.fillRect(0, 0, width, height);
  ctx.save(); ctx.translate(-width * .7, height * .16); ctx.rotate(-.38);
  const colors = dark ? ['#31586f', '#88bbbf', '#427387'] : ['#77a5b8', '#effff3', '#96cbd1'];
  colors.forEach((color, i) => {
    ctx.beginPath(); ctx.ellipse(width * .15, height * .4, width * (.82 + i * .12), height * 1.07, 0, 0, Math.PI * 2);
    ctx.strokeStyle = color; ctx.lineWidth = width * .075; ctx.stroke();
  });
  ctx.restore();
  const glow = ctx.createRadialGradient(width * .15, height * .12, 0, width * .15, height * .12, width * 1.4);
  glow.addColorStop(0, dark ? 'rgba(224,255,255,.08)' : 'rgba(255,255,255,.42)'); glow.addColorStop(1, 'rgba(255,255,255,0)');
  ctx.fillStyle = glow; ctx.fillRect(0, 0, width, height);
}

/** One native lens; no perpetual animation loop or React updates while scrolling. */
export default function NativeGlassMaterial({ reduced, onLensChange }: { reduced: boolean; onLensChange: (path: string) => void }) {
  const { theme } = useMobileTheme();
  const [wallSource, setWallSource] = useState('');
  const temporary = useRef({ wall: [] as string[], lens: [] as string[] });
  const discard = (path: string) => { try { Taro.getFileSystemManager().unlink({ filePath: path, fail: () => {} }); } catch {} };
  const remember = (kind: 'wall' | 'lens', path: string) => {
    const paths = temporary.current[kind]; paths.push(path);
    // Retain the displayed image and its predecessor, not a file for every scroll.
    while (paths.length > 2) discard(paths.shift()!);
  };
  useEffect(() => () => {
    [...temporary.current.wall, ...temporary.current.lens].forEach(discard);
    temporary.current = { wall: [], lens: [] };
  }, []);
  const renderer = useRef<Renderer>();
  const lensVisible = useRef(false);
  const scrollTop = useRef(0), visible = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const initialize = useRef<() => void>();
  const repaint = async () => {
    const state = renderer.current;
    if (!state || !visible.current || reduced) return;
    const sampledScroll = scrollTop.current;
    const rect = { ...state.rect, top: state.rect.top - sampledScroll };
    if (rect.top + rect.height < 0 || rect.top > state.wallpaper.height / state.scale) return;
    try {
      const pixels = refractGlass(state.wallpaper, rect, state.scale, true);
      const ctx = state.canvas.getContext('2d');
      const image = ctx.createImageData(pixels.width, pixels.height); image.data.set(pixels.data);
      ctx.putImageData(image, 0, 0);
      const file = await Taro.canvasToTempFilePath({ canvas: state.canvas as any, fileType: 'png', destWidth: pixels.width, destHeight: pixels.height });
      if (renderer.current === state && visible.current && sampledScroll === scrollTop.current) {
        remember('lens', file.tempFilePath); lensVisible.current = true; onLensChange(file.tempFilePath);
      }
      else discard(file.tempFilePath);
    } catch { if (renderer.current === state) renderer.current = undefined; }
  };
  usePageScroll(event => {
    if (event.scrollTop === scrollTop.current) return;
    scrollTop.current = event.scrollTop;
    // A cached rim belongs to its sampled viewport position. Never drag that image
    // over a different part of the fixed wallpaper while waiting for a fresh sample.
    if (lensVisible.current) { lensVisible.current = false; onLensChange(''); }
    if (!renderer.current || !visible.current) return;
    // Render after scrolling settles, not on every frame or scroll event.
    clearTimeout(timer.current);
    timer.current = setTimeout(() => { timer.current = undefined; void repaint(); }, 140);
  });
  useDidHide(() => { visible.current = false; clearTimeout(timer.current); timer.current = undefined; });
  useDidShow(() => { visible.current = true; initialize.current?.(); });
  useEffect(() => {
    let cancelled = false;
    lensVisible.current = false;
    onLensChange('');
    initialize.current = () => {
      if (!visible.current || reduced) return;
      Taro.nextTick(() => {
        if (cancelled) return;
        Taro.createSelectorQuery().select('#functions-glass-wall').fields({ node: true, size: true })
          .select('#functions-glass-lens').fields({ node: true })
          .select('.tools-entry-script').boundingClientRect()
          .exec(async (results: CanvasField[]) => {
            if (cancelled || !visible.current) return;
            const [wall, lens, rect] = results;
            if (!wall?.node || !lens?.node || !wall.width || !rect?.width) return;
            try {
              const info = Taro.getWindowInfo();
              // Bound pixel work and memory on high-DPR phones.
              const scale = Math.min(1.5, info.pixelRatio || 1);
              wall.node.width = Math.round(wall.width * scale); wall.node.height = Math.round(wall.height * scale);
              const ctx = wall.node.getContext('2d');
              paintWallpaper(ctx, wall.node.width, wall.node.height, theme === 'dark');
              const wallpaper = ctx.getImageData(0, 0, wall.node.width, wall.node.height);
              lens.node.width = Math.round(rect.width * scale); lens.node.height = Math.round(rect.height * scale);
              const file = await Taro.canvasToTempFilePath({ canvas: wall.node as any, fileType: 'png', destWidth: wall.node.width, destHeight: wall.node.height });
              if (cancelled || !visible.current) { discard(file.tempFilePath); return; }
              remember('wall', file.tempFilePath);
              setWallSource(file.tempFilePath);
              renderer.current = { wallpaper, canvas: lens.node, scale, rect: { left: rect.left, top: rect.top + scrollTop.current, width: rect.width, height: rect.height } };
              void repaint();
            } catch { renderer.current = undefined; }
          });
      });
    };
    initialize.current();
    const resize = () => initialize.current?.();
    Taro.onWindowResize(resize);
    return () => { cancelled = true; clearTimeout(timer.current); timer.current = undefined; renderer.current = undefined; initialize.current = undefined; Taro.offWindowResize(resize); };
  }, [theme, reduced, onLensChange]);
  return <View className="tools-glass-background" aria-hidden="true">
    <View className="tools-glass-ribbon" /><View className="tools-glass-glow" />
    {!reduced && wallSource ? <Image src={wallSource} mode="scaleToFill" className="tools-native-wall" /> : null}
    {!reduced ? <><Canvas type="2d" id="functions-glass-wall" className="tools-glass-buffer" /><Canvas type="2d" id="functions-glass-lens" className="tools-glass-buffer" /></> : null}
  </View>;
}
