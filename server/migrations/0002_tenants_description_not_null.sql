-- 修正 tenants.description 可空导致的 scan NULL 问题：
-- 业务语义上 description 允许为空，但不允许为 NULL。
UPDATE tenants SET description = '' WHERE description IS NULL;

ALTER TABLE tenants
    ALTER COLUMN description SET DEFAULT '',
    ALTER COLUMN description SET NOT NULL;
