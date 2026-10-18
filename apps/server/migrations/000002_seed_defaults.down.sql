-- 000002_seed_defaults.down.sql
DELETE FROM settings WHERE key IN ('content_version', 'img_base_url', 'site_timezone', 'visitor_salt');
