export function mergeJobDurationSeconds(job) {
  const persisted = Number(job?.audioDurationSeconds || job?.durationSeconds || 0);
  if (persisted > 0 && Number.isFinite(persisted)) return persisted;
  const actualTotal = (job?.sources || []).reduce((sum, source) => {
    const seconds = Number(source?.actualDurationSeconds || 0);
    return sum + (seconds > 0 && Number.isFinite(seconds) ? seconds : 0);
  }, 0);
  const speed = Number(job?.speed || 0);
  return actualTotal > 0 && speed > 0 && Number.isFinite(speed) ? actualTotal / speed : 0;
}

export function formatMergeJobSpeed(job) {
  const speed = Number(job?.speed || 0);
  return `${speed > 0 && Number.isFinite(speed) ? speed.toFixed(3) : '1.000'}x`;
}
