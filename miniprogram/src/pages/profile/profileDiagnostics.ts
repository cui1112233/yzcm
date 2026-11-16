export type ProfilePhase = 'session-check' | 'request-started' | 'response-received' | 'rendered' | 'failed';
export type ProfileFailureCategory = 'none' | 'session-missing' | 'session-expired' | 'timeout' | 'domain' | 'network' | 'invalid-data' | 'unknown';

const phaseLabels: Record<ProfilePhase, string> = {
  'session-check': '检查本机会话',
  'request-started': '已发起资料请求',
  'response-received': '已收到服务端响应',
  rendered: '账号资料已渲染',
  failed: '读取失败',
};

export function profilePhaseLabel(phase: ProfilePhase) {
  return phaseLabels[phase];
}

export function profileElapsedBucket(milliseconds: number) {
  if (milliseconds < 1000) return '不足 1 秒';
  if (milliseconds < 5000) return '1–5 秒';
  if (milliseconds < 15000) return '5–15 秒';
  return '15 秒以上';
}

export function classifyProfileFailure(cause: unknown): ProfileFailureCategory {
  const value = cause as { statusCode?: number; message?: string; errMsg?: string } | null;
  const text = `${value?.message || ''} ${value?.errMsg || ''}`.toLowerCase();
  if (value?.statusCode === 401 || /登录.*失效|session.*expired/.test(text)) return 'session-expired';
  if (/超时|timeout/.test(text)) return 'timeout';
  if (/domain list|合法域名/.test(text)) return 'domain';
  if (/network|网络|request:fail/.test(text)) return 'network';
  if (/未返回账号资料|invalid.*data/.test(text)) return 'invalid-data';
  return 'unknown';
}
