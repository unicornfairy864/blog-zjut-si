"use client";

/**
 * 管理端仪表盘。
 *
 * D3 范围：只展示已接通的能力；统计卡片随 D12（stats）补齐。
 */
export default function AdminDashboardPage() {
  return (
    <section className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">仪表盘</h1>
      <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
        鉴权链路已接通：argon2id 口令、7 天滑动会话、CSRF 双提交、按用户名锁定。
        文章、媒体、评论与设置页在后续模块接入。
      </p>
    </section>
  );
}
