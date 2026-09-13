-- Compose 应用（Application）表：整包 Compose 规格的存储与运行态。
-- 与 cm_deployments（版本→单容器期望态）并列，面向多服务 Compose 项目。
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS cm_applications (
    id            UUID PRIMARY KEY,
    tenant_id     UUID,
    name          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'created',   -- created / running / stopped / removed / failed
    node_id       TEXT NOT NULL DEFAULT '',          -- 部署落点（Gateway 节点 UUID）
    service_id    UUID,                              -- 关联的 Gateway 服务（可空）
    version       TEXT NOT NULL DEFAULT '',          -- 当前部署版本号
    spec          TEXT NOT NULL DEFAULT '',          -- 该版本 Compose 规格（YAML 原文）
    target_weight INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cm_applications_status ON cm_applications(status);
CREATE INDEX IF NOT EXISTS idx_cm_applications_service ON cm_applications(service_id);
