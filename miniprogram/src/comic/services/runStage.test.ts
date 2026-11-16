import { describe, expect, it } from "vitest";
import {
  COMIC_RUN_STEPS, assetImagesSkipped, comicRunSubmitGate, estimateComicPages, estimateComicRunMinutes,
  estimateComicRunText, runStageLabel, runStageStep, runStageView, runStateLabel,
} from "./runStage";

describe("runStage 映射", () => {
  it("按权威清单给出中文标签与步骤", () => {
    expect(runStageLabel("comic_images")).toBe("漫画图片");
    expect(runStageStep("comic_images")).toBe(2);
    expect(runStageStep("comic_assets")).toBe(0);
    expect(runStageStep("comic_bootstrap")).toBe(1);
    expect(runStageStep("comic_collection")).toBe(3);
    expect(runStageStep("completed")).toBe(4);
    expect(COMIC_RUN_STEPS).toHaveLength(4);
  });

  it("清单外的 stage 原样显示，不推测步骤", () => {
    expect(runStageLabel("comic_unknown_stage")).toBe("comic_unknown_stage");
    expect(runStageStep("comic_unknown_stage")).toBe(-1);
  });

  it("实测补充值 ready_for_upload 归到四步全完成", () => {
    expect(runStageStep("ready_for_upload")).toBe(4);
    expect(runStageLabel("ready_for_upload")).toBe("漫画合集已完成，等待上传");
  });

  it("stopped 是终态，保留服务端原文提示", () => {
    const view = runStageView({
      state: "stopped",
      books: [{ bookId: "book-1", stage: "stopped", status: "stopped", message: "服务重启，自动化已中断，请重新立即执行。" }],
    }, "book-1");
    expect(view.terminal).toBe(true);
    expect(view.notice).toBe("服务重启，自动化已中断，请重新立即执行。");
    // stopped 不在 stage 清单里，四步条保持不动
    expect(view.step).toBe(-1);
  });

  it("空响应不报错也不当结束", () => {
    const view = runStageView(null, "book-1");
    expect(view.terminal).toBe(false);
    expect(view.label).toBe("");
    expect(view.step).toBe(-1);
  });

  it("按书的 stage 计算当前步骤", () => {
    const view = runStageView({ state: "running", books: [{ bookId: "book-2", stage: "comic_images" }] }, "book-2");
    expect(view.terminal).toBe(false);
    expect(view.step).toBe(2);
    expect(view.label).toBe("漫画图片");
  });
});

describe("资产图跳过识别", () => {
  it("服务端跳过资产图不算失败", () => {
    expect(assetImagesSkipped({
      state: "running",
      books: [{ stage: "comic_images", logs: [{ stage: "comic_asset_images", message: "因资产图片开关关闭，已跳过 2 个漫画资产参考图生成" }] }],
    })).toBe(true);
  });

  it("没有跳过日志时为 false", () => {
    expect(assetImagesSkipped({ state: "running", books: [{ stage: "comic_images", logs: [] }] })).toBe(false);
    expect(assetImagesSkipped(null)).toBe(false);
  });
});

describe("提交区页数与时长估算", () => {
  it("181 字给 2 页与全程区间，固定加 1.5 分钟前置耗时", () => {
    expect(estimateComicPages(181)).toBe(2);
    expect(estimateComicRunText(181)).toBe("预计 2 页 · 约 6~6.5 分钟（估算）");
  });

  it("更短正文按 1 页给全程区间", () => {
    expect(estimateComicPages(100)).toBe(1);
    expect(estimateComicRunText(100)).toBe("预计 1 页 · 约 6~6.5 分钟（估算）");
  });

  it("并发 4 路按轮次折算，接近上限时长封顶", () => {
    expect(estimateComicRunMinutes(4)).toEqual({ low: 6, high: 6.5, waves: 1 });
    expect(estimateComicRunMinutes(5)).toEqual({ low: 10.5, high: 11.5, waves: 2 });
    expect(estimateComicPages(4000)).toBe(35);
    expect(estimateComicRunMinutes(35)).toEqual({ low: 42, high: 46.5, waves: 9 });
    expect(estimateComicRunText(0)).toBe("");
  });
});

describe("页数预估边界", () => {
  it("按 90~100 字/页估算", () => {
    expect(estimateComicPages(0)).toBe(0);
    expect(estimateComicPages(90)).toBe(1);
    expect(estimateComicPages(181)).toBe(2);
    expect(estimateComicPages(3500)).toBe(35);
  });
});

describe("提交闸门", () => {
  it("活动任务不提交：与后端 activeAutomationJob 的四个状态一致", () => {
    ["running", "scheduled", "paused", "needs_attention"].forEach((state) => {
      expect(comicRunSubmitGate(state)).toBe("active");
      expect(runStateLabel(state)).not.toBe("");
    });
  });

  it("终态再提交要二次确认，覆盖 cancelled/canceled 两种拼写", () => {
    ["completed", "failed", "cancelled", "canceled", "partial", "stopped"].forEach((state) => {
      expect(comicRunSubmitGate(state)).toBe("finished");
    });
    expect(comicRunSubmitGate(" COMPLETED ")).toBe("finished");
  });

  it("没有任务或状态未知时按原流程提交", () => {
    expect(comicRunSubmitGate("")).toBe("idle");
    expect(comicRunSubmitGate("idle")).toBe("idle");
    expect(comicRunSubmitGate("unknown_value")).toBe("idle");
    expect(runStateLabel("unknown_value")).toBe("unknown_value");
  });
});
