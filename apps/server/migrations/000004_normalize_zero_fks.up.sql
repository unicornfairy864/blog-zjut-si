-- 000004_normalize_zero_fks.up.sql
-- 把外键列上的 0 归一到 NULL（agent.md §5：0 表示"清空/未设置"）。
--
-- 来源：Create 早先把接口层的 0 当真外键写进库（Update 早已归一成 NULL），于是
-- cover_media_id / og_media_id 出现了 0。0 不是合法的 media.id 或 series.id，
-- 留着会让管理端详情回一个非法 id，编辑器读到它以后前端校验直接判不合法——
-- 那篇文章从此保存不了（报"请先补全标题等必填项"）。
-- 幂等：再跑一次没有行可改。
UPDATE posts SET cover_media_id = NULL WHERE cover_media_id = 0;
UPDATE posts SET og_media_id = NULL WHERE og_media_id = 0;
UPDATE posts SET series_id = NULL WHERE series_id = 0;
