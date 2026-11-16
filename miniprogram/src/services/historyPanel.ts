export type PanelEntity = {
  name: string;
  description: string;
};

export type HistoryPanelData = {
  novelText: string;
  characters: PanelEntity[];
  scenes: PanelEntity[];
  shots: string[];
  output: string;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function text(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function entities(value: unknown): PanelEntity[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (typeof item === "string" && item.trim()) return [{ name: item.trim(), description: "" }];
    const record = asRecord(item);
    if (!record) return [];
    const nested = asRecord(record.data);
    const name = text(record.name) || text(record.title) || text(nested?.name) || text(nested?.title);
    if (!name) return [];
    return [{ name, description: text(record.description) || text(nested?.description) }];
  });
}

function shotsFromOutput(output: string) {
  if (!output) return [];
  const cards = output.split(/(?=^###\s*分镜[一二三四五六七八九十\d]+)/m).map((item) => item.trim()).filter(Boolean);
  return cards.length > 1 || /^###\s*分镜[一二三四五六七八九十\d]+/m.test(output) ? cards : [output];
}

export function historyPanelFromEntry(value: unknown): HistoryPanelData {
  const entry = asRecord(value) || {};
  const extractInfo = asRecord(entry.extractInfo) || {};
  const output = text(entry.output);
  return {
    novelText: text(entry.novelText),
    characters: entities(extractInfo.characters || extractInfo.people),
    scenes: entities(extractInfo.scenes),
    shots: shotsFromOutput(output),
    output,
  };
}
