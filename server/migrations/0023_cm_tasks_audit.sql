-- 任务系统增强：重试谱系、创建人、起止时间与列表查询索引。
-- 幂等：可重复执行。

ALTER TABLE cm_tasks ADD COLUMN IF NOT EXISTS parent_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE cm_tasks ADD COLUMN IF NOT EXISTS created_by     TEXT NOT NULL DEFAULT '';
ALTER TABLE cm_tasks ADD COLUMN IF NOT EXISTS started_at     TIMESTAMPTZ;
ALTER TABLE cm_tasks ADD COLUMN IF NOT EXISTS finished_at    TIMESTAMPTZ;

-- 允许无 Gateway 节点归属的任务（如尚未绑定的节点名）落库。
ALTER TABLE cm_tasks ALTER COLUMN node_id DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cm_tasks_created_at ON cm_tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cm_tasks_parent     ON cm_tasks(parent_task_id);
