# Semi Design 工作台

CyberStrikeAI 默认从 `/` 打开 React + Semi Design 工作台。仪表盘、项目、资产、漏洞、文件、MCP 监控、
知识、技能、角色与智能体查看、平台权限、系统设置和 API 文档共用现有登录会话与权限。

原有操作页通过 `/classic` 打开。旧书签继续可用，例如 `/#projects?id=...`、
`/#chat?conversation=...` 和 `/#system-update`。系统设置内的「更多设置」打开现有高级设置，
API 文档内的「打开 API 控制台」打开原 API 控制台。

## 开发与验证

使用 Node.js 22。Vite 从 `src/main.jsx` 启动 React 和 Semi Design。页面在
`src/pages/registry.js` 注册，通过 `src/lib/http.js` 调用同源 API，词条复用 `web/static/i18n/`。

```sh
npm ci --prefix web/frontend
npm run --prefix web/frontend dev
npm run --prefix web/frontend build
node --test web/frontend/tests/*.test.mjs
npm run --prefix web/frontend test:ui
```

Vite 开发地址为 `http://localhost:5199/static/app/`。需要联调时，可为 dev 命令设置
`CSAI_API_ORIGIN=http://127.0.0.1:8080`。浏览器回归使用匿名数据和模拟接口，覆盖登录、导航、
权限、图谱、表单、设置及移动端布局。

构建产物写入 `web/static/app/`，随源码提交。服务端用现有静态资源路径提供 JS 和 CSS，
根路径返回构建后的 HTML。
