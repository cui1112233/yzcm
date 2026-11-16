import { Canvas, View } from '@tarojs/components';
import Taro, { useDidHide, useDidShow, usePageScroll } from '@tarojs/taro';
import { useEffect, useRef, useState } from 'react';
import darkWall from '../assets/glass/wallpaper-dark.png';
import lightWall from '../assets/glass/wallpaper-light.png';
import {
  glassBackingScale,
  glassOpticsForRole,
  refractRoundedSurface,
  sampleOrigin,
  visibleRefractionRects,
  type GlassSurfaceRect,
  type RefractionTheme,
} from './glassRefraction';
import {
  createGlassFrameScheduler,
  GLASS_INTERNAL_SCROLL,
  GLASS_LAYOUT_CHANGE,
  normalizeGlassScroll,
  viewportRectsToDocument,
} from './glassScrollBridge';
import { glassSelectors } from './liquidGlass';

type NativeCanvas = {
  width: number;
  height: number;
  getContext: (kind: '2d') => any;
  requestAnimationFrame: (callback: () => void) => number;
  cancelAnimationFrame: (frame: number) => void;
  createImage: () => any;
};

type Renderer = {
  node: NativeCanvas;
  context: any;
  sourcePixels: Uint8ClampedArray;
  cssHeight: number;
  pixelWidth: number;
  pixelHeight: number;
  scale: number;
  documentRects: GlassSurfaceRect[];
};

type Scheduler = ReturnType<typeof createGlassFrameScheduler>;

const GLASS_INIT_RETRY_DELAYS = [60, 120, 240, 480] as const;

function scaleGlassRect(rect: GlassSurfaceRect, scale: number): GlassSurfaceRect {
  return {
    ...rect,
    left: rect.left * scale,
    top: rect.top * scale,
    width: rect.width * scale,
    height: rect.height * scale,
  };
}

export type DynamicGlassCanvasProps = {
  theme: RefractionTheme;
  reduced: boolean;
  pageKey: string;
};

