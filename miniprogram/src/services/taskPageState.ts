import type { TaskSummary } from "./taskSummary";

export type TaskPageState =
  | { kind: "configuration-error"; message: string }
  | { kind: "loading" }
  | { kind: "all-error"; failedSourceCount: number; entryCount: number }
  | { kind: "empty"; failedSourceCount: 0; entryCount: 0 }
  | { kind: "content"; failedSourceCount: number; entryCount: number };

export function deriveTaskPageState({
  loading,
  summary,
  configurationError,
}: {
  loading: boolean;
  summary: TaskSummary | null;
  configurationError: string;
}): TaskPageState {
  if (configurationError) return { kind: "configuration-error", message: configurationError };
  if (loading && !summary) return { kind: "loading" };
  if (!summary) return { kind: "all-error", failedSourceCount: 0, entryCount: 0 };

  const failedSourceCount = summary.sources.filter((source) => source.state === "error").length;
  if (failedSourceCount === summary.sources.length) {
    return { kind: "all-error", failedSourceCount, entryCount: summary.entries.length };
  }
  if (!summary.entries.length && failedSourceCount === 0) {
    return { kind: "empty", failedSourceCount: 0, entryCount: 0 };
  }
  return { kind: "content", failedSourceCount, entryCount: summary.entries.length };
}
