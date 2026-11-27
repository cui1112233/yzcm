#!/bin/bash
set -euo pipefail
. "$(cd -P "$(dirname "$0")" && pwd)/common.sh"
require_mac
find_brew
printf '将安装 Mac 原生 MySQL 8.4、Redis 和 FFmpeg；不安装 Go 或 Node。\n'
"$BREW" install mysql@8.4 redis ffmpeg
printf '\n环境安装完成，现在可以打开“启动.command”。\n'
