-- Enrollment Token 落库增强：补签发人（审计）与过期清理索引。
-- 幂等：可重复执行。

-- 签发人（管理端操作者标识，可为空）。
ALTER TABLE cm_enrollment_tokens ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '';

-- 过期清理扫描用。
CREATE INDEX IF NOT EXISTS idx_cm_enrollment_tokens_expires ON cm_enrollment_tokens(expires_at);

-- 已消费 Token 的 value 改为哈希占位（不再唯一冲突）：放开原唯一约束为普通索引。
-- 未消费 Token 的 value 仍需唯一，故保留一个仅作用于 active 行的部分唯一索引。
ALTER TABLE cm_enrollment_tokens DROP CONSTRAINT IF EXISTS cm_enrollment_tokens_value_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_cm_enrollment_tokens_active_value
    ON cm_enrollment_tokens(value) WHERE status = 'active';
