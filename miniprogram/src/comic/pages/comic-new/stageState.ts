export type ComicStage = "source" | "assets" | "storyboard" | "render" | "result";

const STAGES: ComicStage[] = ["source", "assets", "storyboard", "render", "result"];

const TITLES: Record<ComicStage, string> = {
  source: "故事设置",
  assets: "资产工作台",
  storyboard: "生成配置确认",
  render: "分页出图",
  result: "成果导出",
};

export function stageIndex(stage: ComicStage) {
  return STAGES.indexOf(stage);
}

export function stageTitle(stage: ComicStage) {
  return TITLES[stage];
}

export function stageNext(stage: ComicStage): ComicStage | null {
  const index = stageIndex(stage);
  return index >= 0 && index < STAGES.length - 1 ? STAGES[index + 1] : null;
}

export function stageCanEnter(
  stage: ComicStage,
  input: { sourceLength: number; batchId: string; assetsCount: number; pagesCount: number; finished: boolean },
) {
  switch (stage) {
    case "source": return true;
    case "assets": return Boolean(input.batchId);
    case "storyboard": return Boolean(input.batchId && (input.assetsCount > 0 || input.pagesCount > 0));
    case "render": return Boolean(input.batchId);
    case "result": return input.pagesCount > 0 || input.finished;
    default: return false;
  }
}
