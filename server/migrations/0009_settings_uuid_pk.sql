-- Phase 3 Ent 化：settings 增加单列 UUID 主键（Ent 建模需要单列主键）。
-- 原 (section, key) 复合主键降为唯一约束，语义不变。
-- 幂等：可重复执行。

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='settings' AND column_name='id') THEN
        ALTER TABLE settings ADD COLUMN id UUID;
    END IF;
END $$;

-- 回填存量行 id（若整列为空）。
UPDATE settings SET id = gen_random_uuid() WHERE id IS NULL;

-- 若 id 尚未设为主键：先去掉复合主键，再设 id 主键，并补 (section,key) 唯一。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint
               WHERE conname='settings_pkey' AND conrelid='settings'::regclass
                 AND conkey IS NOT NULL AND array_length(conkey,1) > 1) THEN
        ALTER TABLE settings DROP CONSTRAINT settings_pkey;
        ALTER TABLE settings ADD PRIMARY KEY (id);
        ALTER TABLE settings ADD CONSTRAINT settings_section_key_key UNIQUE (section, key);
    END IF;
END $$;

ALTER TABLE settings ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE settings ALTER COLUMN id SET NOT NULL;
