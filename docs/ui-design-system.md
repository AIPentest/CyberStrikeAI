# 界面样式约定

主应用的公共样式位于 `web/static/css/design-system.css`，在模块与移动端 CSS 后加载。
颜色沿用现有主题变量；卡片圆角 16px，控件圆角 10px，桌面正文内边距 24px，移动端 16px。
状态色、选中态、图表、终端和工作流画布保留各自的业务语义。

## 容器选择

- 详情、日志、长表单：右侧抽屉，默认 720px；代码、长文本等宽编辑面板 960px；标题栏可展开至 1440px。
- 手机（小于 768px）：抽屉全屏，适配安全区，正文单独滚动，底部操作保持可见。
- 确认、重命名、简短绑定：居中短弹窗，手机上同样保持紧凑。
- 数据比较和批量操作继续使用表格；终端、工作流、会话工作区不强制改成卡片列表。

`web/static/js/modal.js` 的 `DRAWER_IDS` 和 `WIDE_IDS` 管理既有模块迁移清单。
新模块应通过 `openAppModal` / `closeAppModal` 开关容器，关闭时保留原有清理和未保存提示逻辑。
共用生命周期负责嵌套层级、背景滚动锁定、焦点返回、Tab 焦点约束和 Escape 触发原关闭按钮。
异步更新的标题栏会自动补齐展开按钮。短 C2 休眠设置仍为弹窗。

不要再为单个抽屉添加独立的定位、遮罩、圆角或主题色覆盖。
现阶段公共布局中少量 `!important` 用于覆盖历史内联宽高和移动端旧规则；后续修改这些模块时可逐步去除对应历史声明。

## 验证

前端回归：

```sh
node --test web/static/js/*.test.cjs
```

浏览器验证需要外部安装的 Playwright 和 Chromium，例如：

```sh
npm install --prefix /tmp/csai-ui-tools playwright
/tmp/csai-ui-tools/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/csai-ui-tools/node_modules node scripts/check-ui-design-system.cjs
```

脚本使用真实模板、样式和选定模块的真实渲染函数，配以合成数据；不启动后端或访问测试目标。
检查 1440、1024、390、360px 四档屏宽和深浅主题，验证抽屉边界、内部横向溢出、底栏、嵌套层级、展开、键盘关闭、焦点返回，以及主页面横向溢出。
截图与结果默认写入系统临时目录，也可通过 `CSAI_UI_ARTIFACTS` 指定目录。
这覆盖界面布局和公共交互，不等同于真实后端数据、保存操作、所有下拉菜单状态或物理手机软键盘的完整验收。
