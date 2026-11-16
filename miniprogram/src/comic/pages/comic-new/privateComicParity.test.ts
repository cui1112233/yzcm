import { describe, expect, test } from "vitest";
import {
  PUBLIC_COMIC_ASPECTS,
  canEnterPrivateComicStage,
  privateComicAction,
  type PrivateComicStageInput,
} from "./privateComicParity";

const empty: PrivateComicStageInput = {
  sourceLength: 0,
  minSourceLength: 80,
  styleId: "",
  batchId: "",
  assetsCount: 0,
  pagesCount: 0,
  submitted: false,
};

describe("private comic public parity", () => {
  test("offers the public aspect choices in the same order", () => {
    expect(PUBLIC_COMIC_ASPECTS).toEqual([
      { value: "16:9", label: "16:9" },
      { value: "9:16", label: "9:16" },
      { value: "1:1", label: "1:1" },
    ]);
  });

  test("allows settings and requires a created batch before entering assets", () => {
    expect(canEnterPrivateComicStage("settings", empty)).toBe(true);
    expect(canEnterPrivateComicStage("assets", empty)).toBe(false);
    expect(canEnterPrivateComicStage("assets", { ...empty, batchId: "b1" })).toBe(true);
  });

  test("enables the settings action only with enough source and a selected style", () => {
    expect(privateComicAction("settings", empty).enabled).toBe(false);
    expect(privateComicAction("settings", { ...empty, sourceLength: 79, styleId: "s1" }).enabled).toBe(false);
    expect(privateComicAction("settings", { ...empty, sourceLength: 80 }).enabled).toBe(false);
    expect(privateComicAction("settings", { ...empty, sourceLength: 80, styleId: "s1" })).toEqual({
      label: "创建项目并提取资产", enabled: true,
    });
  });

  test("requires only a created batch before entering config (assets are not a gate)", () => {
    expect(canEnterPrivateComicStage("config", { ...empty, assetsCount: 1 })).toBe(false);
    expect(canEnterPrivateComicStage("config", { ...empty, batchId: "b1" })).toBe(true);
    expect(privateComicAction("assets", { ...empty, batchId: "b1" })).toEqual({
      label: "继续：生成配置确认", enabled: true,
    });
  });

  test("requires submission and a batch before entering render", () => {
    expect(canEnterPrivateComicStage("render", { ...empty, batchId: "b1" })).toBe(false);
    expect(canEnterPrivateComicStage("render", { ...empty, submitted: true })).toBe(false);
    expect(canEnterPrivateComicStage("render", { ...empty, batchId: "b1", submitted: true })).toBe(true);
  });

  test("allows submitting config with or without extracted assets", () => {
    expect(privateComicAction("config", empty).enabled).toBe(false);
    expect(privateComicAction("config", { ...empty, batchId: "b1" })).toEqual({
      label: "确认配置，开始分页出图", enabled: true,
    });
  });

  test("requires actual pages before entering results or enabling their action", () => {
    expect(canEnterPrivateComicStage("result", { ...empty, batchId: "b1", submitted: true })).toBe(false);
    expect(canEnterPrivateComicStage("result", { ...empty, pagesCount: 1 })).toBe(true);
    expect(privateComicAction("render", empty).enabled).toBe(false);
    expect(privateComicAction("render", { ...empty, pagesCount: 1 })).toEqual({
      label: "查看生成结果", enabled: true,
    });
    expect(privateComicAction("result", empty)).toEqual({ label: "重新创作", enabled: true });
  });

  test("disables every stage action while busy without hiding accessible stages", () => {
    const ready = { ...empty, sourceLength: 80, styleId: "s1", batchId: "b1", assetsCount: 1, pagesCount: 1, submitted: true, busy: true };
    for (const stage of ["settings", "assets", "config", "render", "result"] as const) {
      expect(canEnterPrivateComicStage(stage, ready)).toBe(true);
      expect(privateComicAction(stage, ready).enabled).toBe(false);
    }
  });
});
