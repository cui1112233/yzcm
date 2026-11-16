import { beforeEach, describe, expect, it, vi } from "vitest";

const taro = vi.hoisted(() => ({
  env: { USER_DATA_PATH: "/user-data" },
  getFileSystemManager: vi.fn(),
}));
const api = vi.hoisted(() => ({ apiRequest: vi.fn() }));

vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("./api", () => api);

import { loadLoginIntroMedia } from "./loginIntroMedia";

describe("login intro media", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    taro.getFileSystemManager.mockReturnValue({
      writeFile: vi.fn(({ success }: { success: () => void }) => success()),
    });
    api.apiRequest.mockResolvedValue(new Uint8Array([1, 2, 3]).buffer);
  });

  it("loads the public video through request and writes a local MP4", async () => {
    const path = await loadLoginIntroMedia("https://yzcm.site/assets/login-intro.mp4");

    expect(api.apiRequest).toHaveBeenCalledWith("https://yzcm.site/assets/login-intro.mp4", {
      responseType: "arraybuffer",
      recoverSession: false,
      timeout: 20000,
    });
    expect(taro.getFileSystemManager().writeFile).toHaveBeenCalledWith(expect.objectContaining({
      filePath: "/user-data/yzcm-login-intro.mp4",
      data: expect.any(ArrayBuffer),
    }));
    expect(path).toBe("/user-data/yzcm-login-intro.mp4");
  });
});
