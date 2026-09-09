-- RBAC：admins 表加 role（super_admin / operator / viewer），存量管理员默认超管。
ALTER TABLE admins ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'super_admin';
