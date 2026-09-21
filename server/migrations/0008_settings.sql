-- Maple Gateway Phase 2 动态设置（S6 Settings）。
-- settings：运行时配置 KV，按 section 组织，版本化用于审计与回滚。
--
-- 列顺序即物理顺序（Postgres 无法事后调序）：id 在首，created_at 在 updated_at 左侧。
-- 0009 起改为 id 单列主键（section+key 降为唯一约束）；created_at 见 0034。
-- 本文件按目标顺序一次性建全，使全新库一次迁移即得正确顺序。

CREATE TABLE IF NOT EXISTS settings (
    id         UUID NOT NULL DEFAULT gen_random_uuid(),  -- 主键（0009 起正式承接主键）
    section    TEXT NOT NULL,              -- gateway / proxy / health / security / logging / metrics
    key        TEXT NOT NULL,              -- section 内唯一，如 read_header_timeout
    value      JSONB NOT NULL DEFAULT 'null',
    version    INT NOT NULL DEFAULT 1,     -- 单调递增，每次更新 +1
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT settings_section_key_key UNIQUE (section, key)
);
CREATE INDEX IF NOT EXISTS idx_settings_section ON settings(section);
