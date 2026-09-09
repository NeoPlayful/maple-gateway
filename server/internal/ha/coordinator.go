package ha

import (
	"context"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Coordinator 运行本进程的 HA 心跳与 Leader 协调。
//
// 多实例协调分两级承载：
//   - Redis 可用：leader 用 SET NX PX 抢分布式锁，断连即降级；
//   - Redis 不可用/未配置：退回 DB lease（gateway_instances.lease_until 原子比较）。
//
// 无论哪种承载，本进程都先在 gateway_instances 注册一行并周期心跳。
type Coordinator struct {
	repo   *Repository
	redis  *pkg.Redis // 可空；nil 时仅走 DB lease
	logger *zap.Logger
	cfg    Config

	dbID     uuid.UUID // 本实例在 gateway_instances 的实体 id（首次注册后填充）
	isLeader bool
	stop     chan struct{}
}

// NewCoordinator 构造。
func NewCoordinator(repo *Repository, redis *pkg.Redis, logger *zap.Logger, cfg Config) *Coordinator {
	return &Coordinator{
		repo:   repo,
		redis:  redis,
		logger: logger,
		cfg:    cfg,
		stop:   make(chan struct{}),
	}
}

// InstanceID 返回本进程实例 ID。
func (c *Coordinator) InstanceID() string { return c.cfg.InstanceID }

// IsLeader 返回本进程当前是否为 leader。
func (c *Coordinator) IsLeader() bool { return c.isLeader }

// enabled 返回是否启用多实例协调。
func (c *Coordinator) enabled() bool { return c.cfg.Enabled }

// Run 启动心跳循环，直到 ctx 取消。Enabled=false 时仅注册 + 心跳，不参与 Leader 竞逐。
func (c *Coordinator) Run(ctx context.Context) {
	if c.cfg.Heartbeat <= 0 {
		c.cfg.Heartbeat = 5 * time.Second
	}
	if c.cfg.LeaseTTL <= 0 {
		c.cfg.LeaseTTL = 10 * time.Second
	}
	c.register(ctx)
	t := time.NewTicker(c.cfg.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.releaseLeader(ctx)
			return
		case <-c.stop:
			c.releaseLeader(ctx)
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

// Stop 主动停止协调（优雅退出时调用）。
func (c *Coordinator) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

// register 注册/刷新本进程行。
func (c *Coordinator) register(ctx context.Context) {
	inst, err := c.repo.Upsert(ctx, Register{
		InstanceID: c.cfg.InstanceID,
		Addr:       c.cfg.Addr,
		Hostname:   c.cfg.Hostname,
		Version:    c.cfg.Version,
	}, StatusOnline, RoleFollower, nil)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("gateway instance register failed", zap.String("err", err.Error()))
		}
		return
	}
	c.dbID = inst.ID
	if c.logger != nil {
		c.logger.Info("gateway instance registered",
			zap.String("instance_id", c.cfg.InstanceID),
			zap.String("id", inst.ID.String()))
	}
}

// tick 每次心跳：续期行 + 若启用协调则尝试维持/竞逐 Leader。
func (c *Coordinator) tick(ctx context.Context) {
	if c.dbID == uuid.Nil {
		c.register(ctx)
		return
	}

	if !c.cfg.Enabled {
		_, err := c.repo.Heartbeat(ctx, c.dbID, RoleFollower, nil)
		if err != nil && c.logger != nil {
			c.logger.Warn("gateway heartbeat failed", zap.String("err", err.Error()))
		}
		return
	}

	// 已持有 leader：先续期（Redis 优先，失败降级 DB），续期失败则降为 follower 重竞逐。
	if c.isLeader {
		if c.renewLeader(ctx) {
			return
		}
		c.isLeader = false
		if c.logger != nil {
			c.logger.Warn("gateway lost leader lease, re-acquiring")
		}
	}

	if c.acquireLeader(ctx) {
		c.isLeader = true
		if c.logger != nil {
			c.logger.Info("gateway became leader", zap.String("instance_id", c.cfg.InstanceID))
		}
	}
}

// renewLeader 尝试续期当前持有的 leader。与 acquireLeader 同分层：
//   - Redis 锁仍归本实例（GET value == 自己）→ EXPIRE 续期，成功即维持；
//   - Redis 锁被他人持有 → 立即让位（不续 DB，避免双主）；
//   - Redis 异常/未配置 → DB lease 原子续期（lease_until 比较）。
func (c *Coordinator) renewLeader(ctx context.Context) bool {
	if c.redis != nil {
		v, err := c.redis.Client.Get(ctx, c.redis.Key(leaderKeyName)).Result()
		switch {
		case err == nil && v == c.cfg.InstanceID:
			// 锁仍归本实例：EXPIRE 续期 Redis 锁，并同步刷新 DB role/lease，
			// 使 HA 查询（读 DB）始终能看到当前有效 leader。
			if e := c.redis.Client.Expire(ctx, c.redis.Key(leaderKeyName), c.cfg.LeaseTTL).Err(); e == nil {
				c.updateRole(ctx, RoleLeader)
				return true
			}
			// EXPIRE 失败按 Redis 异常降级 DB。
			if c.logger != nil {
				c.logger.Warn("redis renew leader expire failed, falling back to db lease",
					zap.String("err", err.Error()))
			}
		case err == nil && v != c.cfg.InstanceID:
			// 锁已转移给其他实例：本实例不再是 leader，让位重竞逐。
			return false
		case err != nil:
			// Redis 异常 → 降级 DB lease。
			if c.logger != nil {
				c.logger.Warn("redis renew leader failed, falling back to db lease",
					zap.String("err", err.Error()))
			}
		}
	}
	// DB lease 续期（Redis 未配置或异常降级；仅在此路径尝试）。
	ok, err := c.repo.TryRenewLeaseDB(ctx, c.dbID, leaseUntil(c.cfg.LeaseTTL))
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("db lease renew failed", zap.String("err", err.Error()))
		}
		return false
	}
	return ok
}

