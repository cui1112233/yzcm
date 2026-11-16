import { expect, test, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
vi.mock("@tarojs/components", () => ({ View: "div", Text: "span", Button: "button", Image: "img", Input: "input" }));
vi.mock("@tarojs/taro", () => ({ default: {} }));
vi.mock("../../components/AppIcon", () => ({ default: () => null }));
import PersonalWorksView from "./PersonalWorksView";
import type { BatchFactoryBatch } from "../../services/batchFactoryV11";

function render(batch: BatchFactoryBatch) {
  return renderToStaticMarkup(<PersonalWorksView readState="ready" batches={[batch]} onCreate={() => {}} onOpen={() => {}} onRetry={() => {}} />);
}

test.each([undefined, "unknown-new-status"])("missing or unknown book status does not become zero completion: %s", (status) => {
  const html = render({ id: "batch-1", books: [{ id: "book-1", status }], bookCount: 1 });
  expect(html).not.toContain('aria-label="完成');
  expect(html).not.toContain("0/1");
});

test("partial book details cannot be displayed as complete batch progress", () => {
  const html = render({ id: "batch-1", books: [{ id: "book-1", status: "completed" }], bookCount: 6 });
  expect(html).not.toContain('aria-label="完成');
});

test("complete known statuses retain actual completion including Chinese statuses", () => {
  const html = render({ id: "batch-1", books: [{ id: "book-1", status: "已完成" }, { id: "book-2", status: "running" }, { id: "book-3", status: "failed" }], bookCount: 3 });
  expect(html).toContain('aria-label="完成 1 / 3"');
  expect(html).toContain("1/3");
});
