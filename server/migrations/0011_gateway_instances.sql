-- 多实例 HA：本 Gateway 进程注册表 + lease。
-- 每个运行的 Gateway 进程注册一行（按 instance_id 幂等 upsert），
-- 周期心跳刷新 last_seen_at；启用多实例协调时，Leader 在该行记录 lease_until。
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS gateway_instances (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id TEXT NOT NULL,              -- 启动生成/配置的稳定实例标识
    addr        TEXT NOT NULL,              -- 管理面地址
    hostname    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'online',  -- online / offline / draining / maintenance
    role        TEXT NOT NULL DEFAULT 'standalone', -- standalone / leader / follower
    lease_until TIMESTAMPTZ,                -- Leader lease 到期时间（DB 承载时有效）
    version     TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_instances_instance_id
    ON gateway_instances(instance_id);
CREATE INDEX IF NOT EXISTS idx_gateway_instances_status
    ON gateway_instances(status);
CREATE INDEX IF NOT EXISTS idx_gateway_instances_role
    ON gateway_instances(role);
