import { Button, Image, ScrollView, Text, Textarea, View } from "@tarojs/components";
import AppIcon from "../../../components/AppIcon";
import {
  COMIC_SOURCE_TRUNCATE_HINT_CHARS, DEFAULT_COMIC_ASPECT_RATIO, MIN_COMIC_SOURCE_CHARS,
  comicSourceLength, type ComicStylePreset,
} from "../../services/comicFlow";
import { PUBLIC_COMIC_ASPECTS, privateComicAction } from "./privateComicParity";
import { estimateComicRunText } from "../../services/runStage";

export type ComicSettingsStageProps = {
  source: string;
  styleId: string;
  aspectRatio: string;
  referencePath: string;
  styles: ComicStylePreset[];
  thumbs: Record<string, string>;
  thumbFailed: Record<string, boolean>;
  setupLoading: boolean;
  error: string;
  onSourceChange: (value: string) => void;
  onStyleChange: (value: string) => void;
  onAspectChange: (value: string) => void;
  onOpenReference: () => void;
  onRetryThumb: (styleId: string) => void;
  onContinue: () => void;
  continueDisabled?: boolean;
  continueLoading?: boolean;
  continueHint?: string;
};

const ASPECT_NOTES = { "16:9": "横版", "9:16": "竖版", "1:1": "方形" };

