// Package applications 保存 Compose 应用（Application）及其版本规格，供 CM 驱动部署。
//
// 与「部署期望态」（desired，版本→单容器）并列：本包面向整包 Compose 应用，
// 一份 Application 可有多个版本，每个版本持有一份 Compose 规格（YAML）。
// 配置了数据库则落 cm_applications / cm_application_versions；否则退化为进程内存储。
package applications

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Application 是一个 Compose 应用。
type Application struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"` // active / stopped / removed
	NodeID      string    `json:"node_id,omitempty"`
	ServiceID   string    `json:"service_id,omitempty"` // 关联的 Gateway 服务（可空）
	// Version 是当前部署的版本号；Spec 为该版本 Compose 规格（YAML 原文）。
	Version      string    `json:"version,omitempty"`
	Spec         string    `json:"spec,omitempty"`
	TargetWeight int       `json:"target_weight,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store 是 Application 存储。
type Store struct {
	db *sql.DB

	mu   sync.RWMutex
	apps map[string]Application
}

// NewStore 构造。db 为空则退化为进程内存储。
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, apps: map[string]Application{}}
}

// Load 从数据库装载 Application（无数据库时为无操作）。
func (s *Store) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, COALESCE(tenant_id::text,''), name, COALESCE(description,''),
		       status, COALESCE(node_id,''), COALESCE(service_id::text,''),
		       COALESCE(version,''), COALESCE(spec,''), COALESCE(target_weight,0),
		       created_at, updated_at
		FROM cm_applications`)
	if err != nil {
		return fmt.Errorf("load cm_applications: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a Application
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Description, &a.Status,
			&a.NodeID, &a.ServiceID, &a.Version, &a.Spec, &a.TargetWeight,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return fmt.Errorf("scan cm_application: %w", err)
		}
		s.apps[a.ID] = a
	}
	return rows.Err()
}

// Put 写入/覆盖一个 Application，返回存储后的对象（含新生成的 id，调用方无需再查）。
func (s *Store) Put(a Application) Application {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	s.mu.Lock()
	// 收敛历史脏键：删除 id 相同但键不同的残留条目，保证一个 id 对应唯一记录。
	for k, v := range s.apps {
		if k != a.ID && v.ID == a.ID {
			delete(s.apps, k)
		}
	}
	s.apps[a.ID] = a
	s.mu.Unlock()
	s.persist(a)
	return a
}

// Get 取一个 Application。
func (s *Store) Get(id string) (Application, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.apps[id]
	return a, ok
}

// Delete 删除一个 Application。按 id 与 map 键双向匹配，兼容个别脏键残留记录。
func (s *Store) Delete(id string) {
	s.mu.Lock()
	for k, v := range s.apps {
		if k == id || v.ID == id {
			delete(s.apps, k)
		}
	}
	s.mu.Unlock()
	if s.db != nil {
		_, _ = s.db.ExecContext(context.Background(), `DELETE FROM cm_applications WHERE id=$1::uuid`, id)
	}
}

// List 返回全部 Application（按创建时间倒序）。
func (s *Store) List() []Application {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Application, 0, len(s.apps))
	for _, a := range s.apps {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// SetStatus 更新 Application 的运行态（部署/停止/移除后回写）。
func (s *Store) SetStatus(id, status string) {
	s.mu.Lock()
	a, ok := s.apps[id]
	if ok {
		a.Status = status
		a.UpdatedAt = time.Now()
		s.apps[id] = a
	}
	s.mu.Unlock()
	if ok {
		s.persist(a)
	}
}

func (s *Store) persist(a Application) {
	if s.db == nil {
		return
	}
	_, _ = s.db.ExecContext(context.Background(), `
		INSERT INTO cm_applications (id, tenant_id, name, description, status, node_id, service_id,
			version, spec, target_weight, created_at, updated_at)
		VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, NULLIF($7,'')::uuid,
			$8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, description=EXCLUDED.description, status=EXCLUDED.status,
			node_id=EXCLUDED.node_id, service_id=EXCLUDED.service_id, version=EXCLUDED.version,
			spec=EXCLUDED.spec, target_weight=EXCLUDED.target_weight, updated_at=EXCLUDED.updated_at`,
		a.ID, a.TenantID, a.Name, a.Description, a.Status, a.NodeID, a.ServiceID,
		a.Version, a.Spec, a.TargetWeight, a.CreatedAt, a.UpdatedAt)
}
