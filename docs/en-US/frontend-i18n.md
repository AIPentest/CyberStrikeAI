# Frontend i18n

[中文](../zh-CN/frontend-i18n.md)

The web UI is a React + Semi Design single-page app. Translations come from
`web/frontend/src/lib/i18n.jsx`, backed by the same JSON files the backend ships.

## Files

```text
web/static/i18n/zh-CN.json
web/static/i18n/en-US.json
web/static/i18n/ru-RU.json
web/static/i18n/ar-SA.json
web/frontend/src/lib/i18n.jsx
```

## Runtime behavior

- Preference is stored in `localStorage['csai_lang']`; otherwise it follows `navigator.language`, defaulting to `zh-CN`.
- The provider loads the active language plus `zh-CN` and `en-US` fallbacks; lookups fall back `active → zh-CN → en-US → caller default`.
- Switching language updates `<html lang>`, sets `dir="rtl"` for Arabic, and swaps Semi's `ConfigProvider` locale.
- No page reload is required; `useT()` re-renders consumers.

## Usage

```jsx
const t = useT();
t('vulnerability.title');
t('common.minutesAgo', { n: 5 });          // {{n}} interpolation
t('chat.unknownKey', null, '中文兜底');     // explicit Chinese fallback
```

Rules:

1. Every visible string goes through `t(...)` with a Chinese fallback as the third argument.
2. Add new keys to `zh-CN.json` and `en-US.json` together; `ru-RU`/`ar-SA` may lag and rely on the fallback chain.
3. Format dates with `formatDateTime`/`relativeTime` from `src/lib/format.js`.
4. Keep keys two levels deep (`<domain>.<key>`) and stable across releases.
