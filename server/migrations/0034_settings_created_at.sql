-- settings 增加 created_at：记录配置键首次建立时间。
--
-- 背景：原设计以 version==1 + updated_at 隐式代表创建时间，但 updated_at 在每次
-- upsert 都被覆盖，一旦改过一次，创建时间即永久丢失（settings_history 里 v1 行的
-- created_at 记的是首次覆盖发生的时刻，也偏晚）。此处补一列显式创建时间。
--
-- 回填策略：存量行无真值，只能近似——用 updated_at 回填（version>1 时偏晚，可接受）。
-- 故必须先加可空列、回填、再设 DEFAULT/NOT NULL，否则 DEFAULT now() 会把存量行
-- 直接填成迁移执行时刻，误差更大。
--
-- 幂等：可重复执行。

ALTER TABLE settings ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ;

-- 仅回填仍为空的行（首次迁移时覆盖存量；重复执行时为 no-op）。
UPDATE settings SET created_at = updated_at WHERE created_at IS NULL;

ALTER TABLE settings ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE settings ALTER COLUMN created_at SET NOT NULL;
