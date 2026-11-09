// Frontend internationalization initialization (based on i18next browser version)
(function () {
    const DEFAULT_LANG = 'zh-CN';
    const STORAGE_KEY = 'csai_lang';
    const RESOURCES_PREFIX = '/static/i18n';

    const loadedLangs = {};

    // For bootstrap and similar logic to wait on; prevents chat from rendering hardcoded Chinese when t() is not ready, causing inconsistency with the language label
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
            console.warn('Unable to read language settings:', e);
        }

        const navLang = (navigator.language || (navigator.languages && navigator.languages[0]) || '').toLowerCase();
        if (navLang.startsWith('zh')) {
            return 'zh-CN';
        }
        if (navLang.startsWith('ru')) {
            return 'ru-RU';
        }
        if (navLang.startsWith('ar')) {
            return 'ar-SA';
        }
        if (navLang.startsWith('en')) {
            return 'en-US';
        }
        return DEFAULT_LANG;
    }

    // 根据语言设置文档方向（阿拉伯语为从右到左 RTL）
    function applyDirection(lang) {
        try {
            if (document && document.documentElement) {
                var l = String(lang || DEFAULT_LANG).toLowerCase();
                document.documentElement.dir = l.indexOf('ar') === 0 ? 'rtl' : 'ltr';
            }
        } catch (e) {
            // ignore
        }
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
                console.warn('Failed to load language pack:', lang, resp.status);
                return;
            }
            const data = await resp.json();
            if (typeof i18next !== 'undefined') {
                i18next.addResourceBundle(lang, 'translation', data, true, true);
            }
            loadedLangs[lang] = true;
        } catch (e) {
            console.error('Exception loading language pack:', lang, e);
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
            // When a key is missing, keep the fallback copy from the template, preventing the page from directly displaying internal key names like assets.project.
            const text = translated && translated !== key ? translated : '';
            // Only replace the entire text when the element has no child elements (text only or empty); otherwise replace only the first pure text node
            // (For example, for the label "Project Name <span class="required">*</span>", only replace the "Project Name" portion, keeping the <span> intact).
            // input/textarea never sets textContent; numbers/child nodes inside the card, as before, are not fully overwritten.
            const hasNoElementChildren = !el.querySelector('*');
            if (!skipText && !isFormControl && text && typeof text === 'string') {
                if (hasNoElementChildren) {
                    el.textContent = text;
                } else {
                    // Only replace the first plain text node (e.g., for the label "Project Name <span class="required">*</span>", only replace "Project Name", keeping the <span>)
                    const firstNode = el.firstChild;
                    if (firstNode && firstNode.nodeType === 3 && firstNode.textContent.trim()) {
                        firstNode.textContent = text + ' ';
                    }
                }
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

        // Conversation input box: if value is the same as placeholder, clear value so the placeholder hint displays correctly
        try {
            const chatInput = document.getElementById('chat-input');
            if (chatInput && chatInput.tagName === 'TEXTAREA') {
                const ph = (chatInput.getAttribute('placeholder') || '').trim();
                if (ph && chatInput.value.trim() === ph) {
                    chatInput.value = '';
                }
            }
        } catch (e) { /* ignore */ }

        // Update html lang attribute
        try {
            if (document && document.documentElement) {
                document.documentElement.lang = i18next.language || DEFAULT_LANG;
            }
        } catch (e) {
            // ignore
        }

        // 更新文档方向（RTL/LTR）
        applyDirection(i18next.language || DEFAULT_LANG);

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
        } else if (lang.indexOf('ru') === 0) {
            label.textContent = i18next.t('lang.ruRU');
        } else if (lang.indexOf('ar') === 0) {
            label.textContent = i18next.t('lang.arSA');
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
        if (lang === 'ru-RU' || lang === 'ar-SA') {
            await loadLanguageResources('en-US');
        }
        await i18next.changeLanguage(lang);
        try {
            localStorage.setItem(STORAGE_KEY, lang);
        } catch (e) {
            console.warn('Unable to save language settings:', e);
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
            console.warn('i18next not loaded, skipping frontend internationalization initialization');
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
            return;
        }

        const initialLang = detectInitialLang();
        await i18next.init({
            lng: initialLang,
            fallbackLng: {
                'ru-RU': ['en-US', 'zh-CN'],
                'ar-SA': ['en-US', 'zh-CN'],
                'default': [DEFAULT_LANG]
            },
            debug: false,
            resources: {}
        });

        await loadLanguageResources(initialLang);
        if (initialLang === 'ru-RU' || initialLang === 'ar-SA') {
            await loadLanguageResources('en-US');
        }
        applyTranslations(document);
        updateLangLabel();
        if (typeof window.refreshThemeToggleLabel === 'function') {
            window.refreshThemeToggleLabel();
        }
        try {
            window.__locale = i18next.language || initialLang;
        } catch (e) { /* ignore */ }

        // Export global function for other scripts to call (supports interpolation parameters, e.g. _t('key', { count: 2 }))
        window.t = function (key, opts) {
            if (typeof i18next === 'undefined') return key;
            return i18next.t(key, opts);
        };
        window.uiLocale = function () {
            const lang = String((window.__locale || (typeof i18next !== 'undefined' && i18next.language) || '')).toLowerCase();
            if (lang.indexOf('zh') === 0) return 'zh-CN';
            if (lang.indexOf('ru') === 0) return 'ru-RU';
            if (lang.indexOf('ar') === 0) return 'ar-SA';
            return 'en-US';
        };
        window.changeLanguage = changeLanguage;
        window.applyTranslations = applyTranslations;

        // Language switch dropdown support
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

        // If chat already rendered the system ready message with fallback Chinese before i18n finished, correct it once here based on the current language
        try {
            if (typeof refreshSystemReadyMessageBubbles === 'function') {
                refreshSystemReadyMessageBubbles();
            }
        } catch (e) { /* ignore */ }

        if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
    }

    document.addEventListener('DOMContentLoaded', function () {
        // i18n initialization executes after DOM Ready
        initI18n().catch(function (e) {
            console.error('Failed to initialize internationalization:', e);
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
        });
    });
})();
