-- Settings 值历史：记录每次覆盖前的旧版本值，供按版本回滚。
-- 语义：settings 表每次 upsert（version+1）时，覆盖前旧行的
-- (section, key, version=旧版本, value=旧值) 追加到本表。
-- rollback 到 version=N 即取本表 section+key+version=N 的 value 写回 settings。
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS settings_history (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section    TEXT NOT NULL,
    key        TEXT NOT NULL,
    version    INT  NOT NULL,          -- 该值对应的 settings.version
    value      JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_settings_history_lookup
    ON settings_history(section, key, version);
