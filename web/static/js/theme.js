(function () {
    'use strict';

    const STORAGE_KEY = 'cyberstrike-theme';
    const THEMES = ['system', 'light', 'dark'];
    const FALLBACK_LABELS = {
        system: 'Follow system',
        light: 'light color',
        dark: 'dark color'
    };
    const FALLBACK_TITLES = {
        system: 'Current: Follow system theme. Click to switch to light.',
        light: 'Current: light theme. Click to switch to dark.',
        dark: 'Current: Dark theme. Click to switch to Follow System.'
    };
    const TITLE_KEYS = {
        system: 'titleSystem',
        light: 'titleLight',
        dark: 'titleDark'
    };

    const media = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;

    function themeText(key, fallback) {
        if (typeof window.t === 'function') {
            const value = window.t('theme.' + key);
            if (value && value !== 'theme.' + key) {
                return value;
            }
        }
        return fallback;
    }

    function getLabel(preference) {
        return themeText(preference, FALLBACK_LABELS[preference] || FALLBACK_LABELS.system);
    }

    function getTitle(preference) {
        const titleKey = TITLE_KEYS[preference] || TITLE_KEYS.system;
        return themeText(titleKey, FALLBACK_TITLES[preference] || FALLBACK_TITLES.system);
    }

    function normalizePreference(value) {
        return THEMES.includes(value) ? value : 'system';
    }

    function readPreference() {
        try {
            return normalizePreference(localStorage.getItem(STORAGE_KEY));
        } catch (err) {
            return 'system';
        }
    }

    function resolveTheme(preference) {
        if (preference === 'dark' || preference === 'light') {
            return preference;
        }
        return media && media.matches ? 'dark' : 'light';
    }

    function updateButton(preference, resolved) {
        const btn = document.getElementById('theme-toggle-btn');
        const label = document.getElementById('theme-toggle-label');
        if (!btn) {
            return;
        }
        btn.dataset.themePreference = preference;
        btn.dataset.theme = resolved;
        const title = getTitle(preference);
        btn.title = title;
        btn.setAttribute('aria-label', title);
        if (label) {
            label.textContent = getLabel(preference);
        }
    }

    function applyTheme(preference) {
        const normalized = normalizePreference(preference);
        const resolved = resolveTheme(normalized);
        const root = document.documentElement;
        root.setAttribute('data-theme-preference', normalized);
        root.setAttribute('data-theme', resolved);
        root.style.colorScheme = resolved;
        updateButton(normalized, resolved);
        document.dispatchEvent(
            new CustomEvent('cyberstrike-themechange', {
                detail: { preference: normalized, resolved: resolved },
            }),
        );
    }

    function savePreference(preference) {
        const normalized = normalizePreference(preference);
        try {
            localStorage.setItem(STORAGE_KEY, normalized);
        } catch (err) {
            // Ignore storage failures; the current page can still apply the theme.
        }
        applyTheme(normalized);
    }

    window.setThemePreference = savePreference;
    window.getThemePreference = readPreference;
    window.cycleThemePreference = function () {
        const current = readPreference();
        const next = THEMES[(THEMES.indexOf(current) + 1) % THEMES.length];
        savePreference(next);
    };
    window.refreshThemeToggleLabel = function () {
        applyTheme(readPreference());
    };

    if (media) {
        const onSystemThemeChange = function () {
            if (readPreference() === 'system') {
                applyTheme('system');
            }
        };
        if (typeof media.addEventListener === 'function') {
            media.addEventListener('change', onSystemThemeChange);
        } else if (typeof media.addListener === 'function') {
            media.addListener(onSystemThemeChange);
        }
    }

    document.addEventListener('languagechange', function () {
        applyTheme(readPreference());
    });

    function initTheme() {
        applyTheme(readPreference());
        if (window.i18nReady && typeof window.i18nReady.then === 'function') {
            window.i18nReady.then(function () {
                applyTheme(readPreference());
            });
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initTheme);
    } else {
        initTheme();
    }
})();
