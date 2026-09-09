-- Phase 5 Direct TLS：domains 追加 TLS 配置字段。
-- tls_mode：cloudflare / managed / manual / disabled（本期可写 manual | disabled）。
-- certificate_status 与 status(路由) 分离，证书失败不自动降级路由。
-- 幂等：可重复执行。

ALTER TABLE domains ADD COLUMN IF NOT EXISTS tls_mode TEXT NOT NULL DEFAULT 'disabled';
ALTER TABLE domains ADD COLUMN IF NOT EXISTS certificate_status TEXT;
CREATE INDEX IF NOT EXISTS idx_domains_tls_mode ON domains(tls_mode);
