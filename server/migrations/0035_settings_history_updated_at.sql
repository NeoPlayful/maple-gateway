-- settings_history 增加 updated_at：与 settings 表时间戳字段对齐。
--
-- 背景：settings_history 是只追加表（每次覆盖前追一行旧值），行本身不会被修改，
-- 故 updated_at 与 created_at 恒等。此处按需求补齐该列，保持两表字段一致。
--
-- 回填策略：存量行用 created_at 回填；必须先加可空列、回填、再设 DEFAULT/NOT NULL，
-- 否则 DEFAULT now() 会把存量行填成迁移执行时刻。
--
-- 幂等：可重复执行。

ALTER TABLE settings_history ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;

UPDATE settings_history SET updated_at = created_at WHERE updated_at IS NULL;

ALTER TABLE settings_history ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE settings_history ALTER COLUMN updated_at SET NOT NULL;
