-- Managed Certificate / ACME 自动签发续期：
-- 1) certificates 追加续期引擎字段（provider_meta / renew_attempts / next_renew_at / last_renew_error）
-- 2) 新增 acme_accounts 表（CA 账户注册与加密账户密钥）
-- private_key_encrypted / key_encrypted 只存 AES-256-GCM 密文(base64)，明文永不落库。
-- 幂等：可重复执行。

ALTER TABLE certificates ADD COLUMN IF NOT EXISTS provider_meta JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE certificates ADD COLUMN IF NOT EXISTS renew_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE certificates ADD COLUMN IF NOT EXISTS next_renew_at TIMESTAMPTZ;
ALTER TABLE certificates ADD COLUMN IF NOT EXISTS last_renew_error TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_certificates_next_renew_at ON certificates(next_renew_at);

CREATE TABLE IF NOT EXISTS acme_accounts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    directory_url  TEXT NOT NULL,                       -- ACME 目录；账户与目录绑定
    account_url    TEXT NOT NULL DEFAULT '',            -- CA 侧账户 URL（注册后回填）
    email          TEXT NOT NULL DEFAULT '',
    key_encrypted  TEXT NOT NULL,                       -- 账户私钥(pem) 的 AES-GCM 密文(base64)
    status         TEXT NOT NULL DEFAULT 'active',      -- active / error
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_acme_accounts_directory_url ON acme_accounts(directory_url);
