-- 收尾统一发布模型：0025 已把 canary/bluegreen 两套表的数据搬入 releases/release_events，
-- 此处删除遗留的四张旧表（过渡期结束，旧记录已无代码引用）。
-- 幂等，可重复执行。
--
-- 删除顺序：先事件表（外键指向主表），再主表。
DROP TABLE IF EXISTS canary_events;
DROP TABLE IF EXISTS canary_releases;
DROP TABLE IF EXISTS bluegreen_events;
DROP TABLE IF EXISTS bluegreen_deployments;
