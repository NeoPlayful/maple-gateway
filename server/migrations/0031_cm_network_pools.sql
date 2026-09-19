-- 项目级 IP 池（IPAM）：网络池与项目网络两张表。
--
-- 背景：每个项目需要一段独立的 Docker bridge 网段。此前依赖 Docker 默认地址池
-- （每个由 Compose 自动建的网络各自消耗一个 /16），几十个项目即耗尽。改为由 CM 统一
-- 分配网段、显式指定 --subnet 建 bridge，绕开默认池，容量与规划完全可控。
--
-- 权威与执行分层：本表（CM 数据库）是 IPAM 状态权威，Node Agent / Docker 只做执行。
-- Scope 为 Node-local：不同节点允许复用相同 CIDR（无跨节点三层互通），故唯一约束一律
-- 带 node_id；同一节点内所有池与项目网段必须互不重叠（重叠检测在业务层）。
--
-- 幂等：可重复执行。

-- cm_node_network_pools：一个节点可挂多个网络池，按 priority ASC 顺序取用。
-- 扩容的方式是「新增池」，而非修改既有池的 CIDR，故无需迁移既有项目。
CREATE TABLE IF NOT EXISTS cm_node_network_pools (
    id                   UUID PRIMARY KEY,
    node_id              TEXT NOT NULL,                    -- Gateway 节点 UUID（对齐 projects.node_id）
    name                 TEXT NOT NULL,                    -- 池名，同节点内唯一
    description          TEXT NOT NULL DEFAULT '',
    address_pool         TEXT NOT NULL,                    -- 池 CIDR，如 10.128.0.0/9
    project_prefix       INT  NOT NULL,                    -- 项目子网前缀，如 24
    priority             INT  NOT NULL DEFAULT 100,        -- 越小越先取用
    status               TEXT NOT NULL DEFAULT 'active',   -- active / draining / disabled / exhausted / conflict
    next_index           BIGINT NOT NULL DEFAULT 0,        -- 顺序分配的游标
    reuse_enabled        BOOLEAN NOT NULL DEFAULT true,    -- 已释放网段是否可复用
    reuse_delay_seconds  INT  NOT NULL DEFAULT 600,        -- 释放后冷却期（秒）
    is_system_default    BOOLEAN NOT NULL DEFAULT false,   -- 是否随节点注册自动创建的默认池
    last_checked_at      TIMESTAMPTZ,                       -- 最近一次冲突复检时间
    last_conflict_reason TEXT NOT NULL DEFAULT '',         -- 最近一次冲突原因
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 一个节点多个池：不得对 node_id 单独加唯一约束。
CREATE UNIQUE INDEX IF NOT EXISTS idx_cm_pools_node_name ON cm_node_network_pools(node_id, name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cm_pools_node_pool ON cm_node_network_pools(node_id, address_pool);
CREATE INDEX IF NOT EXISTS idx_cm_pools_node_status ON cm_node_network_pools(node_id, status);

-- cm_project_networks：记录每个项目实际占用的网段及其 Docker 网络。
-- 生命周期独立于单次部署：Redeploy/Rollback 复用既有网段，仅项目删除才释放。
-- MVP 一个项目一个网段，故 project_id 唯一。
CREATE TABLE IF NOT EXISTS cm_project_networks (
    id                  UUID PRIMARY KEY,
    node_id             TEXT NOT NULL,                     -- 落点节点（Gateway 节点 UUID）
    project_id          UUID NOT NULL,                     -- 所属项目（projects.id）
    pool_id             UUID NOT NULL REFERENCES cm_node_network_pools(id),
    subnet_index        BIGINT NOT NULL,                   -- 池内序号（顺序分配）
    subnet              TEXT NOT NULL,                     -- 项目子网 CIDR，如 10.128.15.0/24
    gateway             TEXT NOT NULL,                     -- 子网网关（首可用地址）
    docker_network_name TEXT NOT NULL,                     -- Docker 网络名，maple-<12>
    status              TEXT NOT NULL DEFAULT 'reserved',  -- reserved/creating/active/deleting/released/failed/conflict/drift
    allocated_at        TIMESTAMPTZ,                        -- 置 active 的时间
    released_at         TIMESTAMPTZ,                        -- 释放时间
    reuse_after         TIMESTAMPTZ,                        -- 冷却结束、可重新分配的时间
    last_error          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 同一节点内网段唯一：并发分配的最终兜底（业务层仍以 FOR UPDATE 预占避让）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_cm_prjnet_node_subnet ON cm_project_networks(node_id, subnet);
-- 一个项目一个网段。
CREATE UNIQUE INDEX IF NOT EXISTS idx_cm_prjnet_project ON cm_project_networks(project_id);
CREATE INDEX IF NOT EXISTS idx_cm_prjnet_pool_status ON cm_project_networks(pool_id, status);
