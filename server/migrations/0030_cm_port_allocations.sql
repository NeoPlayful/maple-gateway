-- cm_port_allocations：CM 集中分配的本机端口占用表。
--
-- 端口由 CM 统一分配（Gateway 经内部接口索取/归还），避免多项目、多版本在同一节点上
-- 抢占同一宿主端口。port 全局唯一：即便容器跨节点迁移，也不会与既有占用冲突——
-- 单节点部署下即为零冲突，多节点下则偏保守（宁可少用端口，不可撞端口）。
-- resource_id 是占用方标识（如 projects.id），释放时按它整批归还。
--
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS cm_port_allocations (
    id          UUID PRIMARY KEY,
    port        INT NOT NULL UNIQUE,               -- 已分配的本机端口（全局唯一）
    node_id     UUID,                              -- 预留：分配落点（当前可不填）
    resource_id TEXT NOT NULL,                     -- 占用方标识（项目/应用 ID）
    kind        TEXT NOT NULL DEFAULT '',          -- 占用类型（project / deployment …）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cm_port_allocations_resource ON cm_port_allocations(resource_id);
