import { apiBaseUrl } from "@/lib/constants/env";

import { CSRF_HEADER, clearCSRFToken, getCSRFToken } from "./csrf";

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

  /**
   * 是否是"未登录 / 会话失效"。
   *
   * v1.14 起业务错误一律 HTTP 200，只按 code 判定；`status` 仅作诊断信息保留。
   */
  get isUnauthorized(): boolean {
    return this.code === 1000;
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

/** 字段级校验错误（§5：失败响应的 `data` 是 `[{field,message}]`）。 */
export interface ApiFieldError {
  field: string;
  message: string;
}

/**
 * 从 ApiError 里取出字段错误数组。
 *
 * `data` 只有校验失败时才是这个形状（无附加数据是 `null`、媒体引用冲突是清单），
 * 所以这里做形状判断而不是强转。
 */
export function apiFieldErrors(error: unknown): ApiFieldError[] {
  if (!(error instanceof ApiError) || !Array.isArray(error.details)) {
    return [];
  }

  const fields: ApiFieldError[] = [];
  for (const item of error.details) {
    if (item === null || typeof item !== "object") {
      continue;
    }
    const candidate = item as Partial<ApiFieldError>;
    if (typeof candidate.field === "string" && typeof candidate.message === "string") {
      fields.push({ field: candidate.field, message: candidate.message });
    }
  }
  return fields;
}

/**
 * 把一次失败转成给用户看的一句话。
 *
 * 有字段错误就逐条列出来（`field：message`），否则退回信封 `msg`：
 * 只显示"参数校验失败"等于什么都没说，用户无从下手。
 */
export function describeApiError(
  error: unknown,
  fallback = "请求失败，请稍后重试",
): string {
  const fields = apiFieldErrors(error);
  if (fields.length > 0) {
    return fields.map((item) => `${item.field}：${item.message}`).join("；");
  }
  return error instanceof ApiError ? error.message : fallback;
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
    // 非信封响应（nginx 502 HTML、上游宕机等）：只能用 HTTP 状态兜底。
    if (!response.ok) {
      throw new ApiError({
        code: 1006,
        status: response.status,
        message: `请求失败（HTTP ${response.status}）`,
      });
    }
    return undefined as T;
  }

  // 业务成败只看信封里的 code：v1.14 起失败也是 HTTP 200，`!response.ok`
  // 只兜住"信封 + 非 2xx"的意外组合（例如 NoRoute 的 404 + 1002）。
  if (envelope.code !== 0 || !response.ok) {
    if (envelope.code === 1000) {
      handleSessionExpired(path);
    }
    throw new ApiError({
      code: envelope.code,
      status: response.status,
      message: envelope.msg || "请求失败",
      details: envelope.data,
    });
  }
  return envelope.data;
}

/**
 * 会话过期时的统一处理：清掉 CSRF 并回登录页。
 *
 * 管理页是 CSR：会话过期后如果只弹一句"未登录或会话已失效"，用户会一直点同一个按钮。
 * 只在浏览器里、且只对管理接口生效（公开接口不该把人踢到后台登录页）。
 */
function handleSessionExpired(path: string): void {
  if (typeof window === "undefined") {
    return;
  }
  if (!path.startsWith("/api/admin/") || path.startsWith("/api/admin/auth/login")) {
    return;
  }
  if (window.location.pathname.startsWith("/admin/login")) {
    return;
  }
  clearCSRFToken();
  window.location.assign("/admin/login");
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
