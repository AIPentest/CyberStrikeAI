# Semi Design 前端预览

在经典界面的用户菜单选择「Semi 界面预览」，或访问 `/static/app/`。预览覆盖仪表盘、项目管理、资产概览和资产库、
漏洞管理、文件管理、MCP 调用监控、知识管理和检索历史、技能状态及平台权限。用户菜单可返回经典界面。
预览提供模型与工具设置。对话、任务和其余设置分区沿用经典界面的深链接和当前登录会话。

## 开发与验证

使用 Node.js 22。Vite 从 `src/main.jsx` 启动 React 和 Semi Design，路由注册在
`src/pages/registry.js`。页面通过 `src/lib/http.js` 调用同源 API，词条复用 `web/static/i18n/`。

```sh
npm ci --prefix web/frontend
npm run --prefix web/frontend dev
npm run --prefix web/frontend build
npm run --prefix web/frontend test:ui
```

Vite 预览地址为 `http://localhost:5199/static/app/`。本地联调时可以为 dev 命令设置
`CSAI_API_ORIGIN=http://127.0.0.1:8080`。浏览器回归使用匿名数据和模拟接口，覆盖登录、导航、
权限、图谱、表单、错误重试和移动端布局。

构建产物位于 `web/static/app/`，随源码提交。官方 Go 服务通过现有 `/static` 路径提供资源。
