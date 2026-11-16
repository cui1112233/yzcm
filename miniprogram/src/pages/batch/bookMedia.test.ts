import { expect, test } from "vitest";
import { bookMediaOptions, mediaFailureMessage, stagePrompt } from "./bookMedia";
import type { BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";

const base: BatchFactoryBookWorkspace = { readState: "ready", batchId: "batch-1", bookId: "book-1", book: { id: "book-1" }, stages: null, assets: [], assetImages: {}, production: null, mergeStatus: null };
test("current storyboard order and prompts exclude historical production tasks", () => {
  const options = bookMediaOptions({ ...base,
    book: { id: "book-1", videos: [{ id: "current-2", videoPrompt: "当前第二版开场" }, { id: "current-1", videoPrompt: "当前结尾" }] },
    production: { tasks: [
      { videoId: "retired", prompt: "废弃分镜", mediaUrl: "/old.mp4" },
      { videoId: "current-1", prompt: "旧结尾", mediaUrl: "/ending.mp4", status: "succeeded" },
      { videoId: "current-2", prompt: "旧开场", mediaUrl: "/opening.mp4", status: "succeeded" },
    ] },
  });
  expect(options).toEqual([
    { key: "shot-2", label: "分镜 1", url: "/opening.mp4", prompt: "当前第二版开场", status: "succeeded", videoId: "current-2", revision: undefined },
    { key: "shot-1", label: "分镜 2", url: "/ending.mp4", prompt: "当前结尾", status: "succeeded", videoId: "current-1", revision: undefined },
  ]);
});
test("an explicitly empty storyboard does not revive historical tasks", () => {
  expect(bookMediaOptions({ ...base, book: { id: "book-1", videos: [] }, production: { tasks: [{ videoId: "retired", mediaUrl: "/old.mp4" }] } })).toEqual([]);
});
test("pairs factory video prompts with production tasks that do not repeat the prompt", () => {
  const options = bookMediaOptions({ ...base, book: { id: "book-1", videos: [{ id: "video-1", videoPrompt: "女主推门进入，镜头跟随" }] }, production: { tasks: [{ videoId: "video-1", status: "succeeded", mediaUrl: "/shot.mp4" }] } });
  expect(options[0].prompt).toBe("女主推门进入，镜头跟随");
});
test("shows saved storyboard prompts even before video production has started", () => {
  const options = bookMediaOptions({ ...base, book: { id: "book-1", videos: [{ id: "video-1", videoPrompt: "低角度仰拍" }] } });
  expect(options).toHaveLength(1);
  expect(options[0]).toMatchObject({ prompt: "低角度仰拍", url: "" });
});
test("keeps each shot selectable including failed shots without inventing media", () => {
  expect(bookMediaOptions({ ...base, production: { tasks: [{ mediaUrl: "/shot.mp4", prompt: "近景" }, { status: "failed" }] } })).toEqual([
    { key: "shot-0", label: "分镜 1", url: "/shot.mp4", prompt: "近景", status: undefined, videoId: "", revision: undefined },
    { key: "shot-1", label: "分镜 2", url: "", prompt: "", status: "failed", videoId: "", revision: undefined },
  ]);
});
test("only offers merged media belonging to the current book", () => {
  const options = bookMediaOptions({ ...base, mergeStatus: { jobs: [{ bookId: "other", outputUrl: "/wrong.mp4" }, { bookId: "book-1", outputUrl: "/merged.mp4", status: "completed" }] } });
  expect(options).toHaveLength(1);
  expect(options[0].url).toBe("/merged.mp4");
});
test("empty and loading books contain no fabricated media", () => expect(bookMediaOptions({ ...base, readState: "loading", book: null })).toEqual([]));
test("reads prompts in the actual nested stage response", () => {
  expect(stagePrompt({ data: { stages: [{ prompt: "近景" }, { videoPrompt: "远景" }] } })).toBe("近景\n\n远景");
  expect(stagePrompt({ data: { stages: [{ status: "queued" }] } })).toBe("");
});

test("download errors retain the actual reason without disclosing signed media URLs", () => {
  const message = mediaFailureMessage(new Error("downloadFile:fail url not in domain list https://media.example/clip.mp4?token=private-signature"), "download");
  expect(message).toContain("url not in domain list");
  expect(message).not.toContain("private-signature");
  expect(message).not.toContain("media.example");
});

test("native playback errors retain their numeric code and reason", () => {
  const message = mediaFailureMessage({ errMsg: "MEDIA_ERR_DECODE", errCode: 3 }, "playback");
  expect(message).toContain("MEDIA_ERR_DECODE");
  expect(message).toContain("3");
});
