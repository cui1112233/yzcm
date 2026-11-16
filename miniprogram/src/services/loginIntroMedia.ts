import Taro from "@tarojs/taro";
import { apiRequest } from "./api";

function writeFile(filePath: string, data: ArrayBuffer) {
  return new Promise<void>((resolve, reject) => {
    Taro.getFileSystemManager().writeFile({
      filePath,
      data,
      success: () => resolve(),
      fail: reject,
    });
  });
}

export async function loadLoginIntroMedia(url: string) {
  const root = String(Taro.env?.USER_DATA_PATH || "").trim();
  if (!root) throw new Error("小程序临时目录不可用，无法加载入场动画");
  const data = await apiRequest<ArrayBuffer>(url, {
    responseType: "arraybuffer",
    recoverSession: false,
    timeout: 20000,
  });
  if (!(data instanceof ArrayBuffer) || !data.byteLength) throw new Error("入场动画文件无效");
  const path = `${root}/yzcm-login-intro.mp4`;
  await writeFile(path, data);
  return path;
}
