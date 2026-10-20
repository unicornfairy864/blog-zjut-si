-- 000003_attachment_local.down.sql
DROP INDEX IF EXISTS idx_post_attachments_storage_key;

ALTER TABLE post_attachments DROP COLUMN storage_key;
ALTER TABLE post_attachments DROP COLUMN mime;
