import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'react-hot-toast';
import App from './App';
import './i18n';
import './stores/theme';
import './index.css';
import './themes/default/theme.css'; // default 常驻；其余主题 css 由 theme store 切到时按需注入

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
    {/* 全局浮层提示：右上角，跨主题/暗色沿用中性面板色。 */}
    <Toaster
      position="top-right"
      toastOptions={{
        className:
          'rounded-lg border border-slate-200 bg-white text-sm text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200',
      }}
    />
  </StrictMode>,
);
