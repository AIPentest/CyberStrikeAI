#!/usr/bin/env bash
#
# CyberStrikeAI 从零环境搭建脚本
#
# 把「从 GitHub 拉下来 → 能跑起来、且和参考环境一致」之间缺的那几步固化成一条命令：
#   1. 检查构建依赖（Go / gcc / git）
#   2. 编译主服务二进制
#   3. 首次生成 config.yaml（不覆盖已有配置）
#   4. 创建运行时目录并处理权限（截图目录归渲染服务用户）
#   5. 安装并启用 systemd 单元（主服务 + 可选截图服务）
#   6. 可选：安装 Playwright + Chromium 并启动截图服务
#
# 用法：
#   sudo ./scripts/bootstrap.sh                      # 主服务 + systemd
#   sudo ./scripts/bootstrap.sh --with-screenshot    # 额外安装站点截图服务
#   ./scripts/bootstrap.sh --dry-run                 # 只打印将执行的动作
#
# 可选参数：
#   --run-user USER       主服务运行用户（默认 root；用普通用户需自行确认权限）
#   --shot-user USER      截图服务运行用户（默认当前 sudo 之前登录的用户；必须非 root）
#   --repo-dir DIR        部署目录（默认脚本所在仓库）
#   --no-systemd          只编译和准备目录，不安装 systemd 单元
#   --skip-build          跳过编译（二进制已存在时）
#   --dry-run             只展示计划，不执行
#
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_USER="root"
SHOT_USER="${SUDO_USER:-}"
WITH_SCREENSHOT=0
USE_SYSTEMD=1
DO_BUILD=1
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --run-user)      RUN_USER="$2"; shift 2 ;;
    --shot-user)     SHOT_USER="$2"; shift 2 ;;
    --repo-dir)      REPO_DIR="$(cd "$2" && pwd)"; shift 2 ;;
    --with-screenshot) WITH_SCREENSHOT=1; shift ;;
    --no-systemd)    USE_SYSTEMD=0; shift ;;
    --skip-build)    DO_BUILD=0; shift ;;
    --dry-run)       DRY_RUN=1; shift ;;
    -h|--help)       sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "未知参数: $1（--help 查看用法）" >&2; exit 2 ;;
  esac
done

info() { printf '\033[36m[%s]\033[0m %s\n' "$1" "$2"; }
ok()   { printf '\033[32m[ OK ]\033[0m %s\n' "$1"; }
warn() { printf '\033[33m[WARN]\033[0m %s\n' "$1"; }
die()  { printf '\033[31m[FAIL]\033[0m %s\n' "$1" >&2; exit 1; }
run()  {
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '  [dry-run] %s\n' "$*"
    return 0
  fi
  "$@"
}

if [ ! -f "$REPO_DIR/go.mod" ]; then
  die "看起来不是仓库根目录：$REPO_DIR（缺少 go.mod）"
fi
if [ "$DRY_RUN" -eq 0 ] && [ "$(id -u)" -ne 0 ]; then
  die "需要 root 权限安装 systemd 单元与目录（或用 --dry-run 预览）"
fi
if [ -z "$SHOT_USER" ]; then
  SHOT_USER="$RUN_USER"
fi
if [ "$WITH_SCREENSHOT" -eq 1 ] && [ "$SHOT_USER" = "root" ]; then
  die "截图服务不能用 root 运行：请用 --shot-user 指定普通用户"
fi
if ! id "$RUN_USER" >/dev/null 2>&1; then die "用户不存在: $RUN_USER"; fi
if [ "$WITH_SCREENSHOT" -eq 1 ] && ! id "$SHOT_USER" >/dev/null 2>&1; then
  die "用户不存在: $SHOT_USER"
fi

echo ""
info PLAN "仓库目录: $REPO_DIR"
info PLAN "主服务用户: $RUN_USER | 截图服务: $([ "$WITH_SCREENSHOT" -eq 1 ] && echo "$SHOT_USER" || echo '不安装')"
info PLAN "systemd: $([ "$USE_SYSTEMD" -eq 1 ] && echo 是 || echo 否) | 编译: $([ "$DO_BUILD" -eq 1 ] && echo 是 || echo 否)"
echo ""

# ---------- 1. 依赖检查 ----------
info DEPS "检查依赖"
command -v go >/dev/null || die "缺少 Go（需要 1.25+）：https://go.dev/dl/"
info DEP "  $(go version)"
if [ "$DO_BUILD" -eq 1 ]; then
  command -v gcc >/dev/null || die "缺少 gcc：go-sqlite3 依赖 cgo，无法编译。Ubuntu: apt install -y build-essential"
  ok "  gcc: $(gcc --version | head -1)"
fi
if [ "$WITH_SCREENSHOT" -eq 1 ]; then
  command -v node >/dev/null || die "缺少 node（截图服务需要 Node 18+）"
  command -v npm >/dev/null || die "缺少 npm"
  info DEP "  node: $(node --version)"
fi

# ---------- 2. 首次配置 ----------
info CONFIG "准备 config.yaml"
if [ -f "$REPO_DIR/config.yaml" ]; then
  ok "  已存在，保持不变"
