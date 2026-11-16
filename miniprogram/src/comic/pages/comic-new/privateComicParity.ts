export const PUBLIC_COMIC_ASPECTS = [
  { value: "16:9", label: "16:9" },
  { value: "9:16", label: "9:16" },
  { value: "1:1", label: "1:1" },
] as const;

export type privateComicStage = "settings" | "assets" | "config" | "render" | "result";

export type PrivateComicStageInput = {
  sourceLength: number;
  minSourceLength: number;
  styleId: string;
  batchId: string;
  assetsCount: number;
  pagesCount: number;
  submitted: boolean;
  busy?: boolean;
};

export function canEnterPrivateComicStage(stage: privateComicStage, input: PrivateComicStageInput): boolean {
  switch (stage) {
    case "settings": return true;
    case "assets": return Boolean(input.batchId);
    // 资产是「边看边继续」的只读展示，不再作为进入确认页的门禁：服务端在跑提取，
    // 客户端再拦一次只会让人干等，资产没齐也能直接确认配置。
    case "config": return Boolean(input.batchId);
    case "render": return Boolean(input.batchId) && input.submitted;
    case "result": return input.pagesCount > 0;
  }
}

export function privateComicAction(
  stage: privateComicStage,
  input: PrivateComicStageInput,
): { label: string; enabled: boolean } {
  let action: { label: string; enabled: boolean };
  switch (stage) {
    case "settings":
      action = {
        label: "创建项目并提取资产",
        enabled: input.sourceLength >= input.minSourceLength && Boolean(input.styleId),
      };
      break;
    case "assets":
      action = { label: "继续：生成配置确认", enabled: canEnterPrivateComicStage("config", input) };
      break;
    case "config":
      action = { label: "确认配置，开始分页出图", enabled: canEnterPrivateComicStage("config", input) };
      break;
    case "render":
      action = { label: "查看生成结果", enabled: canEnterPrivateComicStage("result", input) };
      break;
    case "result":
      action = { label: "重新创作", enabled: true };
      break;
  }
  return { ...action, enabled: action.enabled && !input.busy };
}
