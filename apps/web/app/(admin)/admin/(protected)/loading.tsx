import { Loading } from "@/ui/shared/loading";

/**
 * 管理端受保护区的等待态。
 *
 * 管理页整树是客户端组件、数据走 SWR，跳转本身很快；这个兜底主要覆盖
 * RSC 段加载与首屏会话确认之间的空档，避免出现"白屏一下再出现内容"。
 */
export default function AdminProtectedLoading() {
  return <Loading label="正在加载…" />;
}
