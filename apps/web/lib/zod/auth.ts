import { z } from "zod";

/**
 * 登录表单 schema；字段约束与服务端 binding 保持一致（agent.md §8）。
 */
export const loginSchema = z.object({
  username: z
    .string()
    .trim()
    .min(1, "请输入用户名")
    .max(64, "用户名不能超过 64 个字符"),
  password: z
    .string()
    .min(1, "请输入密码")
    .max(256, "密码不能超过 256 个字符"),
});

/** 登录表单的值类型。 */
export type LoginInput = z.infer<typeof loginSchema>;
