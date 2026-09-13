// Package desired 保存 Gateway 下发的部署期望态，供对账器读取。
//
// 配置了数据库则持久化到 cm_deployments（CM 重启后期望态与编排进度不丢）；
// 否则退化为进程内存储（开发用）。
package desired

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// State 是 Gateway 下发的一份部署期望态（一个版本对应一份）。
type State struct {
	DeploymentID uuid.UUID `json:"deployment_id"`
	ServiceID    uuid.UUID `json:"service_id"`
	VersionID    uuid.UUID `json:"version_id"`
	Version      string    `json:"version"`
	// Status 版本角色（stable/active/canary/standby/draining/inactive）。
	Status       string            `json:"status,omitempty"`
	Image        string            `json:"image"`
	Replicas     int               `json:"replicas"`
	Port         int               `json:"port"`
	Env          map[string]string `json:"env,omitempty"`
	Resources    json.RawMessage   `json:"resources,omitempty"`
	HealthPath   string            `json:"health_path,omitempty"`
	NodeSelector map[string]string `json:"node_selector,omitempty"`
	Strategy     string            `json:"strategy,omitempty"`
}

// Phase 是一次编排阶段状态（供状态查询）。
type Phase struct {
	Status  string `json:"status"` // pending / reconciling / ready / stopped / failed
	Message string `json:"message,omitempty"`
}

// Store 是期望态与编排进度的存储。
type Store struct {
	db *sql.DB

	mu     sync.RWMutex
	states map[uuid.UUID]State
	phases map[uuid.UUID]Phase
	// paused 是人工置为维护（手动 stop）的实例：instance_id → version_id。
	paused map[string]string
}

// NewStore 构造空存储。db 为空则退化为进程内存储。
func NewStore(db *sql.DB) *Store {
	return &Store{
		db:     db,
		states: map[uuid.UUID]State{},
		phases: map[uuid.UUID]Phase{},
		paused: map[string]string{},
	}
}

