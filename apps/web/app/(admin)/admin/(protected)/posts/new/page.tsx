"use client";

import { PostEditor } from "@/ui/admin/post-editor";

/** 新建文章（/admin/posts/new）；首次保存才落库为草稿。 */
export default function AdminPostNewPage() {
  return <PostEditor postId={null} />;
}
