#!/bin/bash
set -euo pipefail
. "$(cd -P "$(dirname "$0")" && pwd)/common.sh"
require_mac
find_tools
machine="$(uname -m)"
if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then machine=arm64; fi
select_binary "$machine"
[ -f "$APP_BIN" ] || fail '缺少对应芯片的主程序，请完整解压运行包。'
chmod 700 "$APP_BIN" "$LAN_ROOT/private" "$RUNTIME"
# This is our own unsigned build, not a global Gatekeeper change.
# Only clear the download marker on the exact selected package executable.
xattr -d com.apple.quarantine "$APP_BIN" 2>/dev/null || true
if is_app_running; then printf '程序已经运行：http://localhost:%s\n' "$HTTP_PORT"; exit 0; fi
if nc -z 127.0.0.1 "$HTTP_PORT" 2>/dev/null; then fail '4001 端口已有其他程序使用，未关闭或替换它。'; fi
mkdir "$RUNTIME/start.lock" 2>/dev/null || fail '另一个启动过程尚未结束，请等待；若之前异常退出，确认没有启动进程后删除 runtime/start.lock 空目录。'
trap 'rmdir "$RUNTIME/start.lock" 2>/dev/null || true' EXIT
start_dependencies
configure_app
nohup "$APP_BIN" > "$RUNTIME/logs/app.log" 2>&1 < /dev/null &
printf '%s\n' "$!" > "$RUNTIME/app.pid"
ready=0
for tick in $(seq 1 180); do
  if curl --noproxy '*' -fsS "http://127.0.0.1:$HTTP_PORT/health" >/dev/null 2>&1; then ready=1; break; fi
  is_app_running || fail '主程序启动失败，请查看 runtime/logs/app.log。'
  sleep 1
done
[ "$ready" = 1 ] || fail '启动等待超时；程序可能仍在初始化，请查看 runtime/logs/app.log，不要重复启动。'
printf '\n启动成功，本机访问：http://localhost:%s\n同事在同一局域网访问以下地址：\n' "$HTTP_PORT"
ifconfig | awk '/inet / && $2 !~ /^(127\.|169\.254\.)/ {print "http://" $2 ":4001"}'
printf '使用线上原账号和原密码登录。账号为打包时快照，之后不会自动同步。\n'
open "http://localhost:$HTTP_PORT"
