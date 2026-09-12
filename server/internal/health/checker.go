// Package health 提供主动健康检查与实例状态状态机。
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Config 健康检查参数（Phase 1 全局配置，服务级配置后续扩展）。
type Config struct {
	Interval         time.Duration
	Timeout          time.Duration
	FailureThreshold int
	SuccessThreshold int
	GracePeriod      time.Duration
	Path             string
}

// DefaultConfig 返回默认参数。
func DefaultConfig() Config {
	return Config{
		Interval:         10 * time.Second,
		Timeout:          3 * time.Second,
		FailureThreshold: 3,
		SuccessThreshold: 2,
		GracePeriod:      10 * time.Second,
		Path:             "/health",
	}
}

// Repo 抽象实例数据访问（便于测试注入）。
type Repo interface {
	AllInstances(ctx context.Context) ([]*instance.Instance, error)
	SetHealth(ctx context.Context, id uuid.UUID, h instance.Health) error
}

// instanceRepo 是 instance.Repository 到 Repo 的适配。
type instanceRepo struct {
	repo *instance.Repository
}

func (r instanceRepo) AllInstances(ctx context.Context) ([]*instance.Instance, error) {
	return r.repo.AllFlat(ctx)
}

func (r instanceRepo) SetHealth(ctx context.Context, id uuid.UUID, h instance.Health) error {
	_, err := r.repo.SetHealth(ctx, id, h)
	return err
}

// NewInstanceRepo 把 instance.Repository 包装为 health.Repo。
func NewInstanceRepo(repo *instance.Repository) Repo {
	return instanceRepo{repo: repo}
}

// Checker 周期扫描全部实例并更新健康状态。
type Checker struct {
	repo   Repo
	cfg    Config
	client *http.Client

	mu        sync.Mutex
	failCount map[string]int
	okCount   map[string]int
	startedAt map[string]time.Time
	logger    *zap.Logger
	metrics   *metrics.Registry // 可选；nil 时不采集实例健康指标
}

// WithMetrics 注入指标注册表（可选）。nil 时不采集。
func (c *Checker) WithMetrics(m *metrics.Registry) *Checker {
	c.metrics = m
	return c
}

// NewChecker 构造。
func NewChecker(repo Repo, cfg Config, logger *zap.Logger) *Checker {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Path == "" {
		cfg.Path = "/health"
	}
	return &Checker{
		repo:      repo,
		cfg:       cfg,
		client:    &http.Client{Timeout: cfg.Timeout},
		failCount: map[string]int{},
		okCount:   map[string]int{},
		startedAt: map[string]time.Time{},
		logger:    logger,
	}
}

// Run 阻塞运行健康检查循环，直到 ctx 取消。
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	// 启动先跑一轮。
	c.checkOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.checkOnce(ctx)
		}
	}
}

func (c *Checker) checkOnce(ctx context.Context) {
	insts, err := c.repo.AllInstances(ctx)
	if err != nil {
		c.logger.Warn("health check: list instances failed", zap.String("err", err.Error()))
		return
	}
	alive := make(map[string]struct{}, len(insts))
	for _, in := range insts {
		alive[in.ID.String()] = struct{}{}
		c.probeOne(ctx, in)
	}
	c.prune(alive)
}

// prune 删除已不在库存中的实例状态。键按实例 UUID 记录，容器重建会生成新 UUID、
// 旧行被删除，若不回收这三张 map 会随历史 UUID 累积无界增长。
func (c *Checker) prune(alive map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.failCount {
		if _, ok := alive[k]; !ok {
			delete(c.failCount, k)
		}
	}
	for k := range c.okCount {
		if _, ok := alive[k]; !ok {
			delete(c.okCount, k)
		}
	}
	for k := range c.startedAt {
		if _, ok := alive[k]; !ok {
			delete(c.startedAt, k)
		}
	}
}

func (c *Checker) probeOne(ctx context.Context, in *instance.Instance) {
	key := in.ID.String()
	now := time.Now()

	c.mu.Lock()
	start, ok := c.startedAt[key]
	if !ok {
		start = now
		c.startedAt[key] = now
	}
	inGrace := now.Sub(start) < c.cfg.GracePeriod
	c.mu.Unlock()

	// 宽限期内不探测不判定（避免容器刚启动抖动误摘除）。
	if inGrace {
		return
	}

	healthy := c.probe(in)
	if healthy {
		c.mu.Lock()
		c.failCount[key] = 0
		c.okCount[key]++
		oks := c.okCount[key]
		c.mu.Unlock()
		// 达到成功阈值 → 标记 healthy（从 non-healthy 恢复）。
		if oks >= c.cfg.SuccessThreshold && in.Health != instance.HealthHealthy {
			c.logger.Info("instance recovered", zap.String("instance", key), zap.String("addr", in.Endpoint()))
			_ = c.repo.SetHealth(ctx, in.ID, instance.HealthHealthy)
			c.setHealthMetric(in.ID, string(instance.HealthHealthy))
		}
	} else {
		c.mu.Lock()
		c.okCount[key] = 0
		c.failCount[key]++
		fails := c.failCount[key]
		c.mu.Unlock()
		// 达到失败阈值 → 标记 unhealthy（从路由池摘除）。
		if fails >= c.cfg.FailureThreshold && in.Health != instance.HealthUnhealthy {
			c.logger.Warn("instance marked unhealthy",
				zap.String("instance", key),
				zap.String("addr", in.Endpoint()),
				zap.Int("fails", fails))
			_ = c.repo.SetHealth(ctx, in.ID, instance.HealthUnhealthy)
			c.setHealthMetric(in.ID, string(instance.HealthUnhealthy))
		}
	}
}

// setHealthMetric 更新实例健康 gauge（instance_id + health 标签，值恒 1）。
func (c *Checker) setHealthMetric(id uuid.UUID, health string) {
	if c.metrics == nil {
		return
	}
	c.metrics.SetGauge("maple_instance_health", 1, map[string]string{
		"instance_id": id.String(),
		"health":      health,
	})
}

// probe 执行一次主动检查。
func (c *Checker) probe(in *instance.Instance) bool {
	scheme := in.Protocol
	if scheme == "" {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s%s", scheme, in.Endpoint(), c.cfg.Path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// compile 保证 instanceRepo 实现 Repo。
var _ Repo = instanceRepo{}
