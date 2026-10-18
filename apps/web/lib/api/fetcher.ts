import { apiBaseUrl } from "@/lib/constants/env";

import { CSRF_HEADER, getCSRFToken } from "./csrf";

/** 统一响应信封（agent.md §5）。 */
export interface ApiEnvelope<T> {
  code: number;
  data: T;
  msg: string;
}

/** 业务错误：组件只捕获这一种类型，绝不解析原始响应。 */
export class ApiError extends Error {
  readonly code: number;
  readonly status: number;
  readonly details: unknown;

  constructor(params: {
    code: number;
    message: string;
    status: number;
    details?: unknown;
  }) {
    super(params.message);
    this.name = "ApiError";
    this.code = params.code;
    this.status = params.status;
    this.details = params.details ?? null;
  }

  /** 是否是"未登录 / 会话失效"。 */
  get isUnauthorized(): boolean {
    return this.status === 401 || this.code === 1000;
  }
}

/** 请求选项：json 与 body 二选一。 */
export interface ApiRequestInit
  extends Omit<RequestInit, "body" | "method"> {
  method?: string;
  body?: BodyInit;
  json?: unknown;
  timeoutMs?: number;
}

/**
 * 唯一的 fetch 出口：拼基址、带 cookie、注入 CSRF、解开信封、抛 ApiError。
 */
export async function apiFetch<T>(
  path: string,
  init: ApiRequestInit = {},
): Promise<T> {
  const { json, timeoutMs = 15_000, headers, ...rest } = init;

  const requestHeaders = new Headers(headers);
  requestHeaders.set("Accept", "application/json");

  let body = rest.body;
  if (json !== undefined) {
    requestHeaders.set("Content-Type", "application/json");
    body = JSON.stringify(json);
  }

  const method = (init.method ?? (json !== undefined ? "POST" : "GET")).toUpperCase();
  const isSafeMethod = method === "GET" || method === "HEAD" || method === "OPTIONS";
  if (!isSafeMethod) {
    const csrf = getCSRFToken();
    if (csrf) {
      requestHeaders.set(CSRF_HEADER, csrf);
    }
  }

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  let response: Response;
  try {
    response = await fetch(resolveUrl(path), {
      ...rest,
      method,
      headers: requestHeaders,
      body,
      signal: controller.signal,
      credentials: "same-origin",
      cache: "no-store",
    });
  } catch (cause) {
    throw new ApiError({
      code: 1006,
      status: 0,
      message: "网络异常，请稍后重试",
      details: cause,
    });
  } finally {
    clearTimeout(timer);
  }

  const text = await response.text();
  const envelope = parseEnvelope<T>(text);

  if (!envelope) {
    if (!response.ok) {
      throw new ApiError({
        code: 1006,
        status: response.status,
        message: `请求失败（HTTP ${response.status}）`,
      });
    }
    return undefined as T;
  }

  if (!response.ok || envelope.code !== 0) {
    throw new ApiError({
      code: envelope.code,
      status: response.status,
      message: envelope.msg || "请求失败",
      details: envelope.data,
    });
  }
  return envelope.data;
}

/** 拼出完整 URL；已经是绝对地址时直接使用。 */
function resolveUrl(path: string): string {
  if (/^https?:\/\//.test(path)) {
    return path;
  }
  return `${apiBaseUrl()}${path}`;
}

/** 只解析符合信封形状的 JSON，其他内容（HTML 错误页等）返回 null。 */
function parseEnvelope<T>(text: string): ApiEnvelope<T> | null {
  if (!text) {
    return null;
  }
  try {
    const parsed = JSON.parse(text) as Partial<ApiEnvelope<T>>;
    if (typeof parsed.code === "number" && typeof parsed.msg === "string") {
      return { code: parsed.code, data: parsed.data as T, msg: parsed.msg };
    }
    return null;
  } catch {
    return null;
  }
}
