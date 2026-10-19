"use client";

import { TaxonomyManager } from "@/ui/admin/taxonomy-manager";

/** 管理端标签管理（/admin/tags）。 */
export default function AdminTagsPage() {
  return <TaxonomyManager kind="tags" />;
}
