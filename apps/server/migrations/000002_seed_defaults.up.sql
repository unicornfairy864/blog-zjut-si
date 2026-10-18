-- 000002_seed_defaults.up.sql
-- settings 必含项（agent.md §4）：content_version / img_base_url / site_timezone / visitor_salt。
-- visitor_salt 每库随机生成一次，用于 visitor_hash 与 ip_hash 加盐。
INSERT INTO settings (key, value_json, updated_at) VALUES
    ('content_version', '1', CAST(strftime('%s', 'now') AS INTEGER)),
    ('img_base_url', '""', CAST(strftime('%s', 'now') AS INTEGER)),
    ('site_timezone', '"Asia/Shanghai"', CAST(strftime('%s', 'now') AS INTEGER)),
    ('visitor_salt', '"' || hex(randomblob(32)) || '"', CAST(strftime('%s', 'now') AS INTEGER))
ON CONFLICT (key) DO NOTHING;