/** 设置只负责输入和继续意图；创建、上传、提取等服务动作由页面持有。 */
export default function ComicSettingsStage({
  source, styleId, aspectRatio, referencePath, styles, thumbs, thumbFailed, setupLoading, error,
  onSourceChange, onStyleChange, onAspectChange, onOpenReference, onRetryThumb, onContinue,
  continueDisabled = false, continueLoading = false, continueHint = "",
}: ComicSettingsStageProps) {
  const length = comicSourceLength(source);
  const remaining = Math.max(0, MIN_COMIC_SOURCE_CHARS - length);
  const tooLong = length > COMIC_SOURCE_TRUNCATE_HINT_CHARS;
  const action = privateComicAction("settings", {
    sourceLength: length, minSourceLength: MIN_COMIC_SOURCE_CHARS, styleId,
    batchId: "", assetsCount: 0, pagesCount: 0, submitted: false,
    busy: setupLoading || continueLoading || continueDisabled,
  });
  const countText = length ? (remaining > 0 ? `已输入 ${length} 字，还需 ${remaining} 字` : `已输入 ${length} 字，可以开始生成了`) : "还没有输入正文";
  // 估算只在够提交长度后出现，措辞固定「预计 / 约 / 估算」，不写成承诺。
  const estimate = length >= MIN_COMIC_SOURCE_CHARS ? estimateComicRunText(length) : "";
  const hint = continueHint || (setupLoading ? "正在读取画风和预设…" : !styleId ? "还没有可用画风，稍后再试" : !length ? "先粘贴小说正文" : remaining > 0 ? `还需 ${remaining} 字` : "先创建项目并提取资产，下一步再确认分镜");
  return <View className="comic-settings-stage">
    {error ? <Text className="mp-error comic-error">{error}</Text> : null}

    <View className="comic-paper">
      <View className="comic-paper-head">
        <View className="comic-paper-title"><AppIcon name="file-pen-line" size={16} /><Text>小说正文</Text></View>
        <Text className="comic-paper-tag">{length ? "当前创作" : "等待灵感"}</Text>
      </View>
      <Textarea
        className="mp-textarea comic-source-input"
        value={source}
        maxlength={-1}
        placeholder="把小说正文整段粘贴在这里，句子之间保留标点，分页会更准。"
        onInput={(event) => onSourceChange(String(event.detail.value))}
      />
      {tooLong ? <Text className="comic-hint">内容有点长，一次最多做 35 页，只会用到前面的部分。</Text> : null}
      <View className="comic-paper-foot">
        <Text>{length.toLocaleString()} 字</Text>
        <Text>至少 {MIN_COMIC_SOURCE_CHARS} 字</Text>
      </View>
    </View>

    <View className="comic-section">
      <View className="comic-section-head">
        <View className="comic-head-main"><Text className="comic-head-index">02</Text><Text className="comic-head-title">画风</Text></View>
        <Text className="comic-section-caption">{styles.length ? `${styles.length} 种可用` : "读取中"}</Text>
      </View>
      {setupLoading ? <Text className="comic-hint">正在读取画风…</Text> : null}
      {styles.length ? <ScrollView scrollX className="comic-style-row">
        {styles.map((style) => {
          const active = style.id === styleId;
          const thumb = thumbs[style.id];
          const failed = Boolean(thumbFailed[style.id]);
          return (
            <View key={style.id} className={`comic-style-card ${active ? "is-active" : ""}`} onClick={() => onStyleChange(style.id)}>
              {thumb
                ? <Image src={thumb} mode="aspectFill" className="comic-style-thumb" />
                : !style.previewUrl
                  // 后端没有下发预览图，保留首字色块，不假装在加载。
                  ? <View className="comic-style-fallback"><Text>{Array.from(style.name)[0] || "画"}</Text></View>
                  : failed
                    ? <View
                      className="comic-style-thumb is-failed"
                      onClick={(event) => { event.stopPropagation(); onRetryThumb(style.id); }}
                    >
                      <AppIcon name="refresh-ccw" size={16} /><Text>重试</Text>
                    </View>
                    // 骨架和真图共用同一个盒子类名，尺寸完全一致，加载完成不会跳动。
                    : <View className="comic-style-thumb is-skeleton" />}
              <Text className="comic-style-name">{style.name}</Text>
              <Text className="comic-style-state">{active ? "当前" : "选用"}</Text>
            </View>
          );
        })}
      </ScrollView> : <View className="comic-empty"><AppIcon name="circle-dashed" size={22} /><View><Text>还没有可用画风，稍后再试。</Text><Text className="comic-caption">画风目录稍后会自动出现。</Text></View></View>}
    </View>

    <View className="comic-section">
      <View className="comic-section-head">
        <View className="comic-head-main"><Text className="comic-head-index">03</Text><Text className="comic-head-title">图片画幅</Text></View>
        <Text className="comic-section-caption">{aspectRatio || DEFAULT_COMIC_ASPECT_RATIO}</Text>
      </View>
      <View className="comic-aspect-row">
        {PUBLIC_COMIC_ASPECTS.map((item) => (
          <View key={item.value} className={`comic-aspect ${aspectRatio === item.value ? "is-active" : ""}`} onClick={() => onAspectChange(item.value)}>
            <Text>{item.label}</Text><Text className="comic-aspect-note">{ASPECT_NOTES[item.value]}</Text>
          </View>
        ))}
      </View>
    </View>

    <View className="comic-section">
      <View className="comic-section-head">
        <View className="comic-head-main"><Text className="comic-head-index">04</Text><Text className="comic-head-title">参考图</Text></View>
        <Text className="comic-section-caption">可选</Text>
      </View>
      <View className="comic-choice" onClick={onOpenReference}>
        <View className="comic-choice-main">
          <AppIcon name="camera" size={18} />
          <View>
            <Text className="comic-choice-title">{referencePath ? "已选一张参考图" : "还没选参考图"}</Text>
            <Text className="comic-choice-note">有参考图时，画风会照着这张图的调子走。</Text>
          </View>
        </View>
        <Text className="comic-choice-mark">{referencePath ? "更换" : "去选"}</Text>
      </View>
    </View>

    <View className="comic-dock">
      <View className="comic-dock-copy">
        <Text className="comic-dock-line">{countText}</Text>
        {estimate ? <Text className="comic-dock-caption">{estimate}</Text> : null}
        <Text className="comic-dock-caption">{hint}</Text>
      </View>
      <Button className="mp-primary" loading={continueLoading} disabled={!action.enabled} onClick={() => { if (action.enabled) onContinue(); }}>
        {continueLoading ? "正在创建项目…" : action.label}
      </Button>
    </View>
  </View>;
}
