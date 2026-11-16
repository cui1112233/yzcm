import { Text, View } from "@tarojs/components";
import type { ComicStage } from "./stageState";
import { stageIndex, stageTitle } from "./stageState";

const STAGES: ComicStage[] = ["source", "assets", "storyboard", "render", "result"];

export type ComicStageHeaderProps = {
  stage: ComicStage;
  title: string;
  subtitle: string;
  hasResult: boolean;
  canSelectStage: (stage: ComicStage) => boolean;
  onSelectStage: (stage: ComicStage) => void;
};

export default function ComicStageHeader({ stage, title, subtitle, hasResult, canSelectStage, onSelectStage }: ComicStageHeaderProps) {
  const current = stageIndex(stage);
  return <>
    <View className="comic-stage-header">
      <View className="comic-stage-header-copy">
        <Text className="comic-eyebrow">PRIVATE COMIC STUDIO</Text>
        <Text className="comic-stage-title">{title}</Text>
        <Text className="comic-stage-subtitle">{subtitle}</Text>
      </View>
      {hasResult && canSelectStage("result") ? <Text className="comic-stage-result-link" onClick={() => onSelectStage("result")}>查看结果</Text> : null}
    </View>
    <View className="comic-stage-rail">
      {STAGES.map((item, index) => {
        const done = index < current || (item === "result" && hasResult);
        const active = item === stage;
        const enabled = canSelectStage(item);
        return <View
          key={item}
          className={`comic-stage-node ${active ? "is-active" : ""} ${done ? "is-done" : ""} ${enabled ? "" : "is-disabled"}`}
          onClick={enabled ? () => onSelectStage(item) : undefined}
        >
          <Text className="comic-stage-node-dot">{done ? "✓" : index + 1}</Text>
          <Text className="comic-stage-node-label">{stageTitle(item)}</Text>
        </View>;
      })}
    </View>
  </>;
}
