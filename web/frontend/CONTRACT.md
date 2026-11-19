# 前端契约（React 19 + Semi Design）

所有页面都在 `web/frontend/src/pages/<页面 id>/` 下实现，入口固定为 `index.jsx`，默认导出页面组件。
页面 id 与路由 `#/<页面 id>` 一一对应，清单见 `src/pages/registry.js` 与 `src/lib/nav.js`。

## 唯一允许的公共依赖

```js
import { Button, Card, Table, Form, Modal, Drawer, Select, Input, Tabs, Tag, Space, Typography,
         Banner, Descriptions, Popconfirm, Toast, Tooltip, Switch, Checkbox, Radio, RadioGroup,
         DatePicker, Progress, Spin, Empty, List, Timeline, Collapse, Pagination, TextArea,
         Avatar, Badge, Divider, SideSheet, Tree, TreeSelect, Upload, Slider, Steps, SplitButton,
         ButtonGroup, Skeleton, Notification } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch, IconDelete, IconEdit } from '@douyinfe/semi-icons';
```

路由：`react-router-dom` v7（`useNavigate`、`useSearchParams`、`useParams`、`Link`）。
图形：`cytoscape`（仅 deep-assessments / projects 需要）。终端：`@xterm/xterm` + `@xterm/addon-fit`。
Markdown：统一用 `MarkdownView`，不要自己引 marked/dompurify。

不要新增 npm 依赖，不要直接 `fetch`，不要写 `window.xxx` 全局，不要操作 DOM 查询（`document.getElementById`）。

## 公共模块

```js
import { api, downloadFile, postStream, errorMessage } from '../../lib/http';
import { useAuth } from '../../lib/auth';          // { user, roles, hasPermission('x:read'), hasAnyPermission([...]), refresh }
import { useT } from '../../lib/i18n';             // t('key', {n:1}, '中文默认值')
import { useResource, useInterval, useDebounced, useLocalStorage } from '../../components/hooks';
import {
  PageHeader, SectionCard, StatGrid, StatCard, DataTable, ErrorBanner, SeverityTag, StatusTag,
  CopyButton, MarkdownView, JsonViewer, KeyValueList, toastSuccess, toastWarning, toastError,
  confirmAction
} from '../../components/ui';
```

`api` 用法：`api.get('/vulnerabilities', { page: 1, page_size: 20 })`、`api.post('/x', body)`、
`api.put`、`api.del`。路径不带 `/api` 前缀（自动补）。返回值已解析 JSON，非 2xx 抛 `ApiError`。

`useResource(loader, deps)` 返回 `{ data, loading, error, reload, setData }`，列表页统一用它。

`t(key, vars, fallback)` 的 key 取自仓库既有 `web/static/i18n/zh-CN.json`；找不到时用第三个参数的中文兜底。
文案必须走 `t(...)` 或直接写中文，不要硬编码英文。

## 样式

页面私有样式写同目录 `page.css` 并在 `index.jsx` 里 `import './page.css';`。
仅允许少量布局样式（flex/grid/间距），颜色一律使用 Semi 变量：
`var(--semi-color-bg-1)`、`var(--semi-color-border)`、`var(--semi-color-text-2)`、`var(--semi-color-primary)` 等。
不要复制旧版 css 文件，不要自定义整套配色。

## 数据契约来源

后端接口在 `internal/app/app.go`（`setupRoutes`）注册，响应结构看 `internal/handler/*.go`。
旧前端调用方式在 `web/static/js/<模块>.js`，字段名与请求体以旧代码 + handler 为准，不要臆造字段。
页面容器在 `web/templates/index.html`（`id="page-<id>"`）可查既有交互与字段提示。

## 页面完成标准

1. 首屏加载真实数据（GET），有 loading / 空态 / 错误态（`ErrorBanner` + `onRetry`）。
2. 主操作可用：列表 + 详情（Drawer/Modal）+ 新建/编辑表单 + 删除确认 + 刷新；写操作按钮用 `hasPermission('x:write')` 控制禁用或隐藏。
3. 深链接：按 id 定位的页面从 `useSearchParams()` 读取（如 `#/vulnerabilities?id=xxx`），操作后写回 URL。
4. 不改 `src/pages/registry.js`、`src/lib/*`、`src/components/*`、`src/layout/*`、`src/styles/app.css`。
5. 页面目录内可以任意拆分文件，单文件建议 < 500 行。

## 自检（不要跑整站构建，避免与其他页面互相干扰）

```sh
cd web/frontend
npx esbuild src/pages/<page>/index.jsx --bundle --jsx=automatic --format=esm \
  --external:react --external:react-dom --external:react-router-dom \
  --external:@douyinfe/* --external:cytoscape --external:marked --external:dompurify \
  --outdir=/tmp/csai-check-<page>
```

页面 import 了 `page.css` 时必须用 `--outdir`（`--outfile=/dev/null` 会连带写 `/dev/null.css` 而失败）。
该命令只验证语法与本地相对导入，`--external:@douyinfe/*` 会跳过 Semi 导出名解析——
最终以 `npm run build` 为准，命名导入必须是 Semi 真实导出（`Drawer` 请从 `components/ui` 导入，Semi v2 用 `SideSheet`）。

完成后在最终回复里列出：页面 id、使用的接口、验证过的交互、遗留问题。
