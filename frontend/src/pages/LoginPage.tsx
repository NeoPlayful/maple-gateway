import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../stores/auth';

export default function LoginPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('admin@maple.com');
  const [password, setPassword] = useState('admin123');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr('');
    setBusy(true);
    try {
      await login(email, password);
      navigate('/', { replace: true });
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : '登录失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-screen items-center justify-center bg-slate-100">
      <form
        onSubmit={submit}
        className="w-80 rounded-xl bg-white p-8 shadow-md"
      >
        <h1 className="mb-6 text-center text-xl font-bold text-slate-800">
          🍁 Maple Gateway
        </h1>
        <div className="mb-4">
          <label className="mb-1 block text-sm text-slate-600">邮箱</label>
          <input
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded border px-3 py-2 text-sm outline-none focus:border-emerald-500"
            type="email"
            required
          />
        </div>
        <div className="mb-4">
          <label className="mb-1 block text-sm text-slate-600">密码</label>
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded border px-3 py-2 text-sm outline-none focus:border-emerald-500"
            type="password"
            required
          />
        </div>
        {err && <p className="mb-3 text-sm text-red-600">{err}</p>}
        <button
          disabled={busy}
          className="w-full rounded bg-emerald-600 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
        >
          {busy ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  );
}
