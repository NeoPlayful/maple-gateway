-- Container Manager 控制面核心表。
-- CM 与 Gateway 同库同迁移目录。
--
-- 节点身份不在此重复：Gateway 的 nodes 表是唯一节点权威（name/host/region/labels/
-- weight/路由状态）。cm_node_runtime 只存 CM 侧运行期字段（凭证哈希、OS/arch/Agent
-- 版本、时间戳），以 Gateway 节点 UUID 为键，不复制任何 Gateway 字段。
--
-- 幂等：可重复执行。

-- 节点的 CM 侧运行期记录。id = Gateway 节点 UUID。
CREATE TABLE IF NOT EXISTS cm_node_runtime (
    id              UUID PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',     -- 节点名（= Agent hostname，与 Gateway nodes.name 对应）
    credential_hash TEXT NOT NULL DEFAULT '',    -- SHA-256(凭证明文) 十六进制，明文不下库
    os              TEXT NOT NULL DEFAULT '',
    arch            TEXT NOT NULL DEFAULT '',
    agent_version   TEXT NOT NULL DEFAULT '',
    revoked         BOOLEAN NOT NULL DEFAULT false,
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cm_node_runtime_revoked ON cm_node_runtime(revoked);

-- 一次性注册令牌。
CREATE TABLE IF NOT EXISTS cm_enrollment_tokens (
    id         TEXT PRIMARY KEY,
    value      TEXT NOT NULL UNIQUE,             -- mg_enroll_ 前缀明文（一次性，短时有效）
    note       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',   -- active / used / expired
    used_by    UUID,                             -- 消费时绑定的节点（Gateway UUID）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    used_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_cm_enrollment_tokens_status ON cm_enrollment_tokens(status);

-- 下发任务的生命周期记录。
CREATE TABLE IF NOT EXISTS cm_tasks (
    id          TEXT PRIMARY KEY,
    node_id     UUID NOT NULL,
    action      TEXT NOT NULL,
    params      JSONB,
    status      TEXT NOT NULL,                   -- pending / dispatching / running / success / failed / cancelled / timeout
    percent     INT NOT NULL DEFAULT 0,
    message     TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    result      JSONB,
    request_id  TEXT NOT NULL DEFAULT '',
    attempts    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deadline_at TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cm_tasks_node ON cm_tasks(node_id);
CREATE INDEX IF NOT EXISTS idx_cm_tasks_status ON cm_tasks(status);

-- 部署期望态与编排进度。
CREATE TABLE IF NOT EXISTS cm_deployments (
    deployment_id UUID PRIMARY KEY,
    service_id    UUID,
    version_id    UUID,
    version       TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL,
    replicas      INT NOT NULL DEFAULT 0,
    port          INT NOT NULL DEFAULT 0,
    env           JSONB,
    resources     JSONB,
    health_path   TEXT NOT NULL DEFAULT '',
    node_selector JSONB,
    strategy      TEXT NOT NULL DEFAULT '',
    phase         TEXT NOT NULL DEFAULT 'pending',  -- pending / reconciling / ready / stopped / failed
    phase_message TEXT NOT NULL DEFAULT '',
    stopped       BOOLEAN NOT NULL DEFAULT false,    -- 被 Stop 的部署保留进度记录
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cm_deployments_phase ON cm_deployments(phase);
