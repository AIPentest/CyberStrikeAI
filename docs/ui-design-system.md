# 前端设计系统

主应用前端已统一迁移到 [Semi Design](https://semi.design/zh-CN/)（React 19 + Vite），
不再维护手写 HTML/CSS/JS 的组件体系。

## 结构

- 页面：`web/frontend/src/pages/<页面 id>/index.jsx`，路由 `#/<页面 id>`。
- 共享组件：`web/frontend/src/components/ui.jsx`（PageHeader、SectionCard、DataTable、StatGrid、ErrorBanner、MarkdownView、JsonViewer、Drawer 等）。
- 外壳样式：`web/frontend/src/styles/app.css`，只放布局与共享排版；颜色一律用 Semi 变量。
- 构建产物：`web/static/app/`，由 Go 直接提供。

## 颜色与主题

品牌主色 `#3266e3`（暗色 `#81a7ff`），通过覆盖 `--semi-color-primary` 等变量接入 Semi 主题，
暗色由 `body[theme-mode="dark"]` 驱动，偏好保存在 `localStorage['cyberstrike-theme']`（system/light/dark）。

## 组件约定

- 列表页用 `DataTable` + `useResource`，统一 loading / 空态 / 错误态（`ErrorBanner`）。
- 指标卡用 `StatGrid`/`StatCard`；详情抽屉用 `Drawer`（封装 Semi `SideSheet`）。
- 危险操作用 `confirmAction`；反馈用 `toastSuccess`/`toastError`。
- Markdown 统一走 `MarkdownView`（marked + DOMPurify），不要自行插入 HTML。

## 校验

```sh
npm ci --prefix web/frontend
npm run --prefix web/frontend build
go test ./internal/app -run TestSPA -count=1
```

`internal/app/spa_test.go` 会检查 `web/static/app/index.html` 引用的哈希资源是否存在，
防止构建产物缺失或未提交。页面开发契约见 `web/frontend/CONTRACT.md`。
