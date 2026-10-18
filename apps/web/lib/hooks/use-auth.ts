"use client";

import useSWR from "swr";

import { adminApi, type MeResult } from "@/lib/api/admin";
import { ApiError } from "@/lib/api/fetcher";
import { swrKeys } from "@/lib/swr/keys";

/**
 * 当前管理端会话。
 *
 * 管理页 layout 先调用它：401 直接跳 /admin/login，避免先渲染再闪回（agent.md §8）。
 */
export function useAuth() {
  const { data, error, isLoading, mutate } = useSWR<MeResult, ApiError>(
    swrKeys.adminMe,
    () => adminApi.me(),
  );

  return {
    admin: data?.admin ?? null,
    csrfToken: data?.csrfToken ?? null,
    isLoading,
    isUnauthorized: error instanceof ApiError && error.isUnauthorized,
    error,
    mutate,
  };
}
