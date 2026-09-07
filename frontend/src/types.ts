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
