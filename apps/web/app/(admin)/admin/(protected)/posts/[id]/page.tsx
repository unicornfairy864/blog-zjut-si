"use client";

import { use } from "react";

import { PostEditor } from "@/ui/admin/post-editor";

/** 编辑文章（/admin/posts/{id}）。 */
export default function AdminPostEditPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const postId = Number.parseInt(id, 10);

  if (!Number.isInteger(postId) || postId <= 0) {
    return <p className="text-sm text-destructive">文章 ID 非法。</p>;
  }

  return <PostEditor postId={postId} />;
}
