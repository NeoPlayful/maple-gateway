-- 版本与部署期望态追加绑定挂载规格：容器把数据根下的子目录映射进容器。
--
-- 每项形如 {"path":"<租户>/<模板>/<项目>/<子目录>","target":"/容器内路径","read_only":false}。
-- path 是相对节点数据根的相对路径，宿主绝对路径由节点用自身 data_dir 拼出并校验越界。
--
-- 幂等：可重复执行。

ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS mounts JSONB;
ALTER TABLE cm_deployments      ADD COLUMN IF NOT EXISTS mounts JSONB;
