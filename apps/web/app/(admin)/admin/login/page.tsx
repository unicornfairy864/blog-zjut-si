"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useSWRConfig } from "swr";

import { adminApi } from "@/lib/api/admin";
import { ApiError } from "@/lib/api/fetcher";
import { loginSchema, type LoginInput } from "@/lib/zod/auth";
import { swrKeys } from "@/lib/swr/keys";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/ui/shadcn/card";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";

/**
 * 管理端登录页（/admin/login）。
 *
 * 契约说明：8641 路由表只放行 /admin*，所以登录页必须挂在 /admin 之下；
 * 它不在 (protected) 组里，因此不会被会话守卫拦下（agent.md §8）。
 */
export default function AdminLoginPage() {
  const router = useRouter();
  const { mutate } = useSWRConfig();
  const [formError, setFormError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "" },
  });

  async function onSubmit(values: LoginInput) {
    setFormError(null);
    try {
      await adminApi.login(values);
      // 登录成功后立刻刷新会话缓存，拿到 csrfToken 再进管理页。
      await mutate(swrKeys.adminMe);
      router.replace("/admin");
    } catch (error) {
      setFormError(
        error instanceof ApiError ? error.message : "登录失败，请稍后重试",
      );
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/40 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>管理端登录</CardTitle>
          <CardDescription>
            仅本站管理员可用；请通过 SSH 隧道访问 127.0.0.1:8641。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4" noValidate>
            <div className="space-y-2">
              <Label htmlFor="username">用户名</Label>
              <Input
                id="username"
                autoComplete="username"
                aria-invalid={Boolean(errors.username)}
                aria-describedby={errors.username ? "username-error" : undefined}
                {...register("username")}
              />
              {errors.username ? (
                <p id="username-error" className="text-xs text-destructive">
                  {errors.username.message}
                </p>
              ) : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                aria-invalid={Boolean(errors.password)}
                aria-describedby={errors.password ? "password-error" : undefined}
                {...register("password")}
              />
              {errors.password ? (
                <p id="password-error" className="text-xs text-destructive">
                  {errors.password.message}
                </p>
              ) : null}
            </div>

            {formError ? (
              <Alert variant="destructive">
                <AlertDescription>{formError}</AlertDescription>
              </Alert>
            ) : null}

            <Button type="submit" className="w-full" disabled={isSubmitting}>
              {isSubmitting ? "登录中…" : "登录"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
