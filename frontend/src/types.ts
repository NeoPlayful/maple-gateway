// 与后端 JSON 对应的核心类型。
export interface Service {
  id: string;
  name: string;
  protocol: string;
  tenant_id?: string;
}

export interface Deployment {
  id: string;
  service_id: string;
  name: string;
  status: string;
  strategy: string;
}

export interface Version {
  id: string;
  deployment_id: string;
  version: string;
  image?: string;
  weight: number;
  status: string;
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
