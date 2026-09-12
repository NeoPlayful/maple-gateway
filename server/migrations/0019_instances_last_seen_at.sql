-- 实例 last_seen_at 语义修正：改为"最后被 Container Manager 看见"，
-- 由内部上报路径（register / sync 命中 / heartbeat / PATCH）刷新；健康检查不再写它。
-- 新增 stale 状态表示上报超时的实例，并补 last_seen_at 索引供对账/回收扫描。

-- status 允许新增值 stale（无 CHECK 约束，仅更新列注释说明取值）。
COMMENT ON COLUMN instances.status IS 'enabled / disabled / draining / stale';

CREATE INDEX IF NOT EXISTS idx_instances_last_seen_at ON instances(last_seen_at);
