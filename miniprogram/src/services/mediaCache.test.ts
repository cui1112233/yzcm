import { beforeEach, describe, expect, it, vi } from "vitest";

const taro = vi.hoisted(() => ({
  env: { USER_DATA_PATH: "/user-data" },
  getFileSystemManager: vi.fn(),
  downloadFile: undefined as any,
}));
const api = vi.hoisted(() => ({ apiRequest: vi.fn() }));
const account = vi.hoisted(() => ({ token: "account-a", cookie: "" }));

vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("./api", () => api);
vi.mock("./session", () => ({ getBusinessToken: () => account.token, getSessionCookie: () => account.cookie }));

import { forgetAuthenticatedMedia, loadAuthenticatedMedia, resetMediaCache } from "./mediaCache";
import { invalidateSessionResources } from "./sessionBoundary";

describe("authenticated batch media cache", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    resetMediaCache();
    account.token = "account-a";
    account.cookie = "";
    taro.downloadFile = undefined;
    taro.getFileSystemManager.mockReturnValue({
      writeFile: vi.fn(({ success }: { success: () => void }) => success()),
    });
    api.apiRequest.mockResolvedValue(new Uint8Array([1, 2, 3]).buffer);
  });

  it("downloads relative media with arraybuffer and writes it below USER_DATA_PATH", async () => {
    const result = await loadAuthenticatedMedia("/api/media/asset-1.png", "png");

    expect(api.apiRequest).toHaveBeenCalledWith("/api/media/asset-1.png", { responseType: "arraybuffer", recoverSession: false });
    expect(taro.getFileSystemManager().writeFile).toHaveBeenCalledWith(expect.objectContaining({
      filePath: expect.stringMatching(/^\/user-data\/yzcm-media-[a-f0-9]+\.png$/),
      data: expect.any(ArrayBuffer),
    }));
    expect(result.path).toMatch(/^\/user-data\/yzcm-media-[a-f0-9]+\.png$/);
  });
  it("cookie-only sessions download protected video and never leak cookies to external origins", async () => {
    account.token = ""; account.cookie = "ycm_session=private";
    taro.downloadFile = vi.fn(options => { options.success({ statusCode: 200, tempFilePath: "/tmp/cookie-video.mp4" }); return { abort: vi.fn() }; });
    await loadAuthenticatedMedia("/api/studio/assets/a/content", "mp4");
    expect(taro.downloadFile.mock.calls[0][0].header).toMatchObject({ Cookie: "ycm_session=private" });
    await loadAuthenticatedMedia("https://external.example/video.mp4", "mp4");
    expect(taro.downloadFile.mock.calls[1][0].header).toEqual({});
  });

  it("reuses the same cached media without requesting it twice", async () => {
    const first = await loadAuthenticatedMedia("/api/media/video-1.mp4", "mp4");
    const second = await loadAuthenticatedMedia("/api/media/video-1.mp4", "mp4");

    expect(second).toEqual({ path: first.path, fromCache: true });
    expect(api.apiRequest).toHaveBeenCalledTimes(1);
  });

  it("retry discards only the current media file and downloads a fresh copy", async () => {
    const unlink = vi.fn();
    taro.getFileSystemManager.mockReturnValue({ unlink });
    let sequence = 0;
    taro.downloadFile = vi.fn(options => { options.success({ statusCode: 200, tempFilePath: `/tmp/video-${++sequence}.mp4` }); return { abort: vi.fn() }; });
    const first = await loadAuthenticatedMedia("/api/media/a.mp4", "mp4");
    const other = await loadAuthenticatedMedia("/api/media/b.mp4", "mp4");
    forgetAuthenticatedMedia("/api/media/a.mp4");
    const retried = await loadAuthenticatedMedia("/api/media/a.mp4", "mp4");
    expect(retried).toEqual({ path: "/tmp/video-3.mp4", fromCache: false });
    expect(retried.path).not.toBe(first.path);
    expect(await loadAuthenticatedMedia("/api/media/b.mp4", "mp4")).toEqual({ ...other, fromCache: true });
    expect(unlink).toHaveBeenCalledWith(expect.objectContaining({ filePath: first.path }));
    expect(unlink).not.toHaveBeenCalledWith(expect.objectContaining({ filePath: other.path }));
  });

  it("does not expose the previous account file after logout and login", async () => {
    const first = await loadAuthenticatedMedia("/api/media/private.mp4", "mp4");
    account.token = "account-b";
    invalidateSessionResources();
    const second = await loadAuthenticatedMedia("/api/media/private.mp4", "mp4");
    expect(second.fromCache).toBe(false);
    expect(second.path).not.toBe(first.path);
    expect(api.apiRequest).toHaveBeenCalledTimes(2);
  });

  it("rejects late private media arriving after the account changes", async () => {
    let resolve!: (value: ArrayBuffer) => void;
    api.apiRequest.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
    const pending = loadAuthenticatedMedia("/api/media/private.mp4", "mp4");
    account.token = "account-b";
    invalidateSessionResources();
    resolve(new ArrayBuffer(3));
    await expect(pending).rejects.toThrow(/账号|登录/);
    expect(taro.getFileSystemManager().writeFile).not.toHaveBeenCalled();
  });

  it("downloads large video directly to a native temporary file with same-origin authentication", async () => {
    taro.downloadFile = vi.fn((options) => { options.success({ statusCode: 200, tempFilePath: "/tmp/native-video.mp4" }); return { abort: vi.fn() }; });
    const result = await loadAuthenticatedMedia("/api/media/long.mp4", "mp4");
    expect(result.path).toBe("/tmp/native-video.mp4");
    expect(taro.downloadFile).toHaveBeenCalledWith(expect.objectContaining({ header: { Authorization: "Bearer account-a" }, timeout: 120000 }));
    expect(api.apiRequest).not.toHaveBeenCalled();
    expect(taro.getFileSystemManager().writeFile).not.toHaveBeenCalled();
  });

  it("falls back to the authenticated API request when WeChat blocks the download-file domain", async () => {
    taro.downloadFile = vi.fn((options) => {
      options.fail({ errMsg: "downloadFile:fail createDownloadTask:fail url not in domain list" });
      return { abort: vi.fn() };
    });

    const result = await loadAuthenticatedMedia("/api/batch-factory/v11/batches/batch-1/merge-media/merge-1", "mp4");

    expect(api.apiRequest).toHaveBeenCalledWith("/api/batch-factory/v11/batches/batch-1/merge-media/merge-1", { responseType: "arraybuffer", recoverSession: false, timeout: 120000 });
    expect(taro.getFileSystemManager().writeFile).toHaveBeenCalledWith(expect.objectContaining({
      filePath: expect.stringMatching(/^\/user-data\/yzcm-media-[a-f0-9]+\.mp4$/),
      data: expect.any(ArrayBuffer),
    }));
    expect(result.path).toMatch(/^\/user-data\/yzcm-media-[a-f0-9]+\.mp4$/);
  });

  it("removes a native file that arrives after the download deadline", async () => {
    vi.useFakeTimers();
    try {
      let options: any;
      const unlink = vi.fn();
      const abort = vi.fn();
      taro.getFileSystemManager.mockReturnValue({ unlink });
      taro.downloadFile = vi.fn((value) => { options = value; return { abort }; });
      const pending = loadAuthenticatedMedia("/api/media/late.mp4", "mp4");
      const rejection = expect(pending).rejects.toThrow(/超时/);
      await vi.advanceTimersByTimeAsync(120000);
      await rejection;
      options.success({ statusCode: 200, tempFilePath: "/tmp/late.mp4" });
      expect(abort).toHaveBeenCalledOnce();
      expect(unlink).toHaveBeenCalledWith(expect.objectContaining({ filePath: "/tmp/late.mp4" }));
    } finally { vi.useRealTimers(); }
  });
});
