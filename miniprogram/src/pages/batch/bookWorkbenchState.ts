import type { BatchFactoryBook } from "../../services/batchFactoryV11";

function bookText(book: BatchFactoryBook | null, key: string) {
  const value = book && (book as Record<string, unknown>)[key];
  return String(value || "").trim();
}
export function bookWorkbenchStageRows(book: BatchFactoryBook | null) {
  const source = book as (BatchFactoryBook & Record<string, any>) | null;
  const production = source?.production || source?.video || {};
  const merge = source?.merge || source?.mergeJob || {};
  const publish = source?.publish || source?.publishStatus || {};
  return [
    { key: "source", label: "原文", state: bookText(book, "sourceText") ? "已读取" : "状态未返回" },
    { key: "assets", label: "人物与场景", state: String(source?.assetSummary?.status || "状态未返回") },
    { key: "video", label: "分镜与视频", state: String(production.status || "状态未返回") },
    { key: "merge", label: "合成", state: String(merge.status || "状态未返回") },
    { key: "publish", label: "发布", state: String(publish.status || "状态未返回") },
  ];
}
