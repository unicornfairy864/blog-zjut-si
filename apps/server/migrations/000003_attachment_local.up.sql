-- 000003_attachment_local.up.sql
-- 附件改由本站自己托管（agent.md §10）：新增 storage_key 与 mime。
--
-- storage_key 形如 {sha[:2]}/{sha256}，物理文件是 {file.dir}/{storage_key}{ext}；
-- 公开端一律经 GET /api/attachments/{id}/go 出口，磁盘目录不对外直出。
-- 历史数据（只有 share_url 的外链附件）storage_key 留空，公开端仍按 302 跳转。
ALTER TABLE post_attachments ADD COLUMN storage_key TEXT NOT NULL DEFAULT '';
ALTER TABLE post_attachments ADD COLUMN mime TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_post_attachments_storage_key ON post_attachments (storage_key);
