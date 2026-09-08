// 按模块的请求封装（list / status 动作），返回解包后的 data。
import { api } from './client';

// list 请求列表资源：后端列表返回 {code, data:[...], meta:{total,...}}。
export async function list<T>(path: string): Promise<T[]> {
  return api.get<T[]>(`${path}?limit=200`);
}

export async function create<T>(path: string, body: unknown): Promise<T> {
  return api.post<T>(path, body);
}

export async function update<T>(path: string, id: string, body: unknown): Promise<T> {
  return api.patch<T>(`${path}/${id}`, body);
}

export async function remove(path: string, id: string): Promise<unknown> {
  return api.delete<unknown>(`${path}/${id}`);
}

// statusAction 执行 enable/disable 等状态动作。
export async function statusAction<T>(path: string, id: string, act: string): Promise<T> {
  return api.post<T>(`${path}/${id}/${act}`);
}
