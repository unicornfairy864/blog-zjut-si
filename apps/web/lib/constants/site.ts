/**
 * 站点级常量。文案与 URL 只在这里改，避免散落在组件里。
 */
export const SITE = {
  name: "zjut.si",
  title: "zjut.si",
  description: "陈松的个人博客：Web、算法竞赛、嵌入式开发",
  author: "unicornfairy864",
  url: "https://blog.zjut.si",
  locale: "zh-CN",
  /** 个人主页：导航栏头像指向这里（本站是 blog.zjut.si）。 */
  homeUrl: "https://www.zjut.si",
  /** 导航栏品牌文字：指向本站首页，与 SITE.name 分开是因为它要大写成域名形态。 */
  navLabel: "BLOG.ZJUT.SI",
} as const;
