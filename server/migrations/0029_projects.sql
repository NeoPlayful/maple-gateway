-- templates：应用模板，容器创建的规格来源。
--
-- 一份模板 = 带 {{参数键}} 占位符的 Compose 规格 + 参数定义。用户选模板、填参数即可
-- 创建一个项目，渲染出的规格转为 Application 下发。slug 是模板标识（英文），全局唯一，
-- 并作为数据目录第二段：<节点 data_dir>/<租户标识>/<模板标识>/<项目标识>。
--
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS templates (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    slug        TEXT NOT NULL UNIQUE,               -- 模板标识（英文），数据目录第二段
    description TEXT NOT NULL DEFAULT '',
    spec        TEXT NOT NULL DEFAULT '',           -- Compose 规格模板（含 {{参数键}} 占位符）
    params      JSONB,                              -- 参数定义数组
    status      TEXT NOT NULL DEFAULT 'active',     -- active / disabled
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_templates_status ON templates(status);

-- projects：租户下的一个部署项目。
--
-- 一个租户 + 一个模板可以有多个项目，项目名即项目标识（必须英文），用于区分同一
-- 租户同一模板下的多份独立部署，并作为数据目录的第三段：
--   <节点 data_dir>/<租户标识>/<模板标识>/<项目标识>
--
-- 幂等：可重复执行。

CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    template_id UUID NOT NULL,                       -- 引用的模板（templates.id）
    name        TEXT NOT NULL,                       -- 项目标识（英文），同租户内唯一
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'active',      -- active / disabled
    node_id     TEXT NOT NULL DEFAULT '',            -- 部署落点（Gateway 节点 UUID）
    application_id TEXT NOT NULL DEFAULT '',         -- 渲染模板后生成的 CM Application ID（幂等键）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_tenant_name ON projects(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects(tenant_id);
