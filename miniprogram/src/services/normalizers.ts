export type HistoryEntry = {
  id?: string;
  title?: string;
  preview?: string;
  createdAt?: string;
  created_at?: string;
  status?: string;
};

export type BatchItem = {
  id?: string;
  title?: string;
  name?: string;
  status?: string;
};

export function entriesFromHistory(payload: unknown): HistoryEntry[] {
  const entries = (payload as { entries?: unknown } | null)?.entries;
  return Array.isArray(entries) ? entries.filter((item): item is HistoryEntry => Boolean(item && typeof item === "object")) : [];
}

export function latestHistoryEntry(payload: unknown) {
  return entriesFromHistory(payload)[0];
}

export function batchesFromPayload(payload: unknown): BatchItem[] {
  const value = payload as { batches?: unknown; tasks?: unknown } | null;
  const source = Array.isArray(value?.batches) ? value.batches : Array.isArray(value?.tasks) ? value.tasks : [];
  return source.filter((item): item is Record<string, unknown> => Boolean(item && typeof item === "object")).map((item) => ({
    id: String(item.id ?? item.bookId ?? item.book_id ?? ""),
    title: String(item.title ?? item.name ?? item.bookName ?? item.book_name ?? item.id ?? item.book_id ?? "未命名批次"),
    status: String(item.status ?? item.aiStatus ?? item.ai_status ?? item.originalStatus ?? item.original_status ?? "同步中"),
  }));
}
