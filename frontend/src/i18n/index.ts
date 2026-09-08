import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import zhAdmin from './locales/zh-CN/admin.json';
import enAdmin from './locales/en-US/admin.json';

const LS_LANG_KEY = 'maple-lang';

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      'zh-CN': { admin: zhAdmin },
      'en-US': { admin: enAdmin },
    },
    fallbackLng: 'zh-CN',
    defaultNS: 'admin',
    ns: ['admin'],
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage'],
      caches: [],
      lookupLocalStorage: LS_LANG_KEY,
    },
  });

/**
 * 语言切换唯一入口：用户自己的选择持久化到 localStorage，
 * 后续访问直接按记忆的语言渲染。
 */
export function changeSiteLanguage(lang: string): void {
  if (lang !== 'zh-CN' && lang !== 'en-US') return;
  i18n.changeLanguage(lang);
  try {
    localStorage.setItem(LS_LANG_KEY, lang);
  } catch {
    /* ignore */
  }
}

export default i18n;
