-- 修正 instances.version 可空导致的 scan NULL 问题：
-- 业务上版本标签可为空，但 model 使用 string，统一为非 NULL 空串。
UPDATE instances SET version = '' WHERE version IS NULL;

ALTER TABLE instances
    ALTER COLUMN version SET DEFAULT '',
    ALTER COLUMN version SET NOT NULL;
