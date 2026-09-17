-- nodes 追加数据根目录：容器绑定挂载的宿主根路径。
--
-- 该节点上创建的容器，其映射目录一律落在本目录之下，路径形如
-- <data_dir>/<租户标识>/<模板标识>/<项目标识>/...，实现租户与项目间数据隔离。
-- 为空表示该节点未配置数据目录，此时不做任何绑定挂载。
--
-- 幂等：可重复执行。

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS data_dir TEXT NOT NULL DEFAULT '';
