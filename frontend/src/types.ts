// 与后端 JSON 对应的核心类型。

export interface Service {
  id: string;
  tenant_id: string;
  project_id?: string;
  name: string;
  protocol: string;
  status: string;
  created_at: string;
}

export interface Tenant {
  id: string;
  name: string;
  slug: string;
  status: string;
  description?: string;
  created_at: string;
}

export interface Domain {
  id: string;
  tenant_id: string;
  hostname: string;
  service_id?: string | null;
  status: string;
  verified_at?: string | null;
  created_at: string;
}

export interface Deployment {
  id: string;
  service_id: string;
  name: string;
  status: string;
  strategy: string;
  created_at: string;
}

// 绑定挂载：把节点数据根下的子目录映射进容器。
// path 相对节点数据目录（形如 <租户>/<模板>/<项目>/<子目录>），宿主绝对路径由节点拼出。
export interface Mount {
  path: string;
  target: string;
  read_only?: boolean;
}

export interface Version {
  id: string;
  deployment_id: string;
  // project_id 是版本所属项目（可空）：该版本创建的容器带 maple.project_id 标签，
  // 使单容器与 Compose 应用共用同一项目隔离单元（数据目录 <租户>/<模板>/<项目>）。
  project_id?: string | null;
  version: string;
  image?: string;
  weight: number;
  status: string;
  // 容器执行规格：Container Manager 据此创建容器（下发期望态）。
  replicas: number;
  port?: number;
  env?: Record<string, string>;
  resources?: unknown;
  health_path?: string;
  node_selector?: Record<string, string>;
  mounts?: Mount[];
  created_at: string;
}

// 全局版本列表行：版本规格 + 所属服务/部署归属（GET /api/admin/versions）。
export interface VersionWithOwner extends Version {
  service_id: string;
  service_name: string;
  deployment_name: string;
}

export interface Instance {
  id: string;
  service_id: string;
  deployment_id?: string | null;
  version_id?: string | null;
  node_id?: string | null;
  version?: string;
  address: string;
  port: number;
  protocol: string;
  weight: number;
  status: string;
  health: string;
  last_seen_at?: string | null;
  created_at: string;
}

export interface Node {
  id: string;
  name: string;
  host: string;
  region?: string;
  data_dir?: string;
  status: string;
  weight: number;
  last_seen_at?: string | null;
  created_at: string;
}

export interface TrafficPolicy {
  id: string;
  service_id: string;
  name: string;
  priority: number;
  match: {
    header?: Record<string, string>;
    cookie?: Record<string, string>;
    path?: string;
    path_exact?: string;
    percent?: number;
  };
  target_version_id?: string | null;
  weight: number;
  sticky?: { header_name?: string; cookie_name?: string; ttl_seconds?: number } | null;
  status: string;
  created_at: string;
}

// 统一发布：金丝雀与蓝绿收敛到同一资源，strategy 判别。
// primary=基准版本（canary 的 stable / bluegreen 的 active）；
// secondary=挑战版本（canary 的 canary / bluegreen 的另一色）。
export interface Release {
  id: string;
  strategy: 'canary' | 'bluegreen';
  deployment_id: string;
  service_id?: string | null;
  name: string;
  phase: string;
  primary_version_id: string;
  secondary_version_id: string;
  primary_weight: number;
  secondary_weight: number;
  previous_primary_id?: string | null;
  config: Record<string, unknown>;
  started_at?: string | null;
  finished_at?: string | null;
  created_at: string;
}

export interface ReleaseEvent {
  id: string;
  release_id: string;
  action: string;
  from_weight?: number | null;
  to_weight?: number | null;
  from_version?: string | null;
  to_version?: string | null;
  detail: string;
  created_at: string;
}

