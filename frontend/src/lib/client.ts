// API 客户端：统一 {code, message, data, meta} 响应解包、Bearer 注入、401 处理。
import i18n from '../i18n';
const TOKEN_KEY = 'maple_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}
export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export interface ApiMeta {
  total?: number;
  limit?: number;
  offset?: number;
  count?: number;
}

export interface ApiResp<T> {
  code: string;
  message: string;
  data: T;
  meta?: ApiMeta;
}

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  const resp = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (resp.status === 401) {
    clearToken();
    if (!location.pathname.startsWith('/login')) location.href = '/login';
    throw new ApiError('UNAUTHORIZED', i18n.t('auth.sessionExpired', { ns: 'admin' }), 401);
  }
  let json: ApiResp<T>;
  try {
    json = await resp.json();
  } catch {
    throw new ApiError('PARSE_ERROR', i18n.t('auth.parseError', { ns: 'admin', status: resp.status }), resp.status);
  }
  if (json.code !== 'OK') {
    throw new ApiError(json.code || 'ERROR', json.message || i18n.t('auth.requestFailed', { ns: 'admin' }), resp.status);
  }
  return json.data;
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
};

export async function withMeta<T>(p: string): Promise<{ items: T[]; meta: ApiMeta }> {
  const resp = await fetch(p, {
    headers: getToken() ? { Authorization: `Bearer ${getToken()!}` } : {},
  });
  const json = await resp.json();
  return { items: (json.data ?? []) as T[], meta: json.meta ?? {} };
}
