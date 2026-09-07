-- Maple Gateway Phase 2 动态设置（S6 Settings）。
-- settings：运行时配置 KV，按 section 组织，版本化用于审计与回滚。

CREATE TABLE IF NOT EXISTS settings (
    section    TEXT NOT NULL,              -- gateway / proxy / health / security / logging / metrics
    key        TEXT NOT NULL,              -- section 内唯一，如 read_header_timeout
    value      JSONB NOT NULL DEFAULT 'null',
    version    INT NOT NULL DEFAULT 1,     -- 单调递增，每次更新 +1
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (section, key)
);
CREATE INDEX IF NOT EXISTS idx_settings_section ON settings(section);
