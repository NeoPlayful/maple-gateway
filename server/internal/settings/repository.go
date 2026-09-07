package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 是 settings 数据访问层。
type Repository struct {
	pool *pgxpool.Pool
	mu   sync.RWMutex
	cache map[string]Entry // section:key → Entry
}

// NewRepository 构造。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, cache: map[string]Entry{}}
}

const cols = `section, key, value, version, updated_at`

func scanEntry(row pgx.Row) (Entry, error) {
	var e Entry
	err := row.Scan(&e.Section, &e.Key, &e.Value, &e.Version, &e.UpdatedAt)
	return e, err
}

// All 返回全部条目（按 section,key 排序）。
func (r *Repository) All(ctx context.Context) ([]Entry, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM settings ORDER BY section, key`)
	if err != nil {
		return nil, fmt.Errorf("all settings: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Upsert 设置单条：存在则 version+1 更新，否则插入 v1。
func (r *Repository) Upsert(ctx context.Context, section Section, key string, value json.RawMessage) (*Entry, error) {
	if value == nil {
		value = json.RawMessage("null")
	}
	var e Entry
	err := r.pool.QueryRow(ctx, `
		INSERT INTO settings(section, key, value, version)
		VALUES($1, $2, $3, 1)
		ON CONFLICT (section, key)
		DO UPDATE SET value=EXCLUDED.value, version=settings.version+1, updated_at=now()
		RETURNING `+cols,
		string(section), key, []byte(value)).Scan(&e.Section, &e.Key, &e.Value, &e.Version, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert setting: %w", err)
	}
	return &e, nil
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
