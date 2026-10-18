"use client";

import { SWRConfig } from "swr";
import type { ReactNode } from "react";

/**
 * 管理端 SWR 全局配置（agent.md §8：用 SWR，不引入 Redux）。
 *
 * 管理端数据一律 no-store，因此关闭聚焦重验证与自动重试，避免闪烁。
 */
export function SWRProvider({ children }: { children: ReactNode }) {
  return (
    <SWRConfig
      value={{
        revalidateOnFocus: false,
        revalidateOnReconnect: false,
        shouldRetryOnError: false,
        dedupingInterval: 5_000,
      }}
    >
      {children}
    </SWRConfig>
  );
}
