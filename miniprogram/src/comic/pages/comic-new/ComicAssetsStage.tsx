import { Button, Image, Input, Text, View } from "@tarojs/components";
import { useEffect, useState } from "react";
import {
  COMIC_ASSET_KIND_LABELS, comicAssetKindCounts,
  type ComicAssetImageItem, type ComicAssetItem,
} from "../../services/comicAssets";

export type ComicAssetsStageProps = {
  assets: ComicAssetItem[];
  assetImages: Record<string, ComicAssetImageItem[]>;
  assetThumbs: Record<string, string>;
  loading: boolean;
  /** 服务端正在 comic_assets 阶段提取：此时列表为空是正常的，不是失败。 */
  serverExtracting?: boolean;
  /** 出图开关关闭时服务端会跳过资产图，空图不能显示成失败。 */
  imagesSkipped?: boolean;
  busy: string;
  busyAt: number;
  error: string;
  onExtract: () => void;
  onGenerateMissingImages: () => void;
  onRetry: () => void;
  onRefresh: () => void;
  onContinue: () => void;
  onBack: () => void;
};

/** 秒数只在这个组件内部重渲染，不牵动四步条、资产卡片和画风卡。 */
function BusyClock({ label, startedAt }: { label: string; startedAt: number }) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const tick = () => setSeconds(Math.max(0, Math.floor((Date.now() - startedAt) / 1000)));
    tick();
    const timer = setInterval(tick, 1000);
    return () => clearInterval(timer);
  }, [startedAt]);
  return <Text className="comic-step-hint">{label}… 已用 {seconds}s</Text>;
}