export interface AccessLogRow {
  timestamp: string;
  host: string;
  method: string;
  path: string;
  status: number;
  client_ip: string;
  duration_ms: number;
  request_id?: string;
  user_agent?: string;
  protocol?: string;
  query?: string;
  upstream?: string;
  gateway_instance?: string;
  node?: string;
  route_name?: string;
  // 转发头字段仅在来源可信（命中 trusted_proxies 白名单）时才有值。
  real_client_ip?: string;
  x_forwarded_for?: string;
  x_real_ip?: string;
}

export interface ErrorLogRow {
  timestamp: string;
  host: string;
  path: string;
  status: number;
  error: string;
  request_id?: string;
  method?: string;
  client_ip?: string;
  user_agent?: string;
  protocol?: string;
  query?: string;
  upstream?: string;
  gateway_instance?: string;
  node?: string;
  route_name?: string;
  real_client_ip?: string;
  x_forwarded_for?: string;
  x_real_ip?: string;
}

export interface AuditLogRow {
  id: string;
  admin_id?: string | null;
  action: string;
  target_type: string;
  target_id?: string | null;
  ip?: string | null;
  created_at: string;
}

export interface SettingsEntry {
  value: unknown;
  version: number;
  updated_at: string;
}
export type SettingsMap = Record<string, Record<string, SettingsEntry>>;

export interface RateLimit {
  id: string;
  scope: string; // global / tenant / domain / service / ip
  tenant_id?: string | null;
  domain_id?: string | null;
  service_id?: string | null;
  name: string;
  limit: number;
  window_seconds: number;
  burst?: number | null;
  response_code: number;
  status: string;
  created_at: string;
}

// ---- Container Manager 运行时运维数据（经 Gateway /api/admin/cm/* 代理）----

export interface HostMetrics {
  available: boolean;
  cpu_percent: number;
  mem_total: number;
  mem_used: number;
  mem_percent: number;
  disk_total: number;
  disk_used: number;
  disk_percent: number;
  load_avg_1?: number;
  uptime_sec?: number;
}

export interface DockerDisk {
  layers_size: number;
  images: number;
  containers: number;
  volumes: number;
}

export interface NodeMetric {
  node_name: string;
  metrics: { host: HostMetrics; docker: DockerDisk };
  error?: string;
}

export interface CMNodeStatus {
  name: string;
  host: string;
  region?: string;
  labels?: Record<string, string>;
  healthy: boolean;
  gateway_id?: string;
  last_seen_ms: number;
  // 节点容量与 Agent 元信息（来自 Agent /node/info）。
  cpus?: number;
  memory_bytes?: number;
  docker_version?: string;
  docker_api_version?: string;
}

export interface CMStats {
  node_up: number;
  containers: number;
  last_report_at: string;
  last_error?: string;
}

export interface CMPhase {
  status: string;
  message?: string;
}

export interface CMRuntimeError {
  node_name: string;
  instance_id: string;
  state: string;
  exit_code: number;
  oom_killed: boolean;
  at: number;
}

export interface CMOverview {
  stats?: CMStats;
  deployments?: Record<string, CMPhase>;
  nodes?: CMNodeStatus[];
}

// 受管容器的 Docker 事件（CM 观测经 /api/admin/cm/events 透出）。
export interface CMDockerEvent {
  node_id: string;
  action: string;
  container_id?: string;
  instance_id?: string;
  image?: string;
  exit_code?: number;
  at: number;
}

// 受管容器（CM 观测快照经 /api/admin/cm/containers 透出）。
export interface CMContainer {
  instance_id: string;
  container_id: string;
  name: string;
  image: string;
  state: string; // running / exited / created / dead …
  status: string; // 人类可读状态
  labels?: Record<string, string>;
  node_name: string;
  host_port: number;
  container_port: number;
  ip: string;
  exit_code: number;
  oom_killed: boolean;
  finished_at?: string;
}

// 单容器资源用量（CM 经 /api/admin/cm/instances/:id/stats 透出）。
// 速率类字段（*_bps）由 Agent 两次采样差分得出，首次采样为 0。
export interface CMContainerStats {
  container_id: string;
  cpu_percent: number;
  mem_usage: number;
  mem_limit: number;
  mem_percent: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  net_rx_bps: number;
  net_tx_bps: number;
  blk_read_bytes: number;
  blk_write_bytes: number;
  blk_read_bps: number;
  blk_write_bps: number;
  pids_current: number;
}

