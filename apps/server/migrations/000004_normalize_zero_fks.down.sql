-- 000004_normalize_zero_fks.down.sql
-- 数据归一化不可逆，也不该还原：0 不是合法的 media.id / series.id。
-- 这个空操作只是给 golang-migrate 一个可 step down 的版本（每个版本都要有 down 脚本）。
SELECT 1;
