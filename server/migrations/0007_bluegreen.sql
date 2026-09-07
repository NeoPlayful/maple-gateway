-- Maple Gateway Phase 2 发布控制（S4 Blue/Green）。
-- bluegreen_deployments：跟踪某部署的 blue/green 双版本对与上一 active（供回滚）。
-- 切换动作 = 目标版本升 active(weight 100) + 旧 active 降 standby(weight 0)，由部署版本状态机承载。

CREATE TABLE IF NOT EXISTS bluegreen_deployments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id  UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    blue_version_id  UUID NOT NULL REFERENCES deployment_versions(id) ON DELETE CASCADE,
    green_version_id UUID NOT NULL REFERENCES deployment_versions(id) ON DELETE CASCADE,
    active_version_id UUID NOT NULL,     -- 当前 active（blue 或 green）
    previous_active_id UUID,             -- 切换前的 active，供 rollback
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (deployment_id),
    CHECK (blue_version_id <> green_version_id)
);
CREATE INDEX IF NOT EXISTS idx_bg_deployments_deployment ON bluegreen_deployments(deployment_id);

-- 切换历史（audit）。
CREATE TABLE IF NOT EXISTS bluegreen_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bg_id       UUID NOT NULL REFERENCES bluegreen_deployments(id) ON DELETE CASCADE,
    action      TEXT NOT NULL,          -- switch / rollback
    from_active UUID,
    to_active   UUID,
    detail      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bg_events_bg ON bluegreen_events(bg_id);
