-- Maple Gateway Phase 2 流量策略与限流（S2/S5 模型先行）。
-- 新增：traffic_policies / rate_limits。

-- 流量策略
CREATE TABLE IF NOT EXISTS traffic_policies (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id       UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,                -- service 内唯一
    priority         INT NOT NULL DEFAULT 100,     -- 低数值优先匹配
    match            JSONB NOT NULL DEFAULT '{}',  -- {header,cookie,path,percent}
    target_version_id UUID,                        -- 命中后定向 Version
    weight           INT NOT NULL DEFAULT 100,     -- 版本分流权重
    sticky           JSONB,                        -- {cookie_name,ttl_seconds}
    status           TEXT NOT NULL DEFAULT 'enabled', -- enabled / disabled
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, name)
);
CREATE INDEX IF NOT EXISTS idx_traffic_policies_service ON traffic_policies(service_id);
CREATE INDEX IF NOT EXISTS idx_traffic_policies_version ON traffic_policies(target_version_id);

-- 限流规则
CREATE TABLE IF NOT EXISTS rate_limits (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope          TEXT NOT NULL,                  -- global / tenant / domain / service / ip
    tenant_id      UUID REFERENCES tenants(id) ON DELETE CASCADE,
    domain_id      UUID REFERENCES domains(id) ON DELETE CASCADE,
    service_id     UUID REFERENCES services(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    "limit"        INT NOT NULL,                   -- 请求配额（窗口内），limit 为保留字需加引号
    window_seconds INT NOT NULL DEFAULT 60,
    burst          INT,                            -- 突发容量
    response_code  INT NOT NULL DEFAULT 429,
    status         TEXT NOT NULL DEFAULT 'enabled', -- enabled / disabled
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_rate_limits_scope ON rate_limits(scope);
CREATE INDEX IF NOT EXISTS idx_rate_limits_tenant ON rate_limits(tenant_id);
CREATE INDEX IF NOT EXISTS idx_rate_limits_domain ON rate_limits(domain_id);
CREATE INDEX IF NOT EXISTS idx_rate_limits_service ON rate_limits(service_id);
