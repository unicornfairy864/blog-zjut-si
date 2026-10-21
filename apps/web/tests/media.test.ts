import { describe, expect, it, vi } from "vitest";

import {
  mediaMarkdown,
  waitForMediaReady,
  type MediaItem,
} from "@/lib/api/media";

/** 造一个媒体条目；只填被测代码会读的字段。 */
function item(overrides: Partial<MediaItem> = {}): MediaItem {
  return {
    id: 1,
    sha256: "a".repeat(64),
    originalName: "photo.png",
    mime: "image/png",
    ext: "png",
    bytes: 1024,
    width: 8,
    height: 8,
    pixels: 64,
    alt: "",
    blurhash: "",
    status: "ready",
    error: "",
    url: "/uploads/aa/" + "a".repeat(64) + "/1600.webp",
    origUrl: "/uploads/aa/" + "a".repeat(64) + ".png",
    origArchived: false,
    origArchivedAt: null,
    variants: {},
    createdAt: 0,
    updatedAt: 0,
    ...overrides,
  };
}

describe("mediaMarkdown", () => {
  it("用 url 拼出 Markdown，alt 取去掉扩展名的原文件名", () => {
    expect(mediaMarkdown(item())).toBe(
      "![photo](/uploads/aa/" + "a".repeat(64) + "/1600.webp)",
    );
  });

  it("有人工 alt 时优先用它", () => {
    expect(mediaMarkdown(item({ alt: "封面图" }))).toContain("![封面图]");
  });

  it("alt 里的方括号与换行会破坏 Markdown，必须先去掉", () => {
    const markdown = mediaMarkdown(item({ alt: "图[1]\n换行" }));
    expect(markdown.startsWith("![图1 换行](")).toBe(true);
  });

  it("alt 被清空后退回「图片」，不留空方括号", () => {
    expect(mediaMarkdown(item({ originalName: "[].png", alt: "" }))).toContain(
      "![图片]",
    );
  });
});

describe("waitForMediaReady", () => {
  it("已经 ready 的条目直接返回，不轮询", async () => {
    const find = vi.fn(async () => item({ status: "ready" }));
    const sleep = vi.fn(async () => {});

    const got = await waitForMediaReady(1, { find, sleep });

    expect(got.status).toBe("ready");
    expect(find).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
  });

  it("processing 时轮询到 ready 才返回", async () => {
    const statuses = ["processing", "processing", "ready"];
    let index = 0;
    const find = vi.fn(async () => item({ status: statuses[index++] }));
    const sleep = vi.fn(async () => {});

    const got = await waitForMediaReady(1, { find, sleep });

    expect(got.status).toBe("ready");
    expect(find).toHaveBeenCalledTimes(3);
    expect(sleep).toHaveBeenCalledTimes(2);
  });

  it("条目暂时不在列表里也继续等（分页/时序抖动）", async () => {
    const results: Array<MediaItem | null> = [null, item({ status: "ready" })];
    let index = 0;
    const find = vi.fn(async () => results[index++]);
    const sleep = vi.fn(async () => {});

    const got = await waitForMediaReady(1, { find, sleep });

    expect(got.status).toBe("ready");
    expect(find).toHaveBeenCalledTimes(2);
  });

  it("转码失败立即抛错，并带上服务端给的原因", async () => {
    const find = vi.fn(async () =>
      item({ status: "failed", error: "不支持的图片格式" }),
    );

    await expect(waitForMediaReady(1, { find })).rejects.toThrow(
      "图片转码失败：不支持的图片格式",
    );
    expect(find).toHaveBeenCalledTimes(1);
  });

  it("一直 processing 会在超时后抛错，而不是空转", async () => {
    const find = vi.fn(async () => item({ status: "processing" }));
    // 假时钟：每次 sleep 前进 1 秒。
    let clock = 0;
    const sleep = vi.fn(async (ms: number) => {
      clock += ms;
    });

    await expect(
      waitForMediaReady(1, {
        find,
        sleep,
        now: () => clock,
        timeoutMs: 3_000,
        intervalMs: 1_000,
      }),
    ).rejects.toThrow("图片仍在转码");

    // 3 秒 / 1 秒 = 首次 + 3 次重试。
    expect(find).toHaveBeenCalledTimes(4);
  });

  it("假时钟不前进时靠次数上限退出", async () => {
    const find = vi.fn(async () => item({ status: "processing" }));
    const sleep = vi.fn(async () => {});

    await expect(
      waitForMediaReady(1, {
        find,
        sleep,
        now: () => 0,
        timeoutMs: 3_000,
        intervalMs: 1_000,
      }),
    ).rejects.toThrow("图片仍在转码");

    expect(find).toHaveBeenCalledTimes(4);
  });
});
