package agentprotocol

// 本文件定义 Agent 允许执行的 Action 白名单。CM 只下发白名单内的 Action，
// Agent 侧在收到 task.execute 时同样以白名单校验，双向把关。
//
// 明确禁止：shell.exec / host.exec / command.exec —— 不允许经 Agent 执行任意宿主命令。

const (
	// 系统 / Docker 信息。
	ActionSystemInfo  = "system.info"
	ActionDockerInfo  = "docker.info"
	ActionNodeMetrics = "node.metrics"

	// 容器。
	ActionContainerList    = "container.list"
	ActionContainerInspect = "container.inspect"
	ActionContainerCreate  = "container.create"
	ActionContainerStart   = "container.start"
	ActionContainerStop    = "container.stop"
	ActionContainerRestart = "container.restart"
	ActionContainerRemove  = "container.remove"
	ActionContainerStats   = "container.stats"

	// 镜像。
	ActionImageList    = "image.list"
	ActionImageInspect = "image.inspect"
	ActionImagePull    = "image.pull"
	ActionImageRemove  = "image.remove"

	// 网络。
	ActionNetworkList    = "network.list"
	ActionNetworkInspect = "network.inspect"
	ActionNetworkCreate  = "network.create"
	ActionNetworkRemove  = "network.remove"

	// 卷。
	ActionVolumeList    = "volume.list"
	ActionVolumeInspect = "volume.inspect"
	ActionVolumeCreate  = "volume.create"
	ActionVolumeRemove  = "volume.remove"

	// Application（Compose）。
	ActionApplicationValidate = "application.validate"
	ActionApplicationDeploy   = "application.deploy"
	ActionApplicationStop     = "application.stop"
	ActionApplicationRestart  = "application.restart"
	ActionApplicationRemove   = "application.remove"
	ActionApplicationPs       = "application.ps"

	// 日志。
	ActionLogsRead = "logs.read"
)

// allowedActions 是全部合法 Action 的集合。
var allowedActions = map[string]struct{}{
	ActionSystemInfo: {}, ActionDockerInfo: {}, ActionNodeMetrics: {},
	ActionContainerList: {}, ActionContainerInspect: {}, ActionContainerCreate: {},
	ActionContainerStart: {}, ActionContainerStop: {}, ActionContainerRestart: {},
	ActionContainerRemove: {}, ActionContainerStats: {},
	ActionImageList: {}, ActionImageInspect: {}, ActionImagePull: {}, ActionImageRemove: {},
	ActionNetworkList: {}, ActionNetworkInspect: {}, ActionNetworkCreate: {}, ActionNetworkRemove: {},
	ActionVolumeList: {}, ActionVolumeInspect: {}, ActionVolumeCreate: {}, ActionVolumeRemove: {},
	ActionApplicationValidate: {}, ActionApplicationDeploy: {}, ActionApplicationStop: {},
	ActionApplicationRestart: {}, ActionApplicationRemove: {}, ActionApplicationPs: {},
	ActionLogsRead: {},
}

// forbiddenActions 是显式禁止的 Action（便于测试与文档化，不进入白名单）。
var forbiddenActions = map[string]struct{}{
	"shell.exec":   {},
	"host.exec":    {},
	"command.exec": {},
}

// IsAllowedAction 报告 action 是否在白名单内。
func IsAllowedAction(action string) bool {
	_, ok := allowedActions[action]
	return ok
}

// IsForbiddenAction 报告 action 是否为显式禁止项。
func IsForbiddenAction(action string) bool {
	_, ok := forbiddenActions[action]
	return ok
}
