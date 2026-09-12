package health

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// fakeRepo 记录 SetHealth 调用，供断言。
type fakeRepo struct {
	mu      sync.Mutex
	insts   []*instance.Instance
	changes []string // "id:health"
}

func (f *fakeRepo) AllInstances(context.Context) ([]*instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*instance.Instance, len(f.insts))
	copy(out, f.insts)
	return out, nil
}

func (f *fakeRepo) SetHealth(_ context.Context, id uuid.UUID, h instance.Health) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, id.String()+":"+string(h))
	for _, in := range f.insts {
		if in.ID == id {
			in.Health = h
		}
	}
	return nil
}

func (f *fakeRepo) lastChange() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.changes) == 0 {
		return ""
	}
	return f.changes[len(f.changes)-1]
}

func instFromURL(u string, id uuid.UUID) *instance.Instance {
	parsed, _ := url.Parse(u)
	host := parsed.Host // host:port
	addr := host[:strings.LastIndex(host, ":")]
	port := 0
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			port = parsePort(host[i+1:])
			break
		}
	}
	return &instance.Instance{
		ID: id, Address: addr, Port: port, Protocol: "http",
		Status: instance.StatusEnabled, Health: instance.HealthUnknown,
	}
}

func parsePort(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func TestChecker_MarksUnhealthyAfterThreshold(t *testing.T) {
	id := uuid.New()
	up := httptest.NewServer(nil) // 任何请求都 200
	// 先探测正常注册，再把 server 关闭模拟故障。
	repo := &fakeRepo{insts: []*instance.Instance{instFromURL(up.URL, id)}}

	cfg := Config{
		Interval:         50 * time.Millisecond,
		Timeout:          500 * time.Millisecond,
		FailureThreshold: 2,
		SuccessThreshold: 1,
		GracePeriod:      0,
		Path:             "/health",
	}
	c := NewChecker(repo, cfg, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 先让 server 存活几轮，让状态稳定在 healthy。
	go c.Run(ctx)
	deadline := time.After(2 * time.Second)
	waitHealthy(t, repo, deadline)

	// 关闭 upstream → 应在一段时间内被标 unhealthy。
	up.Close()
	deadline = time.After(3 * time.Second)
	waitState(t, repo, instance.HealthUnhealthy, deadline)

	cancel()
}

func waitHealthy(t *testing.T, repo *fakeRepo, deadline <-chan time.Time) {
	t.Helper()
	for {
		if repo.lastChange() != "" {
			// 已发生变更，说明至少跑了一轮
			return
		}
		select {
		case <-deadline:
			t.Fatal("checker did not run in time")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitState(t *testing.T, repo *fakeRepo, want instance.Health, deadline <-chan time.Time) {
	t.Helper()
	for {
		repo.mu.Lock()
		cur := ""
		if len(repo.insts) > 0 {
			cur = string(repo.insts[0].Health)
		}
		repo.mu.Unlock()
		if cur == string(want) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("did not reach health %q, current %q, changes=%v", want, cur, repoChanges(repo))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func hasKey(c *Checker, m map[string]int, k string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := m[k]
	return ok
}

func hasStart(c *Checker, k string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.startedAt[k]
	return ok
}

// TestChecker_PrunesRemovedInstances 验证移出库存的实例状态被回收，存活实例状态保留。
func TestChecker_PrunesRemovedInstances(t *testing.T) {
	up := httptest.NewServer(nil)
	defer up.Close()

	keep := uuid.New()
	drop := uuid.New()
	repo := &fakeRepo{insts: []*instance.Instance{
		instFromURL(up.URL, keep),
		instFromURL(up.URL, drop),
	}}

	cfg := Config{
		Interval:         50 * time.Millisecond,
		Timeout:          500 * time.Millisecond,
		FailureThreshold: 2,
		SuccessThreshold: 1,
		GracePeriod:      0,
		Path:             "/health",
	}
	c := NewChecker(repo, cfg, zap.NewNop())
	ctx := context.Background()

	// 第一轮：两个实例都产生状态。
	c.checkOnce(ctx)
	if !hasKey(c, c.okCount, keep.String()) || !hasKey(c, c.okCount, drop.String()) {
		t.Fatalf("expected both instances tracked after first round")
	}
	if !hasStart(c, keep.String()) || !hasStart(c, drop.String()) {
		t.Fatalf("expected startedAt for both instances after first round")
	}

	// 移出 drop 实例后再扫一轮：其状态应被剪枝，keep 保留。
	repo.mu.Lock()
	repo.insts = []*instance.Instance{instFromURL(up.URL, keep)}
	repo.mu.Unlock()
	c.checkOnce(ctx)

	if !hasKey(c, c.okCount, keep.String()) || !hasStart(c, keep.String()) {
		t.Fatalf("live instance state must be retained")
	}
	if hasKey(c, c.okCount, drop.String()) || hasKey(c, c.failCount, drop.String()) || hasStart(c, drop.String()) {
		t.Fatalf("removed instance state must be pruned")
	}
}

func repoChanges(repo *fakeRepo) []string {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out := make([]string, len(repo.changes))
	copy(out, repo.changes)
	return out
}
