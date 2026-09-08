import { create } from 'zustand';
import { api, getToken, setToken, clearToken } from '../lib/client';

export interface Admin {
  id: string;
  email: string;
  name: string;
}

interface AuthState {
  admin: Admin | null;
  initialized: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  restore: () => Promise<void>;
}

export const useAuth = create<AuthState>((set) => ({
  admin: null,
  initialized: false,
  login: async (email, password) => {
    const res = await api.post<{ token: string; admin: Admin }>('/api/auth/login', {
      email,
      password,
    });
    setToken(res.token);
    set({ admin: res.admin });
  },
  logout: () => {
    clearToken();
    set({ admin: null });
  },
  restore: async () => {
    if (!getToken()) {
      set({ initialized: true });
      return;
    }
    try {
      const me = await api.get<Admin>('/api/admin/auth/me');
      set({ admin: me, initialized: true });
    } catch {
      set({ admin: null, initialized: true });
    }
  },
}));