export default function ComicAssetsStage(props: ComicAssetsStageProps) {
  const [kind, setKind] = useState("character");
  const [query, setQuery] = useState("");
  const [onlyMissing, setOnlyMissing] = useState(false);
  const [failedThumbs, setFailedThumbs] = useState<Record<string, string>>({});
  const counts = comicAssetKindCounts(props.assets);
  const missing = props.assets.filter((asset) => !props.assetImages[asset.id]?.length).length;
  const blocked = props.loading || Boolean(props.busy) || Boolean(props.serverExtracting);
  const busyLabel = props.busy === "extract" ? "正在提取角色与场景"
    : props.busy === "images" ? "正在补齐资产图"
      : props.busy === "retry" ? "正在重试失败阶段" : "";
  // 资产只做展示，不做必经门禁：服务端还在提取时也允许直接进确认页。
  const canContinue = true;
  const search = query.trim().toLowerCase();
  const visible = props.assets.filter((asset) => asset.kind === kind
    && (!onlyMissing || !props.assetImages[asset.id]?.length)
    && (!search || [asset.name, asset.formalName, asset.appearance, asset.facts, asset.prompt]
      .some((value) => value.toLowerCase().includes(search))));
  const action = (callback: () => void) => { if (!blocked) callback(); };

  return <View className="comic-assets-stage">
    <View className="mp-card">
      <Text className="mp-row-title">资产工作台</Text>
      <Text className="mp-row-description">资产会自动提取并展示；需要时再补齐定妆图，也可以直接继续确认生成配置。</Text>
      <Text className="comic-caption">共 {props.assets.length} 项 · 已生成 {props.assets.length - missing}/{props.assets.length} 张资产图</Text>
      <View className="comic-asset-actions">
        <Button className="mp-secondary" disabled={blocked || missing === 0} loading={props.busy === "images"}
          onClick={() => { if (missing > 0) action(props.onGenerateMissingImages); }}>
          {missing ? `补齐资产图（缺 ${missing}）` : "资产图已齐"}
        </Button>
        <Button className="mp-secondary" disabled={blocked} loading={props.busy === "extract"} onClick={() => action(props.onExtract)}>
          {props.busy === "extract" ? "正在提取…" : "重新提取资产"}
        </Button>
        <Button className="mp-secondary" disabled={blocked} loading={props.busy === "retry"} onClick={() => action(props.onRetry)}>
          {props.busy === "retry" ? "正在重试…" : "重试失败阶段"}
        </Button>
        <Button className="mp-secondary" disabled={blocked} onClick={() => action(() => { setFailedThumbs({}); props.onRefresh(); })}>刷新资产</Button>
      </View>
      {props.loading ? <Text className="comic-step-hint">正在读取资产…</Text> : null}
      {props.serverExtracting ? <Text className="comic-step-hint">正在提取角色与场景…提取完成后会自动出现，不用手动点</Text> : null}
      {busyLabel ? <BusyClock label={busyLabel} startedAt={props.busyAt} /> : null}
      {props.imagesSkipped && props.assets.length ? <Text className="comic-hint">本次没有开启资产图，定妆图会被跳过，不影响继续。</Text> : null}
      <Text className="comic-hint">离开页面服务端仍会继续；进行中的提取或补齐动作没有可续接的句柄，回来后按服务端结果手动重试。</Text>
      {props.error ? <Text className="mp-error">{props.error}</Text> : null}
      <View className="comic-asset-tabs">
        {(["character", "scene", "prop"] as const).map((value) => <View key={value}
          className={`comic-asset-tab ${kind === value ? "is-active" : ""}`} onClick={() => setKind(value)}>
          <Text>{COMIC_ASSET_KIND_LABELS[value]} {counts[value]}</Text>
        </View>)}
      </View>
      <Input className="comic-asset-search" value={query} placeholder="搜索名称或描述" onInput={(event) => setQuery(event.detail.value)} />
      <View className="comic-asset-filter" onClick={() => setOnlyMissing(!onlyMissing)}><Text>{onlyMissing ? "✓ " : ""}只看缺图资产</Text></View>
      {!visible.length && !props.loading && !props.serverExtracting ? <Text className="comic-hint">{props.assets.length ? "当前分类或筛选下没有资产" : "还没有提取到资产，可以直接继续，也可以重新提取或刷新。"}</Text> : null}
      <View className="comic-asset-cards">
        {visible.map((asset) => {
          const hasImages = Boolean(props.assetImages[asset.id]?.length);
          const path = props.assetThumbs[asset.id];
          const showThumb = Boolean(path) && failedThumbs[asset.id] !== path;
          return <View key={asset.id} className="comic-asset-card">
            <View className="comic-asset-card-heading">
              {showThumb ? <Image src={path} mode="aspectFill" className="comic-asset-thumb"
                onError={() => setFailedThumbs((previous) => ({ ...previous, [asset.id]: path }))} />
                : <View className="comic-asset-thumb comic-asset-thumb-empty"><Text>{hasImages ? "图片暂不可用" : props.imagesSkipped ? "未开启" : "未生成"}</Text></View>}
              <View className="comic-asset-info">
                <Text className="comic-asset-name">{asset.name || asset.formalName || "未命名资产"}</Text>
                <Text className="comic-caption">{COMIC_ASSET_KIND_LABELS[asset.kind] || "资产"} · {hasImages ? "已有资产图" : props.imagesSkipped ? "本次未生成资产图" : "缺少资产图"}</Text>
              </View>
            </View>
            {asset.formalName && asset.formalName !== asset.name ? <Text className="comic-asset-detail">正式名称：{asset.formalName}</Text> : null}
            <Text className="comic-asset-detail">外形：{asset.appearance || "暂无外形描述"}</Text>
            {asset.facts ? <Text className="comic-asset-detail">设定：{asset.facts}</Text> : null}
            {asset.prompt ? <Text className="comic-asset-detail">提示词：{asset.prompt}</Text> : null}
          </View>;
        })}
      </View>
    </View>
    <View className="mp-card">
      <Text className="comic-hint">当前小程序仅支持只读查看，不支持资产文字编辑、手动上传和主图切换。</Text>
      <View className="comic-asset-actions">
        <Button className="mp-secondary" disabled>编辑资产</Button>
        <Button className="mp-secondary" disabled>手动上传</Button>
        <Button className="mp-secondary" disabled>切换主图</Button>
      </View>
    </View>
    <View className="comic-dock">
      <View className="comic-dock-copy"><Text className="comic-dock-line">{props.assets.length ? "资产已加载，可以直接继续确认生成配置" : "资产仍在提取，也可以直接继续"}</Text></View>
      <View className="comic-sticky-row">
        <Button className="mp-secondary" disabled={blocked} onClick={() => action(props.onBack)}>返回设置</Button>
        <Button className="mp-primary" disabled={!canContinue} onClick={() => { if (canContinue) props.onContinue(); }}>继续：生成配置确认</Button>
      </View>
    </View>
  </View>;
}
