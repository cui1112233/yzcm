import { defineConfig } from "@tarojs/cli";
import { resolveApiOrigin } from "./env";

const modeFlag = process.argv.findIndex((argument) => argument === "--mode");
const buildMode = modeFlag >= 0 ? process.argv[modeFlag + 1] : undefined;
const apiOrigin = resolveApiOrigin({ mode: buildMode });
const buildSha = process.env.MINIPROGRAM_BUILD_SHA || "unknown";
const buildDirty = process.env.MINIPROGRAM_BUILD_DIRTY === "true";

export default defineConfig({
  projectName: "yizhanchengming-wechat-miniprogram",
  date: "2026-09-25",
  designWidth: 375,
  deviceRatio: {
    375: 2,
    750: 1,
    828: 1.81,
  },
  sourceRoot: "src",
  outputRoot: "dist",
  framework: "react",
  compiler: {
    type: "webpack5",
    prebundle: { enable: false },
  },
  defineConstants: {
    __MINIPROGRAM_API_ORIGIN__: JSON.stringify(apiOrigin),
    __MINIPROGRAM_BUILD_SHA__: JSON.stringify(buildSha),
    __MINIPROGRAM_BUILD_DIRTY__: JSON.stringify(buildDirty),
  },
  plugins: ["@tarojs/plugin-platform-weapp"],
  mini: {
    imageUrlLoaderOption: { limit: true },
    postcss: {
      pxtransform: { enable: true },
    },
  },
});
