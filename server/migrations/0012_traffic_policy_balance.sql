-- 实例选择算法：traffic_policies 加 balance 列。
-- 值：round_robin（默认）/ consistent_hash / least_conn。
-- 幂等：可重复执行。

ALTER TABLE traffic_policies
    ADD COLUMN IF NOT EXISTS balance TEXT NOT NULL DEFAULT 'round_robin';
