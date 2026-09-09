-- Phase 5 Direct TLS：每域名 Manual Certificate 表。
-- private_key_encrypted 只存 AES-256-GCM 密文(base64)，明文私钥永不落库。
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS certificates (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id              UUID REFERENCES domains(id) ON DELETE CASCADE,
    hostname               TEXT NOT NULL,               -- 冗余存 hostname，缓存/检索免 join
    source                 TEXT NOT NULL DEFAULT 'manual', -- manual / cloudflare / acme / origin
    status                 TEXT NOT NULL DEFAULT 'pending', -- active / pending / error / expiring / expired
    certificate_pem        TEXT NOT NULL,
    private_key_encrypted  TEXT NOT NULL,               -- AES-GCM 密文(base64)
    issuer                 TEXT NOT NULL DEFAULT '',
    serial_number          TEXT NOT NULL DEFAULT '',
    issued_at              TIMESTAMPTZ,
    expires_at             TIMESTAMPTZ,
    last_renewed_at        TIMESTAMPTZ,
    last_error             TEXT NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_certificates_domain_id
    ON certificates(domain_id);
CREATE INDEX IF NOT EXISTS idx_certificates_hostname
    ON certificates(hostname);
CREATE INDEX IF NOT EXISTS idx_certificates_expires_at
    ON certificates(expires_at);
