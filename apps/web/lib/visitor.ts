/**
 * 访客标识（agent.md §5「访客标识」）。
 *
 * 公开端没有用户系统，公开 HTML 与公开读接口也一律不 Set-Cookie（§17.1），
 * 所以「点赞」和「PV 打点」这类需要区分同一个人的写请求，只能由前端在
 * `localStorage` 里生成一次随机串，随请求头 `X-Visitor-Id` 带上；
 * 服务端拿不到或拿到非法值时按 `ip|ua` 兜底。
 */
const VISITOR_ID_KEY = "zjut:vid";

/** 服务端接受的形状：8–64 位 `[A-Za-z0-9_-]`；不匹配就让服务端去兜底。 */
const VISITOR_ID_PATTERN = /^[A-Za-z0-9_-]{8,64}$/;

/**
 * 读取访客标识，没有就生成并写回。
 *
 * SSR 阶段没有 `localStorage`；浏览器里也可能因为隐私模式或第三方 iframe
 * 直接抛错。这两种情况都返回空串：宁可这一次请求不带标识，也不能让阅读页
 * 因为打点而崩掉。
 */
export function visitorId(): string {
  if (typeof window === "undefined") {
    return "";
  }
  try {
    const stored = window.localStorage.getItem(VISITOR_ID_KEY);
    // 手工改坏或旧版本留下的值会让服务端每次都回退到 ip|ua，
    // 点赞状态看起来就会"时灵时不灵"，所以形状不对就重新生成。
    if (stored !== null && VISITOR_ID_PATTERN.test(stored)) {
      return stored;
    }
    const generated = crypto.randomUUID();
    window.localStorage.setItem(VISITOR_ID_KEY, generated);
    return generated;
  } catch {
    return "";
  }
}

/** 访客标识请求头；拿不到标识时返回空对象，请求照常发出。 */
export function visitorHeaders(): Record<string, string> {
  const id = visitorId();
  return id ? { "X-Visitor-Id": id } : {};
}
