export const PRODUCTION_STAGES = [
  { key:'image', label:'图片', href:'/comic-screenplay' }, { key:'video', label:'视频', href:'/comic-screenplay' },
  { key:'voice', label:'配音', href:'/comic-screenplay' }, { key:'subtitle', label:'字幕', href:'/ai-cut' },
  { key:'cut', label:'剪辑', href:'/ai-cut' }, { key:'export', label:'导出', href:'/ai-cut' },
];
export function productionStageCounts(jobs = []) { return jobs.reduce((out,row) => ({...out,[row.stage]:(out[row.stage]||0)+1}), Object.fromEntries(PRODUCTION_STAGES.map(row => [row.key,0]))); }
