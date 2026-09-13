// 与后端 JSON 对应的核心类型。

export interface Service {
  id: string;
  tenant_id: string;
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

export interface Version {
  id: string;
  deployment_id: string;
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
  created_at: string;
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

export interface CanaryRelease {
  id: string;
  service_id: string;
  name: string;
  stable_version_id: string;
  canary_version_id: string;
  phase: string;
  canary_weight: number;
  target_weight: number;
  step_weight: number;
  started_at?: string | null;
  finished_at?: string | null;
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
}

export interface ErrorLogRow {
  timestamp: string;
  host: string;
  path: string;
  status: number;
  error: string;
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

export interface BlueGreenDeployment {
  id: string;
  deployment_id: string;
  blue_version_id: string;
  green_version_id: string;
  active_version_id?: string | null;
  previous_active_id?: string | null;
  deployment_name?: string;
  blue_version?: string;
  green_version?: string;
  active_version?: string;
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
  exit_code: number;
  oom_killed: boolean;
  finished_at?: string;
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

// 一个 Compose 应用（CM 应用模型经 /api/admin/cm/applications 透出）。
export interface CMApplication {
  id: string;
  tenant_id?: string;
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
