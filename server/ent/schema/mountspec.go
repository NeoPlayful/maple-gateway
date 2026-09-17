package schema

// MountSpec 是绑定挂载的持久化形态（deployment_versions.mounts / cm_deployments.mounts）。
//
// Path 相对节点数据根，形如 <租户标识>/<模板标识>/<项目标识>/<子目录>；宿主绝对路径由
// 节点用自身 data_dir 拼出并做越界校验，控制面无法指定任意的宿主目录。
// 领域模型 deployment.Mount 与本类型字段一致，序列化后可直接互转。
type MountSpec struct {
	Path     string `json:"path"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}