// 一次下发给 Agent 的任务（CM 任务系统经 /api/admin/cm/tasks 透出）。
export interface CMTask {
  id: string;
  node_id: string;
  action: string;
  params?: unknown;
  status: string; // pending / dispatching / running / success / failed / cancelled / timeout
  message?: string;
  percent?: number;
  error?: string;
  result?: unknown;
  request_id?: string;
  parent_task_id?: string;
  created_by?: string;
  created_at_ms: number;
  started_at_ms?: number;
  finished_at_ms?: number;
  deadline_ms?: number;
  attempts: number;
}

// 任务分页结果（GET /api/admin/cm/tasks 透出 CM 的 {tasks,total}）。
export interface CMTaskPage {
  tasks: CMTask[];
  total: number;
}

// 一个 Compose 应用（CM 应用模型经 /api/admin/cm/applications 透出）。
export interface CMApplication {
  id: string;
  name: string;
  description?: string;
  status: string; // created / running / stopped / removed / failed
  node_id?: string;
  service_id?: string;
  version?: string;
  spec?: string; // Compose 规格（YAML 原文）
  target_weight?: number;
  created_at: string;
  updated_at: string;
}

// Compose 应用内的一个服务运行态（compose ps）。
export interface CMAppService {
  name: string;
  service: string;
  state: string;
  status?: string;
  health?: string;
  image?: string;
  container_id?: string;
  publishers?: { url?: string; target_port?: number; published_port?: number; protocol?: string }[];
}

// ---- 应用模板与项目（容器创建的模板化之路）----

// 模板参数定义：渲染时用取值替换规格中的 {{key}}。
export interface TemplateParam {
  key: string;
  label: string;
  type: 'string' | 'number' | 'bool' | 'select';
  required?: boolean;
  default?: string;
  options?: string[];
  hint?: string;
}

// 一个应用模板：带 {{参数键}} 占位符的 Compose 规格 + 参数定义。
// slug 是模板标识（英文），作为数据目录第二段。
export interface AppTemplate {
  id: string;
  name: string;
  slug: string;
  description?: string;
  spec: string;
  params?: TemplateParam[];
  status: string;
  created_at: string;
  updated_at: string;
}

// 租户下的一个部署项目：项目名即项目标识（英文），作为数据目录第三段。
export interface Project {
  id: string;
  tenant_id: string;
  template_id: string;
  name: string;
  description?: string;
  status: string;
  node_id?: string;
  application_id?: string;
  created_at: string;
  updated_at: string;
}

// 节点上的一个项目级网络池（IPAM）：一段地址池按项目前缀切分给各项目。
export interface NetworkPool {
  id: string;
  node_id: string;
  name: string;
  description?: string;
  address_pool: string;
  project_prefix: number;
  priority: number;
  status: string;
  next_index: number;
  reuse_enabled: boolean;
  reuse_delay_seconds: number;
  is_system_default: boolean;
  last_conflict_reason?: string;
  capacity: number;
  allocated: number;
  reserved: number;
  released_waiting: number;
  available: number;
  usage: number;
  created_at: string;
  updated_at: string;
}

// 一个项目占用的网段及其 Docker 网络。
export interface ProjectNetwork {
  id: string;
  node_id: string;
  project_id: string;
  pool_id: string;
  subnet_index: number;
  subnet: string;
  gateway: string;
  docker_network_name: string;
  status: string;
  last_error?: string;
}

// 系统级容器网络默认：决定未来新节点自动创建的默认池（不改动已有池）。
export interface ContainerNetworkSettings {
  default_network_pool: string;
  default_project_prefix: number;
  allocation_mode: string;
  default_reuse_enabled: boolean;
  default_reuse_delay_seconds: number;
}
