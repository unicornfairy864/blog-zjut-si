import { describe, expect, it } from "vitest";

import { describeStorageKind } from "@/lib/api/storage";

/**
 * 存储对账的纯函数（agent.md §10）。
 *
 * 只覆盖"类型 → 中文标签"的映射：接口调用与表格渲染需要网络 / DOM，不在这里覆盖。
 */
describe("describeStorageKind", () => {
  it("把三种磁盘文件类型写成中文", () => {
    expect(describeStorageKind("original")).toBe("原图");
    expect(describeStorageKind("variant")).toBe("变体");
    expect(describeStorageKind("attachment")).toBe("附件");
  });

  it("未知类型原样回显，避免后端新增类型时界面变空白", () => {
    expect(describeStorageKind("thumbnail")).toBe("thumbnail");
    expect(describeStorageKind("")).toBe("");
  });
});
