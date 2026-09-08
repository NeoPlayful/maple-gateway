package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/ent"
	entsetting "github.com/NeoPlayful/maple-gateway/server/ent/setting"
	enthistory "github.com/NeoPlayful/maple-gateway/server/ent/settinghistory"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// Repository 是 settings 数据访问层（DB 走 Ent，读缓存走内存）。
type Repository struct {
	ent   *ent.Client
	mu    sync.RWMutex
	cache map[string]Entry // section:key → Entry
}

// NewRepository 构造。
func NewRepository(client *ent.Client) *Repository {
	return &Repository{ent: client, cache: map[string]Entry{}}
}

func toEntry(e *ent.Setting) Entry {
	return Entry{
		Section:   Section(e.Section),
		Key:       e.Key,
		Value:     e.Value,
		Version:   e.Version,
		UpdatedAt: e.UpdatedAt,
	}
}

// All 返回全部条目（按 section,key 排序）。
func (r *Repository) All(ctx context.Context) ([]Entry, error) {
	es, err := r.ent.Setting.Query().
		Order(entsetting.BySection(), entsetting.ByKey()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all settings: %w", err)
	}
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		out = append(out, toEntry(e))
	}
	return out, nil
}

// Upsert 设置单条：存在则 version+1 更新，否则插入 v1。
// 覆盖前把旧 (version, value) 写入 settings_history，支撑按版本回滚。
func (r *Repository) Upsert(ctx context.Context, section Section, key string, value json.RawMessage) (*Entry, error) {
	if value == nil {
		value = json.RawMessage("null")
	}
	// 先读当前：若存在，把将被覆盖的旧版本值记入历史。
	cur, err := r.ent.Setting.Query().
		Where(
			entsetting.Section(string(section)),
			entsetting.Key(key),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("read setting before upsert: %w", err)
	}
	now := time.Now()
	if cur != nil {
		if err := r.appendHistory(ctx, section, key, cur.Version, cur.Value, now); err != nil {
			return nil, err
		}
	}
	err = r.ent.Setting.Create().
		SetSection(string(section)).
		SetKey(key).
		SetValue(value).
		SetVersion(1).
		SetUpdatedAt(now).
		OnConflictColumns("section", "key").
		Update(func(u *ent.SettingUpsert) {
			u.UpdateValue()
			u.AddVersion(1)
			u.SetUpdatedAt(now)
		}).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert setting: %w", err)
	}
	// upsert 后按 (section,key) 读回最新 Entry。
	e, err := r.ent.Setting.Query().
		Where(
			entsetting.Section(string(section)),
			entsetting.Key(key),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("get setting after upsert: %w", err)
	}
	ent := toEntry(e)
	return &ent, nil
}

// appendHistory 把一条 (version,value) 追加进 settings_history。
func (r *Repository) appendHistory(ctx context.Context, section Section, key string,
	version int, value json.RawMessage, at time.Time) error {
	_, err := r.ent.SettingHistory.Create().
		SetSection(string(section)).
		SetKey(key).
		SetVersion(version).
		SetValue(value).
		SetCreatedAt(at).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("append setting history: %w", err)
	}
	return nil
}

// History 返回某 section:key 的全部历史（按 version 升序），含当前值摘要。
func (r *Repository) History(ctx context.Context, section Section, key string) ([]HistoryEntry, error) {
	hs, err := r.ent.SettingHistory.Query().
		Where(
			enthistory.Section(string(section)),
			enthistory.Key(key),
		).
		Order(enthistory.ByVersion()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list setting history: %w", err)
	}
	out := make([]HistoryEntry, 0, len(hs))
	for _, h := range hs {
		out = append(out, HistoryEntry{
			Version:   h.Version,
			Value:     h.Value,
			ChangedAt: h.CreatedAt,
		})
	}
	return out, nil
}

// Rollback 把某 key 回滚到指定历史 version：取该版本值执行一次 upsert。
// 返回回滚后的最新 Entry。目标版本须小于当前 version。
func (r *Repository) Rollback(ctx context.Context, section Section, key string, targetVersion int) (*Entry, error) {
	cur, err := r.ent.Setting.Query().
		Where(
			entsetting.Section(string(section)),
			entsetting.Key(key),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pkg.ErrNotFound(fmt.Sprintf("配置 %s:%s 不存在", section, key))
		}
		return nil, fmt.Errorf("read setting for rollback: %w", err)
	}
	if targetVersion >= cur.Version {
		return nil, pkg.ErrValidation(fmt.Sprintf("回滚版本 %d 须小于当前版本 %d", targetVersion, cur.Version))
	}
	hist, err := r.ent.SettingHistory.Query().
		Where(
			enthistory.Section(string(section)),
			enthistory.Key(key),
			enthistory.Version(targetVersion),
		).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, pkg.ErrNotFound(fmt.Sprintf("未找到版本 %d 的历史", targetVersion))
	}
	if err != nil {
		return nil, fmt.Errorf("read setting history for rollback: %w", err)
	}
	return r.Upsert(ctx, section, key, hist.Value)
}

// Reload 把 DB 全量载入内存缓存。
func (r *Repository) Reload(ctx context.Context) error {
	items, err := r.All(ctx)
	if err != nil {
		return err
	}
	cache := make(map[string]Entry, len(items))
	for _, e := range items {
		cache[string(e.Section)+":"+e.Key] = e
	}
	r.mu.Lock()
	r.cache = cache
	r.mu.Unlock()
	return nil
}

// Get 读某 section:key 的当前值（内存缓存）。不存在返回 nil。
func (r *Repository) Get(section Section, key string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.cache[string(section)+":"+key]
	return e, ok
}

// GetString 读字符串值（如 "200ms" / "info"）。不存在或非法返回 def。
func (r *Repository) GetString(section Section, key, def string) string {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var s string
	if err := json.Unmarshal(e.Value, &s); err != nil {
		return def
	}
	return s
}

// GetInt 读整数值。不存在或非法返回 def。
func (r *Repository) GetInt(section Section, key string, def int) int {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var v int
	if err := json.Unmarshal(e.Value, &v); err != nil {
		return def
	}
	return v
}

// GetBool 读布尔值（如 logging.debug）。不存在或非法返回 def。
func (r *Repository) GetBool(section Section, key string, def bool) bool {
	e, ok := r.Get(section, key)
	if !ok {
		return def
	}
	var v bool
	if err := json.Unmarshal(e.Value, &v); err != nil {
		return def
	}
	return v
}

// GetSection 返回某 section 的全部键值（供管理端读）。
func (r *Repository) GetSection(section Section) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Entry{}
	for k, e := range r.cache {
		if strings.HasPrefix(k, string(section)+":") {
			out = append(out, e)
		}
	}
	return out
}