export default function DynamicGlassCanvas({ theme, reduced, pageKey }: DynamicGlassCanvasProps) {
  const renderer = useRef<Renderer>();
  const scheduler = useRef<Scheduler>();
  const scrollTop = useRef(0);
  const active = useRef(true);
  const disposed = useRef(false);
  const failedRef = useRef(false);
  const generation = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout>>();
  const [failed, setFailed] = useState(false);

  const clearRetry = () => {
    if (retryTimer.current === undefined) return;
    clearTimeout(retryTimer.current);
    retryTimer.current = undefined;
  };

  const fail = () => {
    if (failedRef.current || disposed.current) return;
    failedRef.current = true;
    clearRetry();
    scheduler.current?.stop();
    scheduler.current = undefined;
    renderer.current = undefined;
    setFailed(true);
  };

  const draw = (latestScroll: number) => {
    const current = renderer.current;
    if (!current || !active.current || failedRef.current) return;
    const { context, sourcePixels, cssHeight, pixelWidth, pixelHeight, scale } = current;
    const visible = visibleRefractionRects(current.documentRects.map(rect => {
      const origin = sampleOrigin(rect, latestScroll);
      return { ...rect, left: origin.x, top: origin.y };
    }), cssHeight);
    const prepared = visible.map(rect => {
      const optics = glassOpticsForRole(rect.role);
      const scaledRect = scaleGlassRect(rect, scale);
      const pixels = refractRoundedSurface(sourcePixels, pixelWidth, pixelHeight, scaledRect, {
        radius: optics.radius * scale,
        rim: optics.rim * scale,
        bend: optics.bend * scale,
        role: rect.role,
        reduced,
      });
      const image = context.createImageData(Math.round(scaledRect.width), Math.round(scaledRect.height));
      image.data.set(pixels);
      return { image, left: Math.round(scaledRect.left), top: Math.round(scaledRect.top) };
    });
    if (!active.current || failedRef.current) return;
    context.clearRect(0, 0, pixelWidth, pixelHeight);
    prepared.forEach(({ image, left, top }) => {
      context.putImageData(image, left, top);
    });
  };

  const initialize = (attempt = 0) => {
    if (disposed.current || failedRef.current || !active.current) return;
    clearRetry();
    const expected = ++generation.current;
    const retryWhenReady = () => {
      if (disposed.current || failedRef.current || !active.current || expected !== generation.current) return;
      const delay = GLASS_INIT_RETRY_DELAYS[attempt];
      if (delay === undefined) return;
      retryTimer.current = setTimeout(() => {
        retryTimer.current = undefined;
        initialize(attempt + 1);
      }, delay);
    };
    Taro.nextTick(() => {
      if (disposed.current || failedRef.current || !active.current || expected !== generation.current) return;
      const query = Taro.createSelectorQuery();
      query.select(`#dynamic-glass-${pageKey}`).fields({ node: true, size: true });
      query.selectAll(glassSelectors.join(',')).fields({ rect: true, dataset: true });
      query.exec((results: any[]) => {
        if (disposed.current || failedRef.current || !active.current || expected !== generation.current) return;
        try {
          const field = results?.[0];
          const node = field?.node as NativeCanvas | undefined;
          const width = Number(field?.width || 0);
          const height = Number(field?.height || 0);
          if (!node || !width || !height) {
            retryWhenReady();
            return;
          }
          if (typeof node.requestAnimationFrame !== 'function') throw new Error('Canvas 2D unavailable');
          const scale = glassBackingScale(Taro.getWindowInfo?.().pixelRatio || 1);
          const pixelWidth = Math.round(width * scale);
          const pixelHeight = Math.round(height * scale);
          const context = node.getContext('2d');
          const source = Taro.createOffscreenCanvas?.({ type: '2d', width: pixelWidth, height: pixelHeight });
          const sourceContext: any = source?.getContext?.('2d');
          if (!context || !source || !sourceContext) throw new Error('Offscreen Canvas unavailable');

          const viewportRects: GlassSurfaceRect[] = (Array.isArray(results?.[1]) ? results[1] : []).map((rect: any) => ({
            left: Number(rect.left || 0),
            top: Number(rect.top || 0),
            width: Number(rect.width || 0),
            height: Number(rect.height || 0),
            role: rect.dataset?.glassRole === 'primary' ? 'primary' as const : 'secondary' as const,
          })).filter((rect: GlassSurfaceRect) => rect.width > 0 && rect.height > 0);
          if (!viewportRects.length) {
            retryWhenReady();
            return;
          }
          if (typeof node.createImage !== 'function') throw new Error('Canvas image unavailable');
          const wallpaperImage = node.createImage();
          wallpaperImage.onload = () => {
            if (disposed.current || failedRef.current || !active.current || expected !== generation.current) return;
            try {
              sourceContext.clearRect(0, 0, pixelWidth, pixelHeight);
              const sourceWidth = Number(wallpaperImage.width || 0);
              const sourceHeight = Number(wallpaperImage.height || 0);
              if (!sourceWidth || !sourceHeight) throw new Error('Wallpaper dimensions unavailable');
              sourceContext.drawImage(
                wallpaperImage,
                0, 0, sourceWidth, sourceHeight,
                0, 0, pixelWidth, pixelHeight,
              );
              const sourcePixels = sourceContext.getImageData(0, 0, pixelWidth, pixelHeight).data;
              const nextRenderer: Renderer = {
                node,
                context,
                sourcePixels,
                cssHeight: height,
                pixelWidth,
                pixelHeight,
                scale,
                documentRects: viewportRectsToDocument(viewportRects, scrollTop.current),
              };
              scheduler.current?.stop();
              scheduler.current = undefined;
              node.width = pixelWidth;
              node.height = pixelHeight;
              renderer.current = nextRenderer;
              draw(scrollTop.current);
              scheduler.current = createGlassFrameScheduler(
                draw,
                callback => node.requestAnimationFrame(callback),
                frame => node.cancelAnimationFrame(frame),
              );
            } catch (error) {
              fail();
            }
          };
          wallpaperImage.onerror = () => fail();
          wallpaperImage.src = theme === 'dark' ? darkWall : lightWall;
        } catch (_error) {
          fail();
        }
      });
    });
  };

  const updateScroll = (value: number) => {
    scrollTop.current = normalizeGlassScroll(value);
    scheduler.current?.update(scrollTop.current);
  };

  usePageScroll(event => updateScroll(event.scrollTop));
  useDidHide(() => {
    active.current = false;
    generation.current += 1;
    clearRetry();
    scheduler.current?.stop();
  });
  useDidShow(() => {
    active.current = true;
    if (renderer.current) scheduler.current?.redraw();
    else initialize();
  });

  useEffect(() => {
    disposed.current = false;
    active.current = true;
    const onInternalScroll = (event: { pageKey: string; scrollTop: number }) => {
      if (event?.pageKey === pageKey) updateScroll(event.scrollTop);
    };
    const onLayout = (event: { pageKey: string }) => {
      if (event?.pageKey === pageKey) initialize();
    };
    const onResize = () => initialize();
    Taro.eventCenter?.on(GLASS_INTERNAL_SCROLL, onInternalScroll);
    Taro.eventCenter?.on(GLASS_LAYOUT_CHANGE, onLayout);
    Taro.onWindowResize?.(onResize);
    initialize();
    return () => {
      disposed.current = true;
      generation.current += 1;
      clearRetry();
      scheduler.current?.stop();
      scheduler.current = undefined;
      renderer.current = undefined;
      Taro.eventCenter?.off(GLASS_INTERNAL_SCROLL, onInternalScroll);
      Taro.eventCenter?.off(GLASS_LAYOUT_CHANGE, onLayout);
      Taro.offWindowResize?.(onResize);
    };
  }, [theme, reduced, pageKey]);

  return <View className={`dynamic-glass-layer${failed ? ' dynamic-glass-fallback' : ''}`} aria-hidden="true">
    <Canvas type="2d" id={`dynamic-glass-${pageKey}`} className="dynamic-glass-canvas" />
  </View>;
}
