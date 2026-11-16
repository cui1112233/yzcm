export type BatchContentWindow = {
  contentRangeLines: number;
  contentCaptureCharacters: number;
};

export type ContentWindowValidation =
  | { ok: true; value: BatchContentWindow }
  | { ok: false; error: string };

const DEFAULT_CONTENT_WINDOW: BatchContentWindow = {
  contentRangeLines: 5,
  contentCaptureCharacters: 4000,
};

function inRange(value: unknown, minimum: number, maximum: number) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : 0;
}

export function readContentWindow(metadata: Record<string, unknown> = {}): BatchContentWindow {
  const contentRangeLines = inRange(metadata.contentRangeLines, 1, 500);
  const contentCaptureCharacters = inRange(metadata.contentCaptureCharacters, 100, 100000);
  return {
    contentRangeLines: contentRangeLines || DEFAULT_CONTENT_WINDOW.contentRangeLines,
    contentCaptureCharacters: contentCaptureCharacters || DEFAULT_CONTENT_WINDOW.contentCaptureCharacters,
  };
}

export function validateContentWindow(input: Partial<Record<keyof BatchContentWindow, unknown>>): ContentWindowValidation {
  const contentRangeLines = inRange(input.contentRangeLines, 1, 500);
  if (!contentRangeLines) return { ok: false, error: "内容范围需为 1 至 500 的整数" };
  const contentCaptureCharacters = inRange(input.contentCaptureCharacters, 100, 100000);
  if (!contentCaptureCharacters) return { ok: false, error: "获取上限需为 100 至 100000 的整数" };
  return { ok: true, value: { contentRangeLines, contentCaptureCharacters } };
}

export function previewContentWindow(sourceText: string, lines: number) {
  const logicalLines = String(sourceText || "").split(/\r?\n/).map(line => line.trim()).filter(Boolean);
  return { availableLines: logicalLines.length, selectedLines: logicalLines.slice(0, lines) };
}
