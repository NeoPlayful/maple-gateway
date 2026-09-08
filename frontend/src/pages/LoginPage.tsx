import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../stores/auth';

export default function LoginPage() {
  const { t } = useTranslation('admin');
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
      navigate('/admin', { replace: true });
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : t('auth.loginFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-screen items-center justify-center bg-slate-100 dark:bg-slate-950">
      <form
        onSubmit={submit}
        className="w-80 rounded-xl border border-slate-200 bg-white p-8 shadow-md dark:border-slate-700 dark:bg-slate-800"
      >
        <h1 className="mb-6 text-center text-xl font-bold text-slate-800 dark:text-slate-100">
          {t('app.name')}
        </h1>
        <div className="mb-4">
          <label className="mb-1 block text-sm text-slate-600 dark:text-slate-300">{t('auth.email')}</label>
          <input
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 outline-none focus:border-emerald-500 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
            type="email"
            required
          />
        </div>
        <div className="mb-4">
          <label className="mb-1 block text-sm text-slate-600 dark:text-slate-300">{t('auth.password')}</label>
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded border border-slate-300 bg-white px-3 py-2 text-sm text-slate-800 outline-none focus:border-emerald-500 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
            type="password"
            required
          />
        </div>
        {err && <p className="mb-3 text-sm text-red-600">{err}</p>}
        <button
          disabled={busy}
          className="w-full rounded bg-emerald-600 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50 dark:bg-emerald-600 dark:hover:bg-emerald-700"
        >
          {busy ? t('auth.loggingIn') : t('auth.login')}
        </button>
      </form>
    </div>
  );
}
