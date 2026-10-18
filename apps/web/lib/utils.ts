import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** 合并 Tailwind 类名，后者覆盖前者的冲突工具类。 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const dateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

const timeFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

/**
 * 把 Unix 秒格式化为站点时区（Asia/Shanghai）的日期时间（agent.md §4）。
 *
 * 0 / null 表示"未设置"，返回破折号。
 */
export function formatDateTime(unix?: number | null): string {
  if (!unix) {
    return "—";
  }
  return dateTimeFormatter.format(new Date(unix * 1000));
}

/** 只格式化时分秒，用于"已保存 19:05:12"这类提示。 */
export function formatTime(unix?: number | null): string {
  if (!unix) {
    return "—";
  }
  return timeFormatter.format(new Date(unix * 1000));
}
