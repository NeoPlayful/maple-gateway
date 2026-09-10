-- Managed Certificate 签发/续期进度：certificate_operations 表。
-- 记录 ACME 签发/续期的阶段进度，供管理面轮询展示。
-- 与 certificates 表分离：进行中的操作无可用证书材料，不进证书表，避免扰动数据面缓存装载。
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS certificate_operations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hostname     TEXT NOT NULL,
    domain_id    UUID,
    action       TEXT NOT NULL DEFAULT 'issue',   -- issue / renew
    status       TEXT NOT NULL DEFAULT 'queued',  -- queued/account_ready/order_created/challenge_presented/challenge_validated/finalizing/active/failed
    message      TEXT NOT NULL DEFAULT '',        -- 人类可读阶段说明
    error        TEXT NOT NULL DEFAULT '',        -- 失败原因（已分类）
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ
);
-- 同一 hostname 同时仅允许一个进行中的操作（去重，防重复下单触发 CA 限流）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_certificate_operations_active_hostname
    ON certificate_operations(hostname)
    WHERE status NOT IN ('active', 'failed');
CREATE INDEX IF NOT EXISTS idx_certificate_operations_domain_id
    ON certificate_operations(domain_id);
CREATE INDEX IF NOT EXISTS idx_certificate_operations_updated_at
    ON certificate_operations(updated_at);
