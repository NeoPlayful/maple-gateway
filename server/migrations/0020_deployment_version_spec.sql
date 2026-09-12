-- deployment_versions 追加容器执行规格字段：把"部署意图"从纯记录变为可执行规格，
-- Container Manager 据此驱动 Node Agent 创建容器。

ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS replicas      INT NOT NULL DEFAULT 1;  -- 期望副本数
ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS port          INT NOT NULL DEFAULT 0;  -- 容器监听端口（供 Agent 映射）
ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS env           JSONB;                    -- 注入容器的环境变量
ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS resources     JSONB;                    -- CPU/内存限制
ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS health_path   TEXT NOT NULL DEFAULT ''; -- 容器健康检查路径
ALTER TABLE deployment_versions ADD COLUMN IF NOT EXISTS node_selector JSONB;                    -- 调度约束（region/labels）
