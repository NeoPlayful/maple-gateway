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

// Upsert 设置单条：存在则 version+1 更新，否则插入 v1（原子，Ent upsert）。
func (r *Repository) Upsert(ctx context.Context, section Section, key string, value json.RawMessage) (*Entry, error) {
	if value == nil {
		value = json.RawMessage("null")
	}
	now := time.Now()
	err := r.ent.Setting.Create().
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
