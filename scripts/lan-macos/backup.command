#!/bin/bash
set -euo pipefail
. "$(cd -P "$(dirname "$0")" && pwd)/common.sh"
require_mac
find_tools
is_app_running && fail '请等任务完成后打开“停止.command”，再执行备份，确保数据库和作品文件一致。'
[ -f "$RUNTIME/initialized" ] || fail '尚未初始化，没有新的本地数据需要备份。'
start_dependencies
mkdir -p "$LAN_ROOT/backups"
stamp="$(date +%Y%m%d-%H%M%S)"
"$MYSQL_BIN/mysqldump" --defaults-extra-file="$RUNTIME/mysql-client.cnf" --single-transaction --no-tablespaces --set-gtid-purged=OFF qiantie_lan > "$RUNTIME/work-backup.sql"
tar -czf "$LAN_ROOT/backups/yzcm-local-$stamp.tgz" -C "$LAN_ROOT" data private runtime/secrets.env runtime/work-backup.sql snapshot.json source-sha.txt
root_admin shutdown >/dev/null 2>&1 || true
redis_local SHUTDOWN SAVE >/dev/null 2>&1 || true
printf '备份完成：%s/backups/yzcm-local-%s.tgz\n请保留完整运行目录，迁回线上时使用此备份。\n' "$LAN_ROOT" "$stamp"
