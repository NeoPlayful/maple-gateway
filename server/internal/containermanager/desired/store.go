// Package desired 保存 Gateway 下发的部署期望态，供对账器读取。
//
// 当前为进程内存储（桩）：接收并留存期望态，验证 Gateway → CM 下发通道。
// 期望态持久化（cm_deployments / cm_replicas 表）与对账落地随后续编排任务接入。
package desired

import (
	"encoding/json"
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

// Store 是期望态与编排进度的进程内存储。
type Store struct {
	mu     sync.RWMutex
	states map[uuid.UUID]State // key: deployment_id
	phases map[uuid.UUID]Phase // key: deployment_id
	// paused 是人工置为维护（手动 stop）的实例：instance_id → version_id。
	// 对账器把它计入实际副本数，避免管理员手动停掉的实例被立即重建。
	paused map[string]string
}

// NewStore 构造空存储。
func NewStore() *Store {
	return &Store{
		states: map[uuid.UUID]State{},
		phases: map[uuid.UUID]Phase{},
		paused: map[string]string{},
	}
}

// Put 写入/覆盖某部署的期望态。
func (s *Store) Put(st State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[st.DeploymentID] = st
	s.phases[st.DeploymentID] = Phase{Status: "pending"}
}

// Get 读取某部署的期望态。
func (s *Store) Get(deploymentID uuid.UUID) (State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[deploymentID]
	return st, ok
}

// Stop 标记某部署停止（保留记录供状态查询）。
func (s *Store) Stop(deploymentID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, deploymentID)
	s.phases[deploymentID] = Phase{Status: "stopped"}
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
	defer s.mu.Unlock()
	s.phases[deploymentID] = phase
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