// acquireLeader 竞逐 leader。单一事实来源分层：
//   - Redis 可用且锁空闲 → SET NX PX 抢到，成为 leader（此时也刷新 DB role=leader）；
//   - Redis 可用但锁被他人持有 → 本实例为 follower，不再碰 DB lease（避免 DB 双主）；
//   - Redis 不可用/未配置 → 降级 DB lease 原子竞逐（tryAcquireDB 的 lease_until 比较保证单主）。
func (c *Coordinator) acquireLeader(ctx context.Context) bool {
	// Redis 优先：SET NX PX 原子抢锁。
	if c.redis != nil {
		acquired, err := c.redis.Client.SetNX(ctx, c.redis.Key(leaderKeyName), c.cfg.InstanceID,
			c.cfg.LeaseTTL).Result()
		if err != nil {
			// Redis 异常 → 降级 DB lease（DB 原子比较保证单主）。
			if c.logger != nil {
				c.logger.Warn("redis acquire leader failed, falling back to db lease",
					zap.String("err", err.Error()))
			}
			return c.acquireLeaderDB(ctx)
		}
		if acquired {
			c.updateRole(ctx, RoleLeader)
			return true
		}
		// 锁被其他实例持有：本实例是 follower，不再抢 DB lease（防止 Redis 与 DB 双主并存）。
		return false
	}
	// 无 Redis：纯 DB lease。
	return c.acquireLeaderDB(ctx)
}

// acquireLeaderDB 用 DB lease 原子竞逐（Redis 不可用/未配置时的降级路径）。
func (c *Coordinator) acquireLeaderDB(ctx context.Context) bool {
	ok, err := c.tryAcquireDB(ctx)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("db lease acquire failed", zap.String("err", err.Error()))
		}
		return false
	}
	return ok
}

func (c *Coordinator) tryAcquireDB(ctx context.Context) (bool, error) {
	ok, err := c.repo.TryAcquireLeaseDB(ctx, c.dbID, RoleLeader, leaseUntil(c.cfg.LeaseTTL))
	if err != nil {
		return false, err
	}
	if ok {
		c.updateRole(ctx, RoleLeader)
	}
	return ok, nil
}

func (c *Coordinator) updateRole(ctx context.Context, role Role) {
	_, _ = c.repo.Heartbeat(ctx, c.dbID, role, leaseUntilPtr(c.cfg.LeaseTTL))
}

// releaseLeader 退出时释放 leader（尽力而为）。
func (c *Coordinator) releaseLeader(ctx context.Context) {
	if c.isLeader && c.redis != nil {
		// 仅当锁仍由本实例持有时删除，避免误删他人锁。
		v, err := c.redis.Client.Get(ctx, c.redis.Key(leaderKeyName)).Result()
		if err == nil && v == c.cfg.InstanceID {
			_ = c.redis.Client.Del(ctx, c.redis.Key(leaderKeyName)).Err()
		}
	}
	if c.dbID != uuid.Nil {
		now := time.Now()
		_, _ = c.repo.Heartbeat(ctx, c.dbID, RoleFollower, &now)
	}
}

// leaderKeyName 是全局 Leader 锁键名（所有实例竞逐同一把锁，value 存持有者 instance_id）。
// 实际 Redis 键由 redis.Prefix 统一前缀：prefix:ha:leader。
const leaderKeyName = "ha:leader"

func leaseUntil(ttl time.Duration) time.Time { return time.Now().Add(ttl) }
func leaseUntilPtr(ttl time.Duration) *time.Time {
	t := leaseUntil(ttl)
	return &t
}
