import { Button, Image, Video, View } from "@tarojs/components";
import Taro from "@tarojs/taro";
import { useEffect, useId, useState } from "react";
import { forgetAuthenticatedMedia, loadAuthenticatedMedia } from "../../services/mediaCache";
import { recoverBatchCoverVideo } from "../../services/batchCoverMedia";
import defaultCover from "../../assets/default-cover.png";

function coverExtension(url: string) {
  if (/^\/api\/batch-factory\/v11\/batches\/[^/]+\/merge-cover\/[^/]+$/.test(url)) return "jpg";
  return String(url).match(/\.([a-z0-9]{2,5})(?:\?|$)/i)?.[1] || "png";
}

export default function BatchCover({ url = "", batchId = "", className }: { url?: string; batchId?: string; className?: string }) {
  const id = `batch-cover-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`;
  const identity = `${batchId}:${url}`;
  const [visible, setVisible] = useState(!batchId);
  const [media, setMedia] = useState({ url: "", path: "", failed: false, video: false });
  const [retry, setRetry] = useState(0);
  const [videoPreview, setVideoPreview] = useState(false);
  useEffect(() => {
    let active = true;
    let observer: ReturnType<typeof Taro.createIntersectionObserver> | undefined;
    setVisible(!batchId);
    if (batchId) {
      const observe = () => {
        if (!active) return;
        if (typeof Taro.createIntersectionObserver !== 'function') { setVisible(true); return; }
        observer = Taro.createIntersectionObserver(Taro.getCurrentInstance().page!);
        observer.relativeToViewport().observe(`#${id}`, entry => {
          if (active && (entry.intersectionRatio || 0) > 0) { setVisible(true); observer?.disconnect(); }
        });
      };
      if (Taro.nextTick) Taro.nextTick(observe); else observe();
    }
    return () => { active = false; observer?.disconnect(); };
  }, [batchId, url, id]);
  useEffect(() => {
    let active = true;
    setMedia({ url: identity, path: "", failed: false, video: false });
    setVideoPreview(false);
    if (visible) void (async () => {
      if (url) try {
        const result = await loadAuthenticatedMedia(url, coverExtension(url));
        if (active) setMedia({ url: identity, path: result.path, failed: false, video: false });
        return;
      } catch { /* Historical merged covers can expire; recover from an existing successful video. */ }
      try {
        const video = batchId ? await recoverBatchCoverVideo(batchId, () => active) : '';
        if (!video) {
          if (active) setMedia({ url: identity, path: "", video: false, failed: Boolean(url || batchId) });
          return;
        }
        const result = await loadAuthenticatedMedia(video, "mp4");
        if (active) setMedia({ url: identity, path: result.path, video: true, failed: false });
      } catch { if (active) setMedia({ url: identity, path: '', failed: true, video: false }); }
    })();
    return () => { active = false; };
  }, [url, batchId, identity, visible, retry]);
  const current = media.url === identity;
  const failed = current && media.failed;
  return <View id={id} className={`mp-batch-cover ${className || ''}`}
    ariaLabel={failed ? "封面读取失败，点击重试" : "作品封面"}
    onClick={event => {
      if (!failed) return;
      event.stopPropagation();
      forgetAuthenticatedMedia(url);
      setRetry(value => value + 1);
    }}>
    {current && media.video && media.path && videoPreview ? <Video src={media.path} muted controls={false} autoplay={false}
      initialTime={0.01} showCenterPlayBtn={false} showPlayBtn={false} showFullscreenBtn={false} enableProgressGesture={false}
      objectFit="cover" onError={() => setMedia({ url: identity, path: '', failed: true, video: false })} />
      : <Image src={current && !media.video && media.path || defaultCover} mode="aspectFill"
        onError={() => { if (url) setMedia({ url: identity, path: "", failed: true, video: false }); }} />}
    {current && media.video && !videoPreview ? <Button className="mp-batch-cover-preview" ariaLabel="加载视频封面预览"
      onClick={event => { event.stopPropagation(); setVideoPreview(true); }}>预览</Button> : null}
  </View>;
}
