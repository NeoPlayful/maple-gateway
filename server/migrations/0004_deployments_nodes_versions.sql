-- Maple Gateway Phase 2 模型演进（S1）。
-- 新增：nodes / deployments / deployment_versions；instances 挂载 deployment_id / version_id / node_id。
-- 兼容：存量直挂 Service 的实例（deployment_id / version_id 为空）仍可按 Phase 1 语义路由。

-- 节点（Container Manager / Node Agent 注册）
CREATE TABLE IF NOT EXISTS nodes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,          -- 节点名，如 node-01
    host         TEXT NOT NULL,                 -- 节点地址（管理面）
    region       TEXT,                          -- 区域（Phase 3 亲和预留）
    labels       JSONB,                         -- 节点标签
    status       TEXT NOT NULL DEFAULT 'online', -- online / offline / maintenance / disabled
    weight       INT NOT NULL DEFAULT 1,        -- 节点权重（预留）
    last_seen_at TIMESTAMPTZ,                   -- 最近心跳
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);

-- 部署（Service 下的发布单元）
CREATE TABLE IF NOT EXISTS deployments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,                   -- service 内唯一，如 prod
    status     TEXT NOT NULL DEFAULT 'active',  -- active / paused / stopped
    strategy   TEXT NOT NULL DEFAULT 'rolling', -- rolling / recreate / blue_green（记录型，执行归 Container Manager）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, name)
);
CREATE INDEX IF NOT EXISTS idx_deployments_service ON deployments(service_id);

-- 部署版本（分流的最小单元）
CREATE TABLE IF NOT EXISTS deployment_versions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    version       TEXT NOT NULL,                -- deployment 内唯一，如 v1 / v2
    image         TEXT,                         -- 镜像标识（信息记录）
    weight        INT NOT NULL DEFAULT 100,     -- 版本流量权重（version 层 WRR）
    status        TEXT NOT NULL DEFAULT 'stable', -- stable / canary / active / standby / draining / inactive
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (deployment_id, version)
);
CREATE INDEX IF NOT EXISTS idx_deployment_versions_deployment ON deployment_versions(deployment_id);

-- instances 挂载 deployment / version / node
ALTER TABLE instances ADD COLUMN IF NOT EXISTS deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS version_id UUID REFERENCES deployment_versions(id) ON DELETE SET NULL;

-- 挂载组合索引：路由构建按 service 找 instance 时用到
CREATE INDEX IF NOT EXISTS idx_instances_deployment ON instances(deployment_id);
CREATE INDEX IF NOT EXISTS idx_instances_version ON instances(version_id);
CREATE INDEX IF NOT EXISTS idx_instances_node ON instances(node_id);
