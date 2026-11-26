#!/bin/bash
set -euo pipefail
umask 077
LAN_ROOT="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
case "$LAN_ROOT" in *'"'*|*'\'*|*$'\n'*) printf '请把运行包放到不含双引号、反斜线或换行的目录。\n' >&2; exit 1;; esac
cd "$LAN_ROOT"
RUNTIME="$LAN_ROOT/runtime"
MYSQL_PORT=13306
REDIS_PORT=16379
HTTP_PORT=4001
mkdir -p "$RUNTIME/logs" "$LAN_ROOT/data/system" "$LAN_ROOT/data/users"

fail() { printf '\n%s\n' "$*" >&2; exit 1; }
find_brew() {
  if [ -x /opt/homebrew/bin/brew ]; then BREW=/opt/homebrew/bin/brew
  elif command -v brew >/dev/null 2>&1; then BREW="$(command -v brew)"
  elif [ -x /usr/local/bin/brew ]; then BREW=/usr/local/bin/brew
  else fail '尚未安装 Homebrew。请先按“使用说明.txt”完成首次安装，再打开“首次安装.command”。'; fi
}
find_tools() {
  find_brew
  MYSQL_BIN="$("$BREW" --prefix mysql@8.4)/bin"
  REDIS_BIN="$("$BREW" --prefix redis)/bin"
  FFMPEG_BIN="$("$BREW" --prefix ffmpeg)/bin"
  export PATH="$MYSQL_BIN:$REDIS_BIN:$FFMPEG_BIN:$PATH"
  for tool in mysqld mysql mysqladmin mysqldump redis-server redis-cli ffmpeg ffprobe; do
    command -v "$tool" >/dev/null 2>&1 || fail "缺少 $tool，请先打开首次安装.command。"
  done
}
require_mac() { [ "$(uname -s)" = Darwin ] || fail '此启动入口仅供苹果电脑使用。'; }
select_binary() {
  case "$1" in arm64) APP_BIN="$LAN_ROOT/bin/qiantie-darwin-arm64";;
    x86_64) APP_BIN="$LAN_ROOT/bin/qiantie-darwin-amd64";;
    *) fail "暂不支持此芯片：$1";; esac
}
root_mysql() { "$MYSQL_BIN/mysql" --defaults-extra-file="$RUNTIME/mysql-client.cnf" "$@"; }
root_admin() { "$MYSQL_BIN/mysqladmin" --defaults-extra-file="$RUNTIME/mysql-client.cnf" "$@"; }
redis_local() { REDISCLI_AUTH="$LAN_REDIS_PASSWORD" "$REDIS_BIN/redis-cli" -h 127.0.0.1 -p "$REDIS_PORT" "$@"; }
is_app_running() {
  [ -f "$RUNTIME/app.pid" ] || return 1
  APP_PID="$(cat "$RUNTIME/app.pid")"
  case "$APP_PID" in ''|*[!0-9]*) return 1;; esac
  kill -0 "$APP_PID" 2>/dev/null || return 1
  ps -ww -p "$APP_PID" -o command= | grep -F "$LAN_ROOT/bin/qiantie-" >/dev/null
}
write_local_secrets() {
  if [ ! -f "$RUNTIME/secrets.env" ]; then
    printf 'LAN_DB_PASSWORD=%s\nLAN_ROOT_PASSWORD=%s\nLAN_REDIS_PASSWORD=%s\nLAN_BRIDGE_SECRET=%s\n' \
      "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" "$(openssl rand -hex 32)" > "$RUNTIME/secrets.env"
  fi
  . "$RUNTIME/secrets.env"
  for value in "$LAN_DB_PASSWORD" "$LAN_ROOT_PASSWORD" "$LAN_REDIS_PASSWORD" "$LAN_BRIDGE_SECRET"; do
    case "$value" in ''|*[!0-9a-f]*) fail '本地凭据文件格式错误，未启动任何服务。';; esac
  done
  SOCKET_TAG="$(printf '%s' "$LAN_ROOT" | shasum -a 256 | cut -c1-16)"
  MYSQL_SOCKET="/tmp/yzcm-lan-$SOCKET_TAG.sock"
  printf '[client]\nuser=root\nprotocol=SOCKET\nsocket=%s\npassword=%s\n' "$MYSQL_SOCKET" "$LAN_ROOT_PASSWORD" > "$RUNTIME/mysql-client.cnf"
}
start_dependencies() {
  write_local_secrets
  if ! root_admin ping --silent >/dev/null 2>&1; then
    if nc -z 127.0.0.1 "$MYSQL_PORT" 2>/dev/null; then fail '本地数据库端口已被其他程序使用，未覆盖它。'; fi
    mkdir -p "$RUNTIME/mysql"
    printf '[mysqld]\nbind-address=127.0.0.1\nport=%s\nsocket=%s\ndatadir=%s\npid-file=%s\nlog-error=%s\nmysqlx=0\nskip-log-bin\nskip-name-resolve\nmax_connections=60\ninnodb_buffer_pool_size=256M\n' \
      "$MYSQL_PORT" "$MYSQL_SOCKET" "$RUNTIME/mysql" "$RUNTIME/mysql.pid" "$RUNTIME/logs/mysql.log" > "$RUNTIME/mysql.cnf"
    if [ ! -d "$RUNTIME/mysql/mysql" ]; then
      "$MYSQL_BIN/mysqld" --defaults-file="$RUNTIME/mysql.cnf" --initialize-insecure >> "$RUNTIME/logs/mysql-init.log" 2>&1
    fi
    "$MYSQL_BIN/mysqld" --defaults-file="$RUNTIME/mysql.cnf" --daemonize
    ready=0
    for tick in $(seq 1 90); do
      if root_admin ping --silent >/dev/null 2>&1 || "$MYSQL_BIN/mysqladmin" --no-defaults --socket="$MYSQL_SOCKET" -uroot ping --silent >/dev/null 2>&1; then ready=1; break; fi
      sleep 1
    done
    [ "$ready" = 1 ] || fail '本地 MySQL 未能启动，请查看 runtime/logs/mysql.log。'
  fi
  if [ ! -f "$RUNTIME/initialized" ]; then
    printf '首次初始化账号、权限及配置，请稍候……\n'
    # Import once into this package's new database. Never restore over a previously initialized installation.
    if root_mysql -e 'SELECT 1' >/dev/null 2>&1; then
      root_mysql -e 'CREATE DATABASE IF NOT EXISTS qiantie_lan CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'
      root_mysql qiantie_lan < "$LAN_ROOT/private/seed.sql"
      root_mysql -e "CREATE USER IF NOT EXISTS 'qiantie'@'127.0.0.1' IDENTIFIED BY '$LAN_DB_PASSWORD'; GRANT ALL ON qiantie_lan.* TO 'qiantie'@'127.0.0.1';"
    else
      "$MYSQL_BIN/mysql" --no-defaults --socket="$MYSQL_SOCKET" -uroot -e 'CREATE DATABASE IF NOT EXISTS qiantie_lan CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci'
      "$MYSQL_BIN/mysql" --no-defaults --socket="$MYSQL_SOCKET" -uroot qiantie_lan < "$LAN_ROOT/private/seed.sql"
      "$MYSQL_BIN/mysql" --no-defaults --socket="$MYSQL_SOCKET" -uroot -e "CREATE USER IF NOT EXISTS 'qiantie'@'127.0.0.1' IDENTIFIED BY '$LAN_DB_PASSWORD'; GRANT ALL ON qiantie_lan.* TO 'qiantie'@'127.0.0.1'; ALTER USER 'root'@'localhost' IDENTIFIED BY '$LAN_ROOT_PASSWORD';"
    fi
    root_mysql qiantie_lan -e 'SELECT COUNT(*) FROM accounts' > "$RUNTIME/imported-account-count.txt"
    touch "$RUNTIME/initialized"
  fi
  if ! redis_local PING 2>/dev/null | grep -qx PONG; then
    if nc -z 127.0.0.1 "$REDIS_PORT" 2>/dev/null; then fail '本地队列端口已被其他程序使用，未覆盖它。'; fi
    mkdir -p "$RUNTIME/redis"
    printf 'bind 127.0.0.1\nprotected-mode yes\nport %s\nrequirepass %s\ndir "%s"\nappendonly yes\ndaemonize yes\npidfile "%s"\nlogfile "%s"\n' \
      "$REDIS_PORT" "$LAN_REDIS_PASSWORD" "$RUNTIME/redis" "$RUNTIME/redis.pid" "$RUNTIME/logs/redis.log" > "$RUNTIME/redis.conf"
    "$REDIS_BIN/redis-server" "$RUNTIME/redis.conf"
  fi
  redis_local PING 2>/dev/null | grep -qx PONG || fail '本地 Redis 未能启动，请查看 runtime/logs/redis.log。'
}
configure_app() {
  . "$LAN_ROOT/private/settings.env"
  export QIANTIE_MYSQL_DSN="qiantie:$LAN_DB_PASSWORD@tcp(127.0.0.1:$MYSQL_PORT)/qiantie_lan?parseTime=true&multiStatements=true"
  export QIANTIE_REDIS_URL="redis://:$LAN_REDIS_PASSWORD@127.0.0.1:$REDIS_PORT/0"
  export QIANTIE_BRIDGE_SECRET="$LAN_BRIDGE_SECRET"
  export QIANTIE_GO_AUTH_ENABLED=1
  export QIANTIE_GO_LISTEN_ADDR=":$HTTP_PORT"
  export QIANTIE_BATCH_FACTORY_V11_SLICE=5
  export QIANTIE_BATCH_FACTORY_V11_DYNAMIC_TEXT_PROVIDER=1
  export QIANTIE_BATCH_FACTORY_V11_PRODUCTION_ENABLED=1
  export QIANTIE_BATCH_FACTORY_V11_MERGE_ENABLED=1
  export QIANTIE_BATCH_FACTORY_V11_LOCAL_MERGE_ENABLED=1
  export QIANTIE_AUTOMATION_RESUME_ON_BOOT=1
  export QIANTIE_PRESET_PATH="$LAN_ROOT/data/system/presets.json"
  export QIANTIE_PERSONAL_PROMPT_PATH="$LAN_ROOT/data/system/script-constraint-prompts.json"
  export QIANTIE_SCRIPT_HISTORY_DIR="$LAN_ROOT/data/users"
  export QIANTIE_MEMBER_SYSTEM_DIR="$LAN_ROOT/data/system"
  export QIANTIE_MEMBER_AVATAR_DIR="$LAN_ROOT/data/avatars"
  export QIANTIE_CONFIG_SECRET_KEY_FILE="$LAN_ROOT/data/system/config-secret.key"
  export QIANTIE_BATCH_FACTORY_AUTOMATION_PRESETS_PATH="$LAN_ROOT/data/batch-factory-v11-automation-presets.json"
  export QIANTIE_BATCH_FACTORY_AUTOMATION_STATE_PATH="$LAN_ROOT/data/local-automation.json"
  export QIANTIE_LOCAL_EXECUTOR_ARTIFACT_DIR="$LAN_ROOT/data/local-executor-artifacts"
  export QIANTIE_AGENT_ASSET_LOCAL_DIR="$LAN_ROOT/data/agent-assets"
  export QIANTIE_LOCAL_EXECUTOR_DOWNLOADS_DIR="$LAN_ROOT/data/downloads"
  export QIANTIE_FFMPEG_PATH="$FFMPEG_BIN/ffmpeg"
  export QIANTIE_FFPROBE_PATH="$FFMPEG_BIN/ffprobe"
  export GOMAXPROCS="${GOMAXPROCS:-4}"
  export QIANTIE_RELEASE_SHA="$(cat "$LAN_ROOT/source-sha.txt")"
  export QIANTIE_RUNTIME_MANIFEST_PATH="$RUNTIME/runtime-release.json"
  printf '{"gitSha":"%s","frontendEntry":"%s","sha256":"%s"}\n' "$QIANTIE_RELEASE_SHA" \
    "$(cat "$LAN_ROOT/frontend-entry.txt")" "$(shasum -a 256 "$APP_BIN" | cut -d' ' -f1)" > "$QIANTIE_RUNTIME_MANIFEST_PATH"
}
