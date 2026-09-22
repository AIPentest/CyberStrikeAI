// 前端国际化初始化（基于 i18next 浏览器版本）
(function () {
    const DEFAULT_LANG = 'zh-CN';
    const STORAGE_KEY = 'csai_lang';
    const RESOURCES_PREFIX = '/static/i18n';

    const loadedLangs = {};

    // 供 bootstrap 等逻辑等待：避免 chat 在 t() 未就绪时用中文硬编码渲染，导致与语言标签不一致
    let i18nReadyResolve;
    window.i18nReady = new Promise(function (resolve) {
        i18nReadyResolve = resolve;
    });

    function detectInitialLang() {
        try {
            const stored = localStorage.getItem(STORAGE_KEY);
            if (stored) {
                return stored;
            }
        } catch (e) {
            console.warn('无法读取语言设置:', e);
        }

        const navLang = (navigator.language || navigator.userLanguage || '').toLowerCase();
        if (navLang.startsWith('zh')) {
            return 'zh-CN';
        }
        if (navLang.startsWith('en')) {
            return 'en-US';
        }
        return DEFAULT_LANG;
    }

    async function loadLanguageResources(lang) {
        if (loadedLangs[lang]) {
            return;
        }
        try {
            const resp = await fetch(RESOURCES_PREFIX + '/' + lang + '.json', {
                cache: 'no-cache'
            });
            if (!resp.ok) {
                console.warn('加载语言包失败:', lang, resp.status);
                return;
            }
            const data = await resp.json();
            if (typeof i18next !== 'undefined') {
                i18next.addResourceBundle(lang, 'translation', data, true, true);
            }
            loadedLangs[lang] = true;
        } catch (e) {
            console.error('加载语言包异常:', lang, e);
        }
    }

    function applyTranslations(root) {
        if (typeof i18next === 'undefined') return;
        const container = root || document;
        if (!container) return;

        const elements = container.querySelectorAll('[data-i18n]');
        elements.forEach(function (el) {
            const key = el.getAttribute('data-i18n');
            if (!key) return;
            const skipText = el.getAttribute('data-i18n-skip-text') === 'true';
            const isFormControl = (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA');
            const attrList = el.getAttribute('data-i18n-attr');
            const translated = i18next.t(key);
            // 缺键时保留模板中的后备文案，避免页面直接显示 assets.project 一类内部键名。
            const text = translated && translated !== key ? translated : '';
            // 仅当元素无子元素（仅文本或空）时才替换文本，避免覆盖卡片内的数字、子节点等；input/textarea 永不设置 textContent
            const hasNoElementChildren = !el.querySelector('*');
            if (!skipText && !isFormControl && hasNoElementChildren && text && typeof text === 'string') {
                el.textContent = text;
            }

            if (attrList) {
                const titleKey = el.getAttribute('data-i18n-title');
                attrList.split(',').map(function (s) { return s.trim(); }).forEach(function (attr) {
                    if (!attr) return;
                    var val = text;
                    if (attr === 'title' && titleKey) {
                        var titleText = i18next.t(titleKey);
                        if (titleText && titleText !== titleKey && typeof titleText === 'string') val = titleText;
                    }
                    if (val && typeof val === 'string') {
                        el.setAttribute(attr, val);
                    }
                });
            }
        });

        // 对话输入框：若 value 与 placeholder 相同，清空 value 以便正确显示占位提示
        try {
            const chatInput = document.getElementById('chat-input');
            if (chatInput && chatInput.tagName === 'TEXTAREA') {
                const ph = (chatInput.getAttribute('placeholder') || '').trim();
                if (ph && chatInput.value.trim() === ph) {
                    chatInput.value = '';
                }
            }
        } catch (e) { /* ignore */ }

        // 更新 html lang 属性
        try {
            if (document && document.documentElement) {
                document.documentElement.lang = i18next.language || DEFAULT_LANG;
            }
        } catch (e) {
            // ignore
        }

        try {
            if (typeof window.syncHitlAuditBackendUI === 'function') {
                window.syncHitlAuditBackendUI();
            }
        } catch (e) { /* ignore */ }
    }

    function updateLangLabel() {
        const label = document.getElementById('current-lang-label');
        if (!label || typeof i18next === 'undefined') return;
        const lang = (i18next.language || DEFAULT_LANG).toLowerCase();
        if (lang.indexOf('zh') === 0) {
            label.textContent = i18next.t('lang.zhCN');
        } else {
            label.textContent = i18next.t('lang.enUS');
        }
    }

    function closeLangDropdown() {
        const dropdown = document.getElementById('lang-dropdown');
        if (dropdown) {
            dropdown.style.display = 'none';
        }
    }

    function handleGlobalClickForLangDropdown(ev) {
        const dropdown = document.getElementById('lang-dropdown');
        const btn = document.querySelector('.lang-switcher-btn');
        if (!dropdown || dropdown.style.display !== 'block') return;
        const target = ev.target;
        if (btn && btn.contains(target)) {
            return;
        }
        if (!dropdown.contains(target)) {
            closeLangDropdown();
        }
    }

    async function changeLanguage(lang) {
        if (typeof i18next === 'undefined') return;
        const current = i18next.language || DEFAULT_LANG;
        if (lang === current) return;
        await loadLanguageResources(lang);
        await i18next.changeLanguage(lang);
        try {
            localStorage.setItem(STORAGE_KEY, lang);
        } catch (e) {
            console.warn('无法保存语言设置:', e);
        }
        applyTranslations(document);
        updateLangLabel();
        if (typeof window.refreshThemeToggleLabel === 'function') {
            window.refreshThemeToggleLabel();
        }
        try {
            window.__locale = lang;
        } catch (e) { /* ignore */ }
        try {
            document.dispatchEvent(new CustomEvent('languagechange', { detail: { lang: lang } }));
        } catch (e) { /* ignore */ }
    }

    async function initI18n() {
        if (typeof i18next === 'undefined') {
            console.warn('i18next 未加载，跳过前端国际化初始化');
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
            return;
        }

        const initialLang = detectInitialLang();
        await i18next.init({
            lng: initialLang,
            fallbackLng: DEFAULT_LANG,
            debug: false,
            resources: {}
        });

        await loadLanguageResources(initialLang);
        applyTranslations(document);
        updateLangLabel();
        if (typeof window.refreshThemeToggleLabel === 'function') {
            window.refreshThemeToggleLabel();
        }
        try {
            window.__locale = i18next.language || initialLang;
        } catch (e) { /* ignore */ }

        // 导出全局函数供其他脚本调用（支持插值参数，如 _t('key', { count: 2 })）
        window.t = function (key, opts) {
            if (typeof i18next === 'undefined') return key;
            return i18next.t(key, opts);
        };
        window.changeLanguage = changeLanguage;
        window.applyTranslations = applyTranslations;

        // 语言切换下拉支持
        window.toggleLangDropdown = function () {
            const dropdown = document.getElementById('lang-dropdown');
            if (!dropdown) return;
            if (dropdown.style.display === 'block') {
                dropdown.style.display = 'none';
            } else {
                dropdown.style.display = 'block';
            }
        };
        window.onLanguageSelect = function (lang) {
            changeLanguage(lang);
            closeLangDropdown();
        };

        document.addEventListener('click', handleGlobalClickForLangDropdown);

        // 若 chat 已在 i18n 完成前用后备中文渲染了系统就绪消息，这里按当前语言纠正一次
        try {
            if (typeof refreshSystemReadyMessageBubbles === 'function') {
                refreshSystemReadyMessageBubbles();
            }
        } catch (e) { /* ignore */ }

        if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
    }

    document.addEventListener('DOMContentLoaded', function () {
        // i18n 初始化在 DOM Ready 后执行
        initI18n().catch(function (e) {
            console.error('初始化国际化失败:', e);
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
        });
    });
})();
