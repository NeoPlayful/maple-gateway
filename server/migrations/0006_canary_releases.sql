-- Maple Gateway Phase 2 发布控制（S4）。
-- 新增：canary_releases（Canary 发布状态机）/ canary_events（发布事件流水）。

-- Canary 发布
CREATE TABLE IF NOT EXISTS canary_releases (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id         UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,                   -- service 内唯一，如 "v2-canary"
    stable_version_id  UUID NOT NULL REFERENCES deployment_versions(id) ON DELETE CASCADE,
    canary_version_id  UUID NOT NULL REFERENCES deployment_versions(id) ON DELETE CASCADE,
    phase              TEXT NOT NULL DEFAULT 'created', -- created / running / paused / completed / rolled_back / failed
    canary_weight      INT NOT NULL DEFAULT 0,          -- 当前 canary 版本流量权重（0-100）
    target_weight      INT NOT NULL DEFAULT 100,        -- 目标权重（步进上限）
    step_weight        INT NOT NULL DEFAULT 10,         -- 每次 weight 步进量
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, name),
    CHECK (stable_version_id <> canary_version_id),
    CHECK (canary_weight >= 0 AND canary_weight <= 100),
    CHECK (target_weight >= 0 AND target_weight <= 100),
    CHECK (step_weight >= 1 AND step_weight <= 100)
);
CREATE INDEX IF NOT EXISTS idx_canary_releases_service ON canary_releases(service_id);
CREATE INDEX IF NOT EXISTS idx_canary_releases_phase ON canary_releases(phase);

-- 发布事件流水（9.4 audit/事件表）。
CREATE TABLE IF NOT EXISTS canary_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id   UUID NOT NULL REFERENCES canary_releases(id) ON DELETE CASCADE,
    phase        TEXT NOT NULL,          -- 动作：start / pause / resume / weight / promote / rollback
    from_weight  INT,
    to_weight    INT,
    detail       TEXT,                   -- 说明（如 "rollback: v2 drained, v1 weight restored"）
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_canary_events_release ON canary_events(release_id);
