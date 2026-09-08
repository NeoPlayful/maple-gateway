import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './i18n';
import './stores/theme';
import './index.css';
import './themes/default/theme.css'; // default 常驻；其余主题 css 由 theme store 切到时按需注入

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
