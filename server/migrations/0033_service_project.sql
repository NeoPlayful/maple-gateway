-- 服务归属下沉到项目：services 增加 project_id，形成「一项目一服务」。
--
-- 背景：项目已成为运行与部署边界（独立 /24、存储三段路径、Compose 工程），
-- 服务应随项目而生、随项目而灭，而不是直接挂在租户下。此前真正承载绑定的是
-- CM 侧 cm_applications.service_id（应用级），Gateway 的 services 只有 tenant_id，
-- 无法回答"这个服务属于哪个项目"。
--
-- project_id 可空：既有租户级服务（含版本部署路径的服务）不受影响，不填即维持原行为。
-- 唯一索引落在 project_id 上实现 1:1——Postgres 对 NULL 不去重，故多个未归属服务可共存。
-- tenant_id 保留为派生归属（从项目继承），路由表的租户门控仍依赖它。
-- 外键指向 projects.id，项目删除时置空而非连同删除——服务数据保留，仅解除归属。
--
-- 幂等：可重复执行。

ALTER TABLE services ADD COLUMN IF NOT EXISTS project_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'services_project_id_fkey'
    ) THEN
        ALTER TABLE services
            ADD CONSTRAINT services_project_id_fkey
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
    END IF;
END $$;

-- 一项目一服务：project_id 非空时唯一（NULL 可重复，兼容存量未归属服务）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_services_project ON services(project_id);
