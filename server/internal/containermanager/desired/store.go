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
}

// NewStore 构造空存储。
func NewStore() *Store {
	return &Store{
		states: map[uuid.UUID]State{},
		phases: map[uuid.UUID]Phase{},
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
