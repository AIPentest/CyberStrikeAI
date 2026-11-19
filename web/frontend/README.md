# 前端（React 19 + Semi Design）

CyberStrikeAI 的 Web 界面是一个 Vite 构建的 React 单页应用，UI 组件统一来自
[Semi Design](https://semi.design/zh-CN/)。页面不再使用 Go 模板、手写 HTML/CSS/JS 或全局 `window.*` 脚本。

## 目录

```
web/frontend/
├─ index.html            # Vite 入口，挂载 #root
├─ vite.config.mjs       # base=/static/app/，产物输出到 web/static/app
├─ CONTRACT.md           # 页面开发契约（公共模块 / 样式 / 自检）
└─ src/
   ├─ main.jsx           # 主题、i18n、Semi ConfigProvider、HashRouter
   ├─ App.jsx            # 路由与权限守卫
   ├─ lib/               # http、auth、i18n、theme、nav、format
   ├─ layout/            # 侧边导航、顶栏、登录页
   ├─ components/        # PageHeader / DataTable / StatGrid / Markdown 等
   ├─ pages/<页面 id>/   # 与路由 #/<页面 id> 一一对应
   └─ styles/app.css     # 外壳与共享组件样式
```

## 开发

```sh
npm ci --prefix web/frontend
npm run --prefix web/frontend dev      # 本地热更新，静态资源直接从工作区读取
# 联调时显式指定你实际启动的后端，避免误连占用默认端口的其他应用：
CSAI_API_ORIGIN=http://127.0.0.1:8080 npm run --prefix web/frontend dev
```

后端仍由 Go 提供：`go run ./cmd/server -config config.yaml`。未设置 `CSAI_API_ORIGIN` 时，开发服务器的 API 返回 503，不会连接默认端口上的其他服务。

浏览器回归：`npm run --prefix web/frontend test:ui`（需要本机 Chrome）。测试使用独立浏览器和匿名 fixture，拦截全部 API 与外联，不连接部署环境；报告和截图分别位于 `playwright-report/`、`test-results/`。该测试不替代真实后端联调。

## 构建与发布

```sh
npm run --prefix web/frontend build
```

产物写入 `web/static/app/`（哈希文件名）。Go 服务通过 `router.Static("/static", "./web/static")`
提供静态资源，`GET /` 直接返回 `web/static/app/index.html`；`/api-docs` 302 到 `/#/api-docs`。
构建产物需要随源码一起提交，`internal/app/spa_test.go` 会校验首页引用的资源全部存在。

## 约定

- 路由使用 hash：`#/dashboard`、`#/chat?conversation=<id>`、`#/vulnerabilities?id=<id>`，深链接可直接分享。
- 会话令牌沿用 `localStorage['cyberstrike-auth']`，请求统一走 `src/lib/http.js`（自动附加 `Authorization`）。
- 主题偏好沿用 `localStorage['cyberstrike-theme']`（system/light/dark），语言沿用 `localStorage['csai_lang']`，
  词条复用 `web/static/i18n/*.json`。
- 页面只能通过 `api` / `postStream` 访问 `/api`，不得直接 `fetch`；权限判断用 `useAuth().hasPermission(...)`。
- 新增页面：在 `src/pages/<id>/index.jsx` 实现默认导出组件，并在 `src/pages/registry.js` 与 `src/lib/nav.js` 注册。

详细页面契约见 [CONTRACT.md](./CONTRACT.md)。
