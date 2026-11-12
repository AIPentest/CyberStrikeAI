// Frontend internationalization init (based on the i18next browser build)
(function () {
    const DEFAULT_LANG = 'en-US';
    const STORAGE_KEY = 'csai_lang';
    const RESOURCES_PREFIX = '/static/i18n';

    const loadedLangs = {};

    // Lets logic such as bootstrap wait, so chat does not render hardcoded text before t() is ready and end up inconsistent with the language label
    let i18nReadyResolve;
    window.i18nReady = new Promise(function (resolve) {
        i18nReadyResolve = resolve;
    });

    const SUPPORTED_LANGS = ['en-US', 'ru-RU', 'ar-SA'];

    function detectInitialLang() {
        try {
            const stored = localStorage.getItem(STORAGE_KEY);
            // Only honor a stored preference that still ships. A retired locale
            // (e.g. a stale zh-CN) falls through to the default so the app never
            // boots pointing at a language pack that no longer exists.
            if (stored && SUPPORTED_LANGS.indexOf(stored) !== -1) {
                return stored;
            }
        } catch (e) {
            console.warn('Unable to read the language setting:', e);
        }

        const navLang = (navigator.language || navigator.userLanguage || '').toLowerCase();
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

    // Set the document direction based on the language (Arabic is right-to-left, RTL)
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
            console.error('Error loading language pack:', lang, e);
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
            // On a missing key, keep the template's fallback text, to avoid the page directly showing internal key names like assets.project.
            const text = translated && translated !== key ? translated : '';
            // Only replace text when the element has no child elements (text-only or empty), to avoid overwriting numbers, child nodes, etc. inside a card; input/textarea never has textContent set
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

        // Conversation input: if value equals placeholder, clear value so the placeholder hint displays correctly
        try {
            const chatInput = document.getElementById('chat-input');
            if (chatInput && chatInput.tagName === 'TEXTAREA') {
                const ph = (chatInput.getAttribute('placeholder') || '').trim();
                if (ph && chatInput.value.trim() === ph) {
                    chatInput.value = '';
                }
            }
        } catch (e) { /* ignore */ }

        // Update the html lang attribute
        try {
            if (document && document.documentElement) {
                document.documentElement.lang = i18next.language || DEFAULT_LANG;
            }
        } catch (e) {
            // ignore
        }

        // Update the document direction (RTL/LTR)
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
        if (lang.indexOf('ru') === 0) {
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
            console.warn('Unable to save the language setting:', e);
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
            console.warn('i18next is not loaded; skipping frontend i18n init');
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
            return;
        }

        const initialLang = detectInitialLang();
        await i18next.init({
            lng: initialLang,
            fallbackLng: {
                'ru-RU': ['en-US'],
                'ar-SA': ['en-US'],
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

        // Export a global function for other scripts to call (supports interpolation params, e.g. _t('key', { count: 2 }))
        window.t = function (key, opts) {
            if (typeof i18next === 'undefined') return key;
            return i18next.t(key, opts);
        };
        window.uiLocale = function () {
            const lang = String((window.__locale || (typeof i18next !== 'undefined' && i18next.language) || '')).toLowerCase();
            if (lang.indexOf('ru') === 0) return 'ru-RU';
            if (lang.indexOf('ar') === 0) return 'ar-SA';
            return 'en-US';
        };
        window.changeLanguage = changeLanguage;
        window.applyTranslations = applyTranslations;

        // Language-switcher dropdown support
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

        // If chat rendered the system-ready message with fallback text before i18n finished, correct it once for the current language here
        try {
            if (typeof refreshSystemReadyMessageBubbles === 'function') {
                refreshSystemReadyMessageBubbles();
            }
        } catch (e) { /* ignore */ }

        if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
    }

    document.addEventListener('DOMContentLoaded', function () {
        // i18n init runs after DOM Ready
        initI18n().catch(function (e) {
            console.error('Failed to initialize i18n:', e);
            if (typeof i18nReadyResolve === 'function') i18nReadyResolve();
        });
    });
})();
