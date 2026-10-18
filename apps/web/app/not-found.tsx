import Link from "next/link";

/** 404 页：不返回任何内部信息（agent.md §16 G4）。 */
export default function NotFound() {
  return (
    <main className="container flex min-h-[60vh] flex-col items-center justify-center gap-4 text-center">
      <p className="text-sm font-medium text-muted-foreground">404</p>
      <h1 className="text-2xl font-semibold">没有找到这个页面</h1>
      <Link href="/" className="text-sm underline underline-offset-4">
        回到首页
      </Link>
    </main>
  );
}
