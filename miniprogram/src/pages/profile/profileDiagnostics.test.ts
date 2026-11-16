import { expect, test } from 'vitest';
import { classifyProfileFailure, profileElapsedBucket, profilePhaseLabel } from './profileDiagnostics';

test.each([
  ['session-check', '检查本机会话'],
  ['request-started', '已发起资料请求'],
  ['response-received', '已收到服务端响应'],
  ['rendered', '账号资料已渲染'],
  ['failed', '读取失败'],
] as const)('maps %s to a safe phase label', (phase, label) => {
  expect(profilePhaseLabel(phase)).toBe(label);
});

test.each([
  [0, '不足 1 秒'], [999, '不足 1 秒'], [1000, '1–5 秒'],
  [5000, '5–15 秒'], [15000, '15 秒以上'],
])('buckets elapsed time without exposing request details', (milliseconds, label) => {
  expect(profileElapsedBucket(milliseconds)).toBe(label);
});

test.each([
  [Object.assign(new Error('expired'), { statusCode: 401 }), 'session-expired'],
  [new Error('账号资料读取超时，请检查网络后重新读取'), 'timeout'],
  [{ errMsg: 'request:fail url not in domain list' }, 'domain'],
  [{ errMsg: 'request:fail network' }, 'network'],
  [new Error('服务端未返回账号资料'), 'invalid-data'],
  [new Error('unexpected private server text'), 'unknown'],
] as const)('classifies failures without returning raw messages', (cause, category) => {
  expect(classifyProfileFailure(cause)).toBe(category);
});
