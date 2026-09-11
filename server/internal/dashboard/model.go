// Package dashboard 提供管理端 Dashboard 的只读聚合接口。
// 前端 Dashboard 由后端聚合，避免逐列表接口拼接计数与状态。
package dashboard

// Overview 是 Dashboard 总览的聚合数据源。
type Overview struct {
	Counts        Counts    `json:"counts"`
	Instances     Instances `json:"instances"`
	Nodes         Nodes     `json:"nodes"`
	RunningCanary []Canary  `json:"running_canary,omitempty"` // 进行中的 canary 精简视图
	RunningBG     int       `json:"running_blue_green"`
	CacheHits     uint64    `json:"route_cache_hits"`
	CacheMisses   uint64    `json:"route_cache_misses"`
}

// Counts 资源计数。
type Counts struct {
	Tenants     int `json:"tenants"`
	Domains     int `json:"domains"`
	Services    int `json:"services"`
	Instances   int `json:"instances"`
	Nodes       int `json:"nodes"`
	Deployments int `json:"deployments"`
	Versions    int `json:"versions"`
	Policies    int `json:"traffic_policies"`
	RateLimits  int `json:"rate_limits"`
	Canary      int `json:"canary"`
	BlueGreen   int `json:"blue_green"`
	Users       int `json:"users"`
}

// Instances 实例聚合：总数 / 可路由数 / health 分布 / status 分布。
type Instances struct {
	Total    int            `json:"total"`
	Routable int            `json:"routable"`
	ByHealth map[string]int `json:"by_health"`
	ByStatus map[string]int `json:"by_status"`
}

// Nodes 节点聚合：总数 / 按状态分布（online 可从 by_status 读）。
type Nodes struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
}

// Canary 是 Dashboard 关心的进行中 canary 精简视图。
type Canary struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	ServiceName  string `json:"service_name,omitempty"`
	Name         string `json:"name"`
	Phase        string `json:"phase"`
	CanaryWeight int    `json:"canary_weight"`
	TargetWeight int    `json:"target_weight"`
}
