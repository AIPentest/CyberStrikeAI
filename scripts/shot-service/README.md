# 站点截图服务（Playwright + Chromium）

CyberStrikeAI 的「站点截图」不是主进程自己渲染的：主服务以 root 运行，直接渲染不可信的
目标站点风险太高。这里把浏览器放进一个**独立的非 root 进程**，主服务只通过
`127.0.0.1` 的 HTTP 接口让它「给一个 URL，渲染一张 PNG」。

## 组成

| 文件 | 说明 |
|---|---|
| `server.js` | 截图服务本体（Node + Playwright），提供 `GET /health` 与 `POST /shot` |
| `cyberstrike-shot.service` | systemd 单元，以 `idamini`（非 root）运行 |

## 安装

```bash
# 1) 安装依赖（不要用 root 跑浏览器，用运行服务的普通用户）
mkdir -p ~/shot-service && cd ~/shot-service
npm init -y
npm i playwright --no-audit --no-fund
npx playwright install chromium          # 约 120MB

# 2) 部署服务代码
cp server.js ~/shot-service/

# 3) 截图落盘目录：主服务建目录、chown 给浏览器运行用户
sudo mkdir -p <仓库>/data/screenshots
sudo chown <运行用户>:<运行用户> <仓库>/data/screenshots

# 4) 安装并启动 systemd 单元（按需修改 User/路径）
sudo cp cyberstrike-shot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cyberstrike-shot
systemctl is-active cyberstrike-shot
curl -s http://127.0.0.1:8099/health
```

## 与主服务的约定

主服务配置（`config.yaml`，全部可省略走默认值）：

```yaml
screenshot:
  enabled: true
  service_url: http://127.0.0.1:8099
  dir: data/screenshots
  timeout_seconds: 30
  concurrency: 2
  viewport_width: 1280
  viewport_height: 800
  min_interval_hours: 24
```

主服务启动时会探测 `/health`，不可用则降级为「不自动截图」（手动截图接口返回 503），
不会给每个资产写失败记录。

## 安全说明

- 浏览器**永远以非 root 用户运行**，这是主要边界；
- systemd 单元带 `NoNewPrivileges=true`、`PrivateTmp=true`、`ProtectSystem=full`；
- `server.js` 只接受 `http://` 与 `https://`，拒绝 `file://`、`data:` 等协议；
- 渲染超时、并发数、单次请求体大小都有上限；
- 本功能会访问目标站点，**请仅在已授权范围内使用**。
