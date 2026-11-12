# Frontend i18n

[Chinese](../zh-CN/frontend-i18n.md)

CyberStrikeAI frontend i18n is static and lightweight. Text is organized in JSON files and applied through `data-i18n` attributes plus JavaScript helper functions.

## Files

```text
web/static/i18n/zh-CN.json
web/static/i18n/en-US.json
web/static/js/i18n.js
```

## Key Principles

- Keep keys stable and semantic.
- Update Chinese and English together.
- Do not hardcode new visible text in JS when it should be localized.
- Preserve default HTML text as fallback before JS initialization.

## HTML Usage

```html
<button data-i18n="common.save">Save</button>
```

For attributes, follow the existing `i18n.js` conventions.

## JavaScript Usage

Use the global translation helper where available:

```javascript
const label = t('common.save');
```

When adding dynamic UI, make sure language switching refreshes the text or re-renders the component.

## Migration Workflow

1. Add or update UI text.
2. Add keys to `zh-CN.json`.
3. Add matching keys to `en-US.json`.
4. Replace hardcoded text with `data-i18n` or `t()`.
5. Test both languages and browser console.

## Common Pitfalls

- Missing keys only in one language.
- Dynamic text built from hardcoded fragments.
- Button labels too long in English.
- HTML fallback text diverges from JSON text.
- Adding new page text without updating language switch behavior.
