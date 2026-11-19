# 前端国际化

前端已迁移为 React + Semi Design 单页应用，国际化由 `web/frontend/src/lib/i18n.jsx` 提供，
词条继续复用后端同目录 `web/static/i18n/*.json`（zh-CN / en-US / ru-RU / ar-SA）。

## 运行时行为

- 语言偏好保存在 `localStorage['csai_lang']`，未设置时按 `navigator.language` 推断，默认 `zh-CN`。
- 启动时并行加载当前语言、`zh-CN` 兜底与 `en-US`（第三兜底）三份词条；缺失的键按「当前语言 → zh-CN → en-US → 调用方兜底中文」回退。
- 切换语言会更新 `<html lang>`，阿拉伯语自动把 `<html dir>` 设为 `rtl`，并通过 Semi `ConfigProvider` 切换组件语言包。
- 语言切换不会刷新页面；`useT()` 返回的 `t` 会随 Provider 重新渲染。

## 在页面里使用

```jsx
const t = useT();
t('vulnerability.title');                       // 词条里存在键
t('common.minutesAgo', { n: 5 });               // 支持 {{n}} 插值
t('chat.unknownKey', null, '中文兜底');          // 键缺失时用第三参数
```

要求：

1. 可见文案统一走 `t(...)`，第三个参数写中文兜底，避免出现原始 key。
2. 新增词条时同时更新 `web/static/i18n/zh-CN.json` 与 `en-US.json`；`ru-RU`/`ar-SA` 可暂时缺失，靠回退链。
3. 表单校验、Toast、空态文案同样走 `t(...)`。
4. 时间格式化使用 `src/lib/format.js` 的 `formatDateTime/relativeTime`，不要手拼日期。

## 词条组织

`web/static/i18n/zh-CN.json` 按业务域分组：`common`、`nav`、`dashboard`、`vulnerability`、`chat`、
`settings`、`knowledge`、`skills`、`roles`、`auth`、`apiDocs` 等。新增分组时保持两级键名
（`<domain>.<key>`），避免深层嵌套影响可读性。
