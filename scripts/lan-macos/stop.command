#!/bin/bash
set -euo pipefail
. "$(cd -P "$(dirname "$0")" && pwd)/common.sh"
require_mac
find_tools
write_local_secrets
if is_app_running; then
  pending="$(root_mysql qiantie_lan -N -e "SELECT COUNT(*) FROM batch_automation_jobs WHERE state='running'")"
  [ "$pending" = 0 ] || fail '还有自动生产任务在执行或排队，请先在页面暂停任务并等待正在出图的请求结束，再关闭。'
  media_pending="$(root_mysql qiantie_lan -N -e "SELECT (SELECT COUNT(*) FROM batch_factory_v12_production_jobs WHERE status IN ('queued','running')) + (SELECT COUNT(*) FROM batch_factory_v12_merge_jobs WHERE status IN ('queued','running')) + (SELECT COUNT(*) FROM agent_video_jobs WHERE state IN ('submitting','queued','running','result_ready','ingesting'))")"
  [ "$media_pending" = 0 ] || fail '还有视频生产、合成或结果保存任务，请等待完成后再关闭。'
  busy="$(redis_local --raw EVAL "local n=0;local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000);for _,k in ipairs(redis.call('KEYS','qiantie:imagecapacity:*')) do if redis.call('TYPE',k).ok=='string' then local ok,s=pcall(cjson.decode,redis.call('GET',k));if ok and type(s)=='table' and s.leases then for _,v in pairs(s.leases) do if v.deadline>now then n=n+1 end end end end end;return n" 0)"
  [ "$busy" = 0 ] || fail '还有已提交的图片请求，稍等它们返回后再关闭，避免丢图。'
  kill -TERM "$APP_PID"
  for tick in $(seq 1 20); do kill -0 "$APP_PID" 2>/dev/null || break; sleep 1; done
  is_app_running && fail '程序尚未退出，没有强行结束。'
fi
root_admin shutdown >/dev/null 2>&1 || true
redis_local SHUTDOWN SAVE >/dev/null 2>&1 || true
printf '本运行包已停止，数据保留。\n'
