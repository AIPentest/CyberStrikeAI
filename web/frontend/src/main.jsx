import '@douyinfe/semi-ui/react19-adapter';

import React from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { ConfigProvider } from '@douyinfe/semi-ui';
import zhCN from '@douyinfe/semi-ui/lib/es/locale/source/zh_CN';
import enUS from '@douyinfe/semi-ui/lib/es/locale/source/en_US';
import ruRU from '@douyinfe/semi-ui/lib/es/locale/source/ru_RU';
import arSA from '@douyinfe/semi-ui/lib/es/locale/source/ar';
import App from './App';
import { AuthProvider } from './lib/auth';
import { I18nProvider, useI18n } from './lib/i18n';
import { ThemeProvider } from './lib/theme';
import './styles/app.css';

const SEMI_LOCALES = { 'zh-CN': zhCN, 'en-US': enUS, 'ru-RU': ruRU, 'ar-SA': arSA };

function Shell() {
    const { lang } = useI18n();
    return (
        <ConfigProvider locale={SEMI_LOCALES[lang] || zhCN} direction={lang === 'ar-SA' ? 'rtl' : 'ltr'}>
            <AuthProvider>
                <HashRouter>
                    <App />
                </HashRouter>
            </AuthProvider>
        </ConfigProvider>
    );
}

createRoot(document.getElementById('root')).render(
    <React.StrictMode>
        <ThemeProvider>
            <I18nProvider>
                <Shell />
            </I18nProvider>
        </ThemeProvider>
    </React.StrictMode>
);
