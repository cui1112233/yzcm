import type { PublicMemberCenter } from "../../config/profileData";
import { readMemberProfile } from "../../services/memberProfile";

export type UsageNumbers = {
  totalTokens?: number;
  calls?: number;
  outputTokens?: number;
  estimatedCost?: number;
  costCurrency?: string;
};

export type UsageCall = {
  id?: string;
  feature?: string;
  model?: string;
  totalTokens?: number;
  createdAt?: string;
};

export type UsageStatsView = {
  day: UsageNumbers;
  month: UsageNumbers;
  recent: UsageCall[];
};

function numberValue(value: unknown) {
  const number = typeof value === "number" ? value : typeof value === "string" && value.trim() ? Number(value) : Number.NaN;
  return Number.isFinite(number) ? number : undefined;
}

function stringValue(value: unknown) {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function numbers(source: unknown): UsageNumbers {
  const input = source && typeof source === "object" && !Array.isArray(source) ? source as Record<string, unknown> : {};
  return {
    totalTokens: numberValue(input.totalTokens),
    calls: numberValue(input.calls),
    outputTokens: numberValue(input.outputTokens),
    estimatedCost: numberValue(input.estimatedCost),
    costCurrency: stringValue(input.costCurrency),
  };
}

export function toUsageStatsView(center: PublicMemberCenter): UsageStatsView {
  const usage = center.usage;
  if (!usage || typeof usage !== "object") throw new Error("服务端未返回用量数据");
  const recent = Array.isArray(usage.recent)
    ? usage.recent.filter((item) => item && typeof item === "object").map((item) => ({
      id: stringValue(item.id),
      feature: stringValue(item.feature),
      model: stringValue(item.model),
      totalTokens: numberValue(item.totalTokens),
      createdAt: stringValue(item.createdAt || item.created_at),
    }))
    : [];
  return { day: numbers(usage.day), month: numbers(usage.month), recent };
}

export async function readUsageStats(): Promise<UsageStatsView> {
  return toUsageStatsView(await readMemberProfile());
}
