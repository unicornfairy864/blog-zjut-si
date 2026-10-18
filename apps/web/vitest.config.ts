import path from "node:path";
import { defineConfig } from "vitest/config";

/**
 * 单元/组件测试配置（agent.md §17.2：vitest + testing-library）。
 *
 * 测试由验收方执行；本文件只提供 `pnpm test` 的入口与别名。
 */
export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.{ts,tsx}"],
    setupFiles: ["tests/setup.ts"],
    passWithNoTests: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
});