else
  [ -f "$REPO_DIR/config.example.yaml" ] || die "缺少 config.example.yaml"
  run cp "$REPO_DIR/config.example.yaml" "$REPO_DIR/config.yaml"
  ok "  已从 config.example.yaml 生成，**请随后填写 openai.api_key 等密钥**"
fi

# ---------- 3. 目录与权限 ----------
info DIRS "创建运行时目录"
run mkdir -p "$REPO_DIR/data" "$REPO_DIR/log" "$REPO_DIR/tmp"
if [ "$WITH_SCREENSHOT" -eq 1 ]; then
  run mkdir -p "$REPO_DIR/data/screenshots"
  # 截图由非 root 的渲染服务写盘，目录必须归该用户。
  run chown "$SHOT_USER:$SHOT_USER" "$REPO_DIR/data/screenshots"
  ok "  data/screenshots -> $SHOT_USER"
fi

# ---------- 4. 编译 ----------
if [ "$DO_BUILD" -eq 1 ]; then
  info BUILD "编译主服务"
  ( cd "$REPO_DIR" && run env GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" go build -o cyberstrike-ai ./cmd/server )
  ok "  $REPO_DIR/cyberstrike-ai"
fi

# ---------- 5. systemd ----------
if [ "$USE_SYSTEMD" -eq 1 ]; then
  info SYSTEMD "安装服务单元"
  # 主服务单元：从模板替换占位符
  tmp_unit="$(mktemp)"
  sed -e "s|__REPO_DIR__|$REPO_DIR|g" \
      -e "s|__RUN_USER__|$RUN_USER|g" \
      -e "s|__HOME_DIR__|$(eval echo "~$RUN_USER")|g" \
      "$REPO_DIR/scripts/systemd/cyberstrikeai.service.example" > "$tmp_unit"
  run install -m 644 "$tmp_unit" /etc/systemd/system/cyberstrikeai.service
  rm -f "$tmp_unit"

  if [ "$WITH_SCREENSHOT" -eq 1 ]; then
    HOME_SHOT="$(eval echo "~$SHOT_USER")"
    shot_unit="$(mktemp)"
    sed -e "s|/home/idamini/shot-service|$HOME_SHOT/shot-service|g" \
        -e "s|User=idamini|User=$SHOT_USER|g" \
        -e "s|Group=idamini|Group=$SHOT_USER|g" \
        -e "s|/home/idamini/.cache/ms-playwright|$HOME_SHOT/.cache/ms-playwright|g" \
        -e "s|/home/idamini/桌面/CyberStrikeAI/data/screenshots|$REPO_DIR/data/screenshots|g" \
        "$REPO_DIR/scripts/shot-service/cyberstrike-shot.service" > "$shot_unit"
    run install -m 644 "$shot_unit" /etc/systemd/system/cyberstrike-shot.service
    rm -f "$shot_unit"
  fi

  run systemctl daemon-reload
  run systemctl enable cyberstrikeai
  if [ "$WITH_SCREENSHOT" -eq 1 ]; then
    run systemctl enable cyberstrike-shot
  fi
  ok "  单元已安装并设为开机自启"
fi

# ---------- 6. 截图服务依赖 ----------
if [ "$WITH_SCREENSHOT" -eq 1 ]; then
  info SHOT "安装 Playwright + Chromium（用户 $SHOT_USER，约 200MB）"
  info SHOT "  目标目录: $(eval echo "~$SHOT_USER")/shot-service"
  if [ "$DRY_RUN" -eq 0 ]; then
    HOME_SHOT="$(eval echo "~$SHOT_USER")"
    runuser -u "$SHOT_USER" -- bash -c '
      set -e
      mkdir -p ~/shot-service && cd ~/shot-service
      [ -f package.json ] || npm init -y >/dev/null
      npm i playwright --no-audit --no-fund >/dev/null 2>&1
      npx playwright install chromium
    '
    ok "  Playwright 就绪"
    install -m 644 "$REPO_DIR/scripts/shot-service/server.js" "$HOME_SHOT/shot-service/server.js"
    ok "  server.js 已部署到 $HOME_SHOT/shot-service/"
  else
    info SHOT "  [dry-run] 跳过实际安装"
  fi
fi

# ---------- 7. 收尾提示 ----------
echo ""
info DONE "环境准备完成"
cat <<EOF

下一步：
  1) 填写密钥：编辑 $REPO_DIR/config.yaml（openai.api_key 等；FOFA/ZoomEye/Quake 按需）
  2) 启动主服务：  sudo systemctl start cyberstrikeai
     或前台调试：  cd $REPO_DIR && ./cyberstrike-ai -config config.yaml --https
  3) 首次启动会在 data/ 下创建数据库，并在日志打印一次性的 admin 密码：
       sudo journalctl -u cyberstrikeai -n 50 | grep -A3 "ADMIN SETUP"
  4) 访问：https://<本机IP>:8080/（自签证书需在浏览器点一次继续）

站点截图（若本次未安装）：
  sudo ./scripts/bootstrap.sh --with-screenshot --shot-user <普通用户>
  详见 scripts/shot-service/README.md 与 docs/zh-CN/deployment.md

注意事项：
  - config.yaml 与 data/ 含密钥和数据，已被 .gitignore 忽略，**不要提交**。
  - 截图服务必须用非 root 用户运行；截图目录权限已按该用户处理。
EOF
