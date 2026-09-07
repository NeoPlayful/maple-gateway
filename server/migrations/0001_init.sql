-- Maple Gateway Phase 1 初始迁移。
-- 表：tenants / domains / services / instances / health_checks / admins / audit_logs

-- 租户
CREATE TABLE IF NOT EXISTS tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'active', -- active / suspended / disabled
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 域名 → Tenant 映射
CREATE TABLE IF NOT EXISTS domains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    hostname    TEXT NOT NULL UNIQUE,          -- 小写 / punycode
    service_id  UUID,                          -- 默认 Service（S6 起使用）
    status      TEXT NOT NULL DEFAULT 'active', -- active / disabled / pending
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tenant 下的应用服务
CREATE TABLE IF NOT EXISTS services (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,                  -- 租户内唯一
    protocol   TEXT NOT NULL DEFAULT 'http',   -- http / https
    status     TEXT NOT NULL DEFAULT 'active', -- active / disabled
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

-- 实际 Container 实例
CREATE TABLE IF NOT EXISTS instances (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id  UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    node_id     UUID,                          -- Phase 2 关联 nodes 表
    version     TEXT,                          -- 版本标签，Phase 2 金丝雀复用
    address     TEXT NOT NULL,                 -- 内部 IP / Host
    port        INT NOT NULL,
    protocol    TEXT NOT NULL DEFAULT 'http',
    weight      INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'enabled',  -- enabled / disabled / draining
    health      TEXT NOT NULL DEFAULT 'unknown',  -- unknown / healthy / unhealthy / recovering
    last_seen_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_instances_service ON instances(service_id);
CREATE INDEX IF NOT EXISTS idx_instances_health ON instances(health);

-- 健康检查配置（service_id 为空 = 全局默认）
CREATE TABLE IF NOT EXISTS health_checks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id       UUID REFERENCES services(id) ON DELETE CASCADE,
    type             TEXT NOT NULL DEFAULT 'http', -- http / tcp
    path             TEXT NOT NULL DEFAULT '/health',
    interval_seconds INT NOT NULL DEFAULT 10,
    timeout_seconds  INT NOT NULL DEFAULT 3,
    failure_threshold INT NOT NULL DEFAULT 3,
    success_threshold INT NOT NULL DEFAULT 2,
    grace_period_seconds INT NOT NULL DEFAULT 10,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 管理员（Management API 登录）
CREATE TABLE IF NOT EXISTS admins (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 管理操作审计
CREATE TABLE IF NOT EXISTS audit_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id    UUID,                            -- Phase 1 无外键（Token 未关联账号时为 null）
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT,
    detail      JSONB,
    ip          TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);
