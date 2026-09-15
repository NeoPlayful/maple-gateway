-- 统一发布模型：把 canary_releases / bluegreen_deployments 两套独立系统
-- 合并为 releases / release_events 两张表（strategy 判别列 + 统一效果写入）。
-- 幂等，可重复执行。旧表保留（只读一个版本过渡），不在此处删除。
--
-- 统一语义：一次发布 = 某部署下的两个版本 + 当前流量分配。
--   strategy=canary    : primary=stable，secondary=canary；权重连续（0-100）
--   strategy=bluegreen : primary=active，secondary=另一色；权重二元（100/0）

CREATE TABLE IF NOT EXISTS releases (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy            TEXT NOT NULL,                    -- canary / bluegreen
    deployment_id       UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    service_id          UUID,                             -- 归属服务（API 筛选用，可空）
    name                TEXT NOT NULL DEFAULT '',
    phase               TEXT NOT NULL DEFAULT 'created',
                        -- canary: created/running/paused/completed/rolled_back/failed
                        -- bluegreen: active（二元配置，无进度概念）
    primary_version_id  UUID NOT NULL,                    -- canary=stable / bluegreen=active
    secondary_version_id UUID NOT NULL,                   -- canary=canary / bluegreen=另一色
    primary_weight      INT NOT NULL DEFAULT 100,
    secondary_weight    INT NOT NULL DEFAULT 0,
    previous_primary_id UUID,                             -- 上一 primary（bluegreen 回滚用；canary 恒空）
    config              JSONB NOT NULL DEFAULT '{}'::jsonb, -- 策略专属：canary{target_weight,step_weight} / bluegreen{blue,green}
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (primary_version_id <> secondary_version_id),
    CHECK (primary_weight >= 0 AND primary_weight <= 100),
    CHECK (secondary_weight >= 0 AND secondary_weight <= 100)
);

CREATE INDEX IF NOT EXISTS idx_releases_deployment ON releases(deployment_id);
CREATE INDEX IF NOT EXISTS idx_releases_service ON releases(service_id);
CREATE INDEX IF NOT EXISTS idx_releases_phase ON releases(phase);

-- 部署级互斥：每个部署至多一个"活跃"（非终态）发布。
-- 终态 = completed / rolled_back / failed；其余（created/running/paused/active）视为活跃。
-- 这正是本次统一要堵的漏洞：过去 canary 与 bluegreen 可同时挂在一个部署上互相打乱权重。
CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_active_deployment
    ON releases(deployment_id)
    WHERE phase NOT IN ('completed', 'rolled_back', 'failed');

-- 统一事件流水（合并 canary_events 与 bluegreen_events）。
CREATE TABLE IF NOT EXISTS release_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id   UUID NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    action       TEXT NOT NULL,          -- start/pause/resume/weight/promote/rollback/switch
    from_weight  INT,
    to_weight    INT,
    from_version UUID,
    to_version   UUID,
    detail       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_release_events_release ON release_events(release_id);

-- ---------------------------------------------------------------------------
-- 数据搬迁：沿用原记录 ID 作为 releases.id，使事件外键可直接续接。
-- ---------------------------------------------------------------------------

-- canary_releases -> releases（strategy=canary）
INSERT INTO releases (
    id, strategy, deployment_id, service_id, name, phase,
    primary_version_id, secondary_version_id, primary_weight, secondary_weight,
    previous_primary_id, config, started_at, finished_at, created_at, updated_at
)
SELECT
    c.id,
    'canary',
    v.deployment_id,
    c.service_id,
    c.name,
    c.phase,
    c.stable_version_id,
    c.canary_version_id,
    100 - c.canary_weight,
    c.canary_weight,
    NULL,
    jsonb_build_object('target_weight', c.target_weight, 'step_weight', c.step_weight),
    c.started_at,
    c.finished_at,
    c.created_at,
    c.updated_at
FROM canary_releases c
JOIN deployment_versions v ON v.id = c.stable_version_id
WHERE NOT EXISTS (SELECT 1 FROM releases r WHERE r.id = c.id);

-- bluegreen_deployments -> releases（strategy=bluegreen，primary=active）
INSERT INTO releases (
    id, strategy, deployment_id, service_id, name, phase,
    primary_version_id, secondary_version_id, primary_weight, secondary_weight,
    previous_primary_id, config, started_at, finished_at, created_at, updated_at
)
SELECT
    b.id,
    'bluegreen',
    b.deployment_id,
    d.service_id,
    'blue-green',
    'active',
    b.active_version_id,
    CASE WHEN b.active_version_id = b.blue_version_id THEN b.green_version_id ELSE b.blue_version_id END,
    100,
    0,
    b.previous_active_id,
    jsonb_build_object('blue_version_id', b.blue_version_id, 'green_version_id', b.green_version_id),
    NULL,
    NULL,
    b.created_at,
    b.updated_at
FROM bluegreen_deployments b
JOIN deployments d ON d.id = b.deployment_id
WHERE NOT EXISTS (SELECT 1 FROM releases r WHERE r.id = b.id);

-- canary_events -> release_events（action = 原 phase 列）
INSERT INTO release_events (
    id, release_id, action, from_weight, to_weight, from_version, to_version, detail, created_at
)
SELECT
    e.id, e.release_id, e.phase, e.from_weight, e.to_weight, NULL, NULL, COALESCE(e.detail, ''), e.created_at
FROM canary_events e
WHERE NOT EXISTS (SELECT 1 FROM release_events re WHERE re.id = e.id);

-- bluegreen_events -> release_events（from/to active 映射为 from/to version）
INSERT INTO release_events (
    id, release_id, action, from_weight, to_weight, from_version, to_version, detail, created_at
)
SELECT
    e.id, e.bg_id, e.action, NULL, NULL, e.from_active, e.to_active, COALESCE(e.detail, ''), e.created_at
FROM bluegreen_events e
WHERE NOT EXISTS (SELECT 1 FROM release_events re WHERE re.id = e.id);
