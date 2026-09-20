-- 为版本部署（单容器）补上项目归属：deployment_versions / cm_deployments / instances 增加 project_id。
--
-- 背景：应用模板实例化出的 Compose 应用早已挂在项目下（projects.application_id），
-- 但「服务 → 部署 → 版本 → 单容器」这条链的容器没有任何项目归属——版本挂载路径里
-- 虽然写着 <租户>/<模板>/<项目>，却只是自由文本，系统无法回答"这个容器属于哪个项目"。
-- 本迁移把项目归属落到版本上，经期望态与容器标签一路传到实例，使项目成为两类负载
-- （Compose 应用、版本单容器）共用的隔离单元。
--
-- project_id 可空：既有版本/实例不受影响，不填即维持原行为（无项目归属）。
-- 外键指向 projects.id，项目删除时置空而非连同删除——数据保留，仅解除归属。
--
-- 幂等：可重复执行。

ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS project_id UUID;
ALTER TABLE cm_deployments      ADD COLUMN IF NOT EXISTS project_id UUID;
ALTER TABLE instances           ADD COLUMN IF NOT EXISTS project_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'deployment_versions_project_id_fkey'
    ) THEN
        ALTER TABLE deployment_versions
            ADD CONSTRAINT deployment_versions_project_id_fkey
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'instances_project_id_fkey'
    ) THEN
        ALTER TABLE instances
            ADD CONSTRAINT instances_project_id_fkey
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_deployment_versions_project ON deployment_versions(project_id);
CREATE INDEX IF NOT EXISTS idx_instances_project ON instances(project_id);