// Load 从数据库装载期望态与编排进度（无数据库时为无操作）。
func (s *Store) Load(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT deployment_id::text, COALESCE(service_id::text,''), COALESCE(version_id::text,''),
		       version, status, image, replicas, port, env, resources,
		       health_path, node_selector, strategy, phase, phase_message, stopped
		FROM cm_deployments`)
	if err != nil {
		return fmt.Errorf("load cm_deployments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			st, sid, vid, version, status, image, healthPath, strategy, phase, phaseMsg string
			replicas, port                                                              int
			env, resources, selector                                                    []byte
			stopped                                                                     bool
		)
		if err := rows.Scan(&st, &sid, &vid, &version, &status, &image, &replicas, &port,
			&env, &resources, &healthPath, &selector, &strategy, &phase, &phaseMsg, &stopped); err != nil {
			return fmt.Errorf("scan cm_deployment: %w", err)
		}
		did := uuid.MustParse(st)
		if !stopped {
			s.states[did] = State{
				DeploymentID: did,
				ServiceID:    parseUUID(sid),
				VersionID:    parseUUID(vid),
				Version:      version,
				Status:       status,
				Image:        image,
				Replicas:     replicas,
				Port:         port,
				Env:          decodeStrMap(env),
				Resources:    json.RawMessage(resources),
				HealthPath:   healthPath,
				NodeSelector: decodeStrMap(selector),
				Strategy:     strategy,
			}
		}
		s.phases[did] = Phase{Status: phase, Message: phaseMsg}
	}
	return rows.Err()
}

// Put 写入/覆盖某部署的期望态。
func (s *Store) Put(st State) {
	s.mu.Lock()
	s.states[st.DeploymentID] = st
	s.phases[st.DeploymentID] = Phase{Status: "pending"}
	s.mu.Unlock()
	s.persistState(st, false)
}

// Get 读取某部署的期望态。
func (s *Store) Get(deploymentID uuid.UUID) (State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[deploymentID]
	return st, ok
}

// Stop 标记某部署停止（保留进度记录供状态查询）。
func (s *Store) Stop(deploymentID uuid.UUID) {
	s.mu.Lock()
	delete(s.states, deploymentID)
	s.phases[deploymentID] = Phase{Status: "stopped"}
	s.mu.Unlock()
	s.persistStop(deploymentID)
}

// Phase 返回某部署的当前编排阶段。
func (s *Store) Phase(deploymentID uuid.UUID) Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.phases[deploymentID]; ok {
		return p
	}
	return Phase{Status: "unknown"}
}

// SetPhase 回写某部署的编排阶段（对账器每轮结算后调用）。
func (s *Store) SetPhase(deploymentID uuid.UUID, phase Phase) {
	s.mu.Lock()
	s.phases[deploymentID] = phase
	s.mu.Unlock()
	if s.db != nil {
		_, _ = s.db.ExecContext(context.Background(),
			`UPDATE cm_deployments SET phase=$2, phase_message=$3, updated_at=now() WHERE deployment_id=$1::uuid`,
			deploymentID.String(), phase.Status, phase.Message)
	}
}

// All 返回全部期望态（对账器读取用）。
func (s *Store) All() []State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]State, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st)
	}
	return out
}

// AllPhases 返回全部部署的编排进度（管理读接口用）。
func (s *Store) AllPhases() map[uuid.UUID]Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[uuid.UUID]Phase, len(s.phases))
	for k, v := range s.phases {
		out[k] = v
	}
	return out
}

// Pause 把实例标记为人工维护（手动 stop 后登记），避免被对账器立即重建。
func (s *Store) Pause(instanceID, versionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused[instanceID] = versionID
}

// Resume 清除实例的人工维护标记（手动 start 后恢复由对账器接管）。
func (s *Store) Resume(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.paused, instanceID)
}

// PausedCount 返回各版本被人工置为维护的实例数（instance 计入实际副本数用）。
func (s *Store) PausedCount() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.paused))
	for _, vid := range s.paused {
		out[vid]++
	}
	return out
}

func (s *Store) persistState(st State, stopped bool) {
	if s.db == nil {
		return
	}
	env, _ := json.Marshal(st.Env)
	selector, _ := json.Marshal(st.NodeSelector)
	if len(st.Resources) == 0 {
		st.Resources = json.RawMessage("null")
	}
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO cm_deployments (deployment_id, service_id, version_id, version, status, image,
			replicas, port, env, resources, health_path, node_selector, strategy,
			phase, phase_message, stopped, updated_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'pending','',$14,now())
		ON CONFLICT (deployment_id) DO UPDATE SET
			service_id=EXCLUDED.service_id, version_id=EXCLUDED.version_id, version=EXCLUDED.version,
			status=EXCLUDED.status, image=EXCLUDED.image, replicas=EXCLUDED.replicas, port=EXCLUDED.port,
			env=EXCLUDED.env, resources=EXCLUDED.resources, health_path=EXCLUDED.health_path,
			node_selector=EXCLUDED.node_selector, strategy=EXCLUDED.strategy,
			phase='pending', phase_message='', stopped=false, updated_at=now()`,
		st.DeploymentID.String(), st.ServiceID.String(), st.VersionID.String(), st.Version, st.Status,
		st.Image, st.Replicas, st.Port, env, []byte(st.Resources), st.HealthPath, selector, st.Strategy, stopped)
	if err != nil {
		// 落库失败不阻断内存编排；下一次 Put 会重试覆盖。
		return
	}
}

func (s *Store) persistStop(deploymentID uuid.UUID) {
	if s.db == nil {
		return
	}
	_, _ = s.db.ExecContext(context.Background(),
		`UPDATE cm_deployments SET phase='stopped', stopped=true, updated_at=now() WHERE deployment_id=$1::uuid`,
		deploymentID.String())
}

func parseUUID(s string) uuid.UUID {
	if s == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func decodeStrMap(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
