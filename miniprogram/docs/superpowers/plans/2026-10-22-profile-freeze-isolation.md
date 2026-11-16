# Profile Freeze Isolation v1.0.8 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a `v1.0.8` diagnostic experience build in which only the “我的” tab leaves the liquid-glass compositor path, remains navigable during profile failures, and visibly reports the last safe profile-loading phase.

**Architecture:** Keep the existing public-account session and `/api/member/me` contracts unchanged. Exclude the profile root from `liquidEnabled`, explicitly restore the native WeChat TabBar when that non-liquid tab is shown, and add a small pure diagnostic model consumed by the profile page. The page owns one active request identity, rejects late responses after cancellation/session changes, and exposes retry and return-to-login controls without rendering secrets.

**Tech Stack:** Taro 4.2.1, React 18.3.1, TypeScript 5.7.3, Vitest 3.2.4, WeChat Mini Program production build (`dist/`).

## Global Constraints

- Change only the actual mini-program checkout at `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram`; WeChat Developer Tools must open its generated `dist/` directory.
- Preserve public-account login, Bearer/Cookie session behavior, `/api/member/me`, and every page other than `pages/profile/index`.
- Remove liquid glass only from `pages/profile/index`; retain the existing dark theme, content hierarchy, and native WeChat TabBar.
- Diagnostic UI may show only `v1.0.8`, phase label, elapsed-time bucket, and safe failure category. Never render tokens, cookies, passwords, full headers, request URLs with query strings, or response bodies.
- A profile request has a 15-second maximum wait. Mount and `useDidShow` share one request lock; cancellation, unmount, session revision changes, and newer attempts invalidate late responses.
- The repository is already dirty. Never reset, clean, stage, or commit unrelated files; never stage `.env.production`.
- Local tests, build success, Developer Tools simulation, and upload success do not prove the freeze is fixed. Completion requires the `v1.0.8` label plus true-device navigation/logout/profile acceptance.

---

## File Map

- Modify `src/ui/liquidGlass.ts`: stop opting the profile root into liquid rendering.
- Modify `src/ui/liquidGlass.test.ts`: lock the profile exclusion contract.
- Modify `src/theme.tsx`: restore the native TabBar when the non-liquid profile tab is shown.
- Modify `src/theme.test.ts`: verify native TabBar restoration is scoped to the profile root.
- Create `src/pages/profile/profileDiagnostics.ts`: define safe phases, failure categories, and elapsed buckets.
- Create `src/pages/profile/profileDiagnostics.test.ts`: exhaustively test the pure diagnostic mapping.
- Modify `src/pages/profile/index.tsx`: integrate diagnostics, request identity/cancellation, retry, and return-to-login controls; remove per-card refraction from this page.
- Modify `src/pages/profile/index.test.tsx`: verify request deduplication, phase progression, late-response rejection, retry, and login escape.
- Modify `src/app.scss`: add ordinary dark-material and compact diagnostic styles for the profile root.
- Modify `src/config/release.ts`: publish the client-visible version as `1.0.8`.
- Modify `src/components/LoginScene.test.tsx`: lock the login-page version label to `v1.0.8`.

### Task 1: Isolate The Profile Tab From Liquid Composition

**Files:**
- Modify: `src/ui/liquidGlass.ts`
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/theme.tsx`
- Modify: `src/theme.test.ts`

**Interfaces:**
- Consumes: `liquidEnabled(className: string): boolean`, `Taro.showTabBar`, and the existing `ThemedPage` class-name contract.
- Produces: `liquidEnabled("mp-page mp-profile-page") === false`; showing `mp-profile-page` calls `Taro.showTabBar({ animation: false })` without changing non-tab detail pages.

- [ ] **Step 1: Change the liquid-material test to describe the isolation build**

```ts
test('profile isolation keeps the diagnostic tab outside liquid composition', () => {
  expect(liquidEnabled('mp-page native-login-page')).toBe(false);
  expect(liquidEnabled('mp-page mp-profile-page')).toBe(false);
  expect(liquidEnabled('mp-page mp-primary-page mp-functions-page')).toBe(true);
  expect(liquidEnabled('mp-page mp-profile-detail-page')).toBe(true);
  expect(liquidEnabled('mp-page mp-studio')).toBe(false);
});
```

- [ ] **Step 2: Add a focused theme test for native TabBar restoration**

Mock `showTabBar` alongside the existing Taro mocks, render `ThemedPage` with `className="mp-page mp-profile-page"`, invoke the registered `useDidShow` callback, and assert:

```ts
expect(taro.showTabBar).toHaveBeenCalledWith({ animation: false });
expect(taro.hideTabBar).not.toHaveBeenCalled();
```

Also render `mp-detail-page` and assert `showTabBar` is not called, preventing ordinary subpages from exposing tab chrome.

- [ ] **Step 3: Run the focused tests and confirm the expected failures**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/theme.test.ts`

Expected: FAIL because profile currently returns `true` from `liquidEnabled` and `ThemedPage` does not call `showTabBar` for the isolated root.

- [ ] **Step 4: Remove only the profile root from `liquidEnabled`**

```ts
export function liquidEnabled(className: string) {
  const classes = className.split(/\s+/);
  return classes.includes('mp-primary-page') || classes.includes('mp-profile-detail-page');
}
```

- [ ] **Step 5: Restore native tab chrome only for the isolated profile root**

In `ThemedPage`, derive the route intent without introducing a conditional hook:

```ts
const nativeProfileTab = className.split(/\s+/).includes('mp-profile-page') && !liquid;
useDidShow(() => {
  if (nativeProfileTab) void Taro.showTabBar?.({ animation: false });
});
```

Keep the existing `LiquidNavigation` behavior unchanged for the other liquid tab roots. This explicit restore is required because the previous liquid tab may have hidden native navigation before switching to “我的”.

- [ ] **Step 6: Run focused tests**

Run: `npx vitest run src/ui/liquidGlass.test.ts src/theme.test.ts`

Expected: PASS.

- [ ] **Step 7: Commit only the isolation contract**

```bash
git add src/ui/liquidGlass.ts src/ui/liquidGlass.test.ts src/theme.tsx src/theme.test.ts
git commit -m "fix: isolate profile from liquid glass"
```

### Task 2: Add A Safe Profile Diagnostic Model

**Files:**
- Create: `src/pages/profile/profileDiagnostics.ts`
- Create: `src/pages/profile/profileDiagnostics.test.ts`

**Interfaces:**
- Produces: `ProfilePhase`, `ProfileFailureCategory`, `profilePhaseLabel(phase)`, `profileElapsedBucket(milliseconds)`, and `classifyProfileFailure(cause)`.
- Consumes: only primitive values and error-like objects; this module must not import Taro, session storage, or API response types.

- [ ] **Step 1: Write failing tests for every public diagnostic value**

```ts
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
```

- [ ] **Step 2: Run the new test and verify it fails because the module is absent**

Run: `npx vitest run src/pages/profile/profileDiagnostics.test.ts`

Expected: FAIL with a module resolution error for `./profileDiagnostics`.

- [ ] **Step 3: Implement the closed diagnostic vocabulary**

```ts
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
```

The UI must map the returned category to fixed user-facing copy; it must never render `cause.message` as diagnostic detail.

- [ ] **Step 4: Run the focused diagnostic test**

Run: `npx vitest run src/pages/profile/profileDiagnostics.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit the pure diagnostic model**

```bash
git add src/pages/profile/profileDiagnostics.ts src/pages/profile/profileDiagnostics.test.ts
git commit -m "feat: add safe profile load diagnostics"
```

### Task 3: Make Profile Loading Escapable And Reject Late Results

**Files:**
- Modify: `src/pages/profile/index.tsx`
- Modify: `src/pages/profile/index.test.tsx`
- Modify: `src/app.scss`

**Interfaces:**
- Consumes: Task 2 diagnostic functions; `sessionRevision(): number`; existing `readMemberProfile()`, `logoutAccount()`, `hasAccountSession()`, and `MINIPROGRAM_VERSION`.
- Produces: one active request at a time; safe visible stage; `cancelAndLogin(): void`; retry after failure; ordinary-material profile content without `PageTextureRefraction` or `AmbientStarfield`.

- [ ] **Step 1: Extend the page harness with observable controls**

Add mocks for `switchTab`, `showTabBar`, `logoutAccount`, `sessionRevision`, and `MINIPROGRAM_VERSION`. Add tree helpers that find nodes by `className` and invoke their `onClick`. Reset the hoisted request/session state in `beforeEach` so tests cannot share an active request.

- [ ] **Step 2: Write the failing deduplication and ordered-phase test**

Render with an authenticated session and a deferred `readMemberProfile` promise. Run the mount effect, invoke the profile `useDidShow` callback, and assert one request. Resolve with `{ member: { username: 'real-account' } }`, flush microtasks, rerender, and assert the diagnostic card contains `账号资料已渲染` and `v1.0.8`.

```ts
expect(h.read).toHaveBeenCalledTimes(1);
expect(textContent(findByClass(render(), 'mp-profile-diagnostic'))).toContain('账号资料已渲染');
expect(textContent(findByClass(render(), 'mp-profile-diagnostic'))).toContain('v1.0.8');
```

- [ ] **Step 3: Write the failing escape and late-response tests**

While the deferred request is unresolved, click `mp-profile-cancel`, assert `Taro.reLaunch({ url: '/pages/bind/index' })`, then resolve the old promise and assert the member hero is still absent. In a separate test, reject the first request, click `mp-profile-retry`, and assert `readMemberProfile` is called a second time.

Also assert that rendered source contains none of the per-profile visual components:

```ts
expect(source).not.toContain('<PageTextureRefraction');
expect(source).not.toContain('<AmbientStarfield');
```

- [ ] **Step 4: Run the page tests and confirm they fail for the missing diagnostic/escape behavior**

Run: `npx vitest run src/pages/profile/index.test.tsx src/pages/profile/profile-iconography.test.ts`

Expected: FAIL because `v1.0.7` has no diagnostic card, cancellation action, or late-response rejection path, and still mounts refraction/starfield elements.

- [ ] **Step 5: Replace the boolean lock with an attempt identity**

Use refs that can be invalidated synchronously:

```ts
const nextAttempt = useRef(0);
const activeAttempt = useRef<number | null>(null);
const mounted = useRef(true);

useEffect(() => () => {
  mounted.current = false;
  activeAttempt.current = null;
  nextAttempt.current += 1;
}, []);
```

At the start of `load`, return when `activeAttempt.current !== null`. Capture `const attempt = ++nextAttempt.current`, `const revision = sessionRevision()`, and `const startedAt = Date.now()`. Set phase to `session-check`, then check `hasAccountSession()`. Missing session sets `failed/session-missing` and navigates to login without starting a request.

Immediately before calling `readMemberProfile`, set `activeAttempt.current = attempt` and phase `request-started`. Every continuation must first verify:

```ts
const current = () => mounted.current
  && activeAttempt.current === attempt
  && sessionRevision() === revision;
```

After resolution, set `response-received`; validate `hasMember(result)`; then store the result and set `rendered`. On rejection set `failed` plus `classifyProfileFailure(cause)`. In `finally`, clear the lock only when `activeAttempt.current === attempt`; this allows retry while preventing an older request from unlocking a newer one.

- [ ] **Step 6: Add safe loading, failure, and rendered diagnostics**

Render one compact `mp-profile-diagnostic` block above page content using only fixed labels:

```tsx
<View className="mp-profile-diagnostic">
  <Text>{`v${MINIPROGRAM_VERSION}`}</Text>
  <Text>{profilePhaseLabel(diagnostic.phase)}</Text>
  <Text>{profileElapsedBucket(diagnostic.elapsedMs)}</Text>
  {diagnostic.failure !== 'none' ? <Text>{failureLabels[diagnostic.failure]}</Text> : null}
</View>
```

Use a closed `failureLabels: Record<ProfileFailureCategory, string>` in the page. Retain the existing user-facing generic error heading, but do not render raw `cause.message`, `errMsg`, headers, request options, or response data in the diagnostic block.

```ts
const failureLabels: Record<ProfileFailureCategory, string> = {
  none: '',
  'session-missing': '未找到可用登录会话',
  'session-expired': '登录状态已失效',
  timeout: '读取超时，请检查网络后重试',
  domain: '当前网络域名配置不可用',
  network: '网络连接失败，请稍后重试',
  'invalid-data': '服务端未返回完整账号资料',
  unknown: '账号资料暂时无法读取',
};
```

The failure card and diagnostic block must both use `failureLabels[diagnostic.failure]`; neither surface may render the caught error's raw message.

- [ ] **Step 7: Add escape controls whose navigation does not wait for logout**

```ts
const cancelAndLogin = () => {
  activeAttempt.current = null;
  nextAttempt.current += 1;
  setLoading(false);
  void logoutAccount().catch(() => undefined);
  void Taro.reLaunch({ url: '/pages/bind/index' });
};
```

During loading show `取消并返回登录`. During failure show both `重新读取` and `返回登录`. Reuse `cancelAndLogin` for the normal “退出登录” button so a failed remote logout cannot block local navigation.

- [ ] **Step 8: Remove profile-only compositor children and apply ordinary dark material**

Remove `PageTextureRefraction` and `AmbientStarfield` imports/usages from `src/pages/profile/index.tsx`. Keep the existing content cards, but add scoped styles such as:

```scss
.mp-profile-page:not(.mp-liquid) { background: #10161b; }
.mp-profile-page:not(.mp-liquid) .mp-card,
.mp-profile-page:not(.mp-liquid) .mp-profile-hero,
.mp-profile-page:not(.mp-liquid) .mp-usage-card,
.mp-profile-page:not(.mp-liquid) .mp-profile-section {
  border: 1rpx solid rgba(255,255,255,.10);
  background: #182128;
  box-shadow: 0 8rpx 24rpx rgba(0,0,0,.18);
}
.mp-profile-diagnostic { display: flex; flex-wrap: wrap; gap: 8rpx 16rpx; margin: 16rpx 0; color: #aebbc3; font-size: 20rpx; }
.mp-profile-actions { display: flex; gap: 12rpx; }
```

Replace `liquid-profile-bottom-space` with a neutral `mp-profile-bottom-space` sized only for the native TabBar and safe area. Keep the “降低透明度” preference under the service section because it still controls the remaining liquid pages.

- [ ] **Step 9: Run focused profile tests**

Run: `npx vitest run src/pages/profile/index.test.tsx src/pages/profile/profile-iconography.test.ts src/pages/profile/profileDiagnostics.test.ts`

Expected: PASS, including one request for mount + `useDidShow`, retry after failure, immediate login escape, and ignored late completion.

- [ ] **Step 10: Commit only the profile behavior**

```bash
git add src/pages/profile/index.tsx src/pages/profile/index.test.tsx src/pages/profile/profileDiagnostics.ts src/pages/profile/profileDiagnostics.test.ts src/app.scss
git commit -m "fix: keep profile diagnostics responsive"
```

If Task 2 was committed separately, omit its two files from this command.

### Task 4: Bump The Observable Release Identity

**Files:**
- Modify: `src/config/release.ts`
- Modify: `src/components/LoginScene.test.tsx`

**Interfaces:**
- Consumes: `MINIPROGRAM_VERSION` rendered by `LoginScene` and the profile diagnostic block.
- Produces: both surfaces render exactly `v1.0.8` from one constant.

- [ ] **Step 1: Update the login test first**

```ts
expect(version.props.children).toBe('v1.0.8');
```

- [ ] **Step 2: Run the version test and confirm it fails**

Run: `npx vitest run src/components/LoginScene.test.tsx`

Expected: FAIL with received value `v1.0.7`.

- [ ] **Step 3: Change the single release constant**

```ts
export const MINIPROGRAM_VERSION = '1.0.8';
```

- [ ] **Step 4: Run version and profile tests**

Run: `npx vitest run src/components/LoginScene.test.tsx src/pages/profile/index.test.tsx`

Expected: PASS and both surfaces derive `v1.0.8` from `src/config/release.ts`.

- [ ] **Step 5: Commit the release identity**

```bash
git add src/config/release.ts src/components/LoginScene.test.tsx
git commit -m "chore: mark profile isolation build 1.0.8"
```

### Task 5: Full Regression, Build, Upload, And True-Device Acceptance

**Files:**
- Verify: all scoped source and test files from Tasks 1–4
- Verify generated output: `dist/app.js`, `dist/pages/bind/index.js`, `dist/pages/profile/index.js`
- Do not commit generated output unless repository policy already tracks it.

**Interfaces:**
- Consumes: the completed `v1.0.8` source tree and AppID `wxd72c311f3711eda9` from `project.config.json`.
- Produces: tested production `dist/`, a Developer Tools upload receipt, and true-device evidence for interaction plus the last diagnostic phase.

- [ ] **Step 1: Review the exact scoped diff before running broad checks**

Run:

```bash
git diff -- src/ui/liquidGlass.ts src/ui/liquidGlass.test.ts src/theme.tsx src/theme.test.ts src/pages/profile/profileDiagnostics.ts src/pages/profile/profileDiagnostics.test.ts src/pages/profile/index.tsx src/pages/profile/index.test.tsx src/app.scss src/config/release.ts src/components/LoginScene.test.tsx
```

Expected: no API-origin change, no session credential output, no unrelated page visual change, and no secret file in the diff.

- [ ] **Step 2: Run the full automated suite**

Run: `npm test`

Expected: all Vitest files pass. Record the actual file/test totals; do not reuse the previous `85/403` count.

- [ ] **Step 3: Run TypeScript without emitting files**

Run: `npx tsc --noEmit`

Expected: exit code 0.

- [ ] **Step 4: Check patch hygiene**

Run: `git diff --check`

Expected: no whitespace errors.

- [ ] **Step 5: Build the production WeChat bundle**

Run: `npm run build:weapp`

Expected: exit code 0 and updated `dist/` output.

- [ ] **Step 6: Verify release identity and compositor exclusion in generated output**

Run:

```bash
rg -n "1\.0\.8|账号资料已渲染|已发起资料请求|取消并返回登录" dist/pages/bind/index.js dist/pages/profile/index.js
rg -n "PageTextureRefraction|AmbientStarfield|liquid-navigation" dist/pages/profile/index.js
```

Expected: the first command finds the version/diagnostic copy; the second command has no profile-page matches. Also inspect `project.config.json` without printing private settings and confirm `miniprogramRoot` resolves to `dist/` and `appid` is `wxd72c311f3711eda9`.

- [ ] **Step 7: Inspect repository state before any final commit or upload**

Run: `git status --short`

Expected: unrelated pre-existing modifications and untracked files remain untouched. Confirm `.env.production` is not staged with `git diff --cached --name-only`.

- [ ] **Step 8: Compile and upload the exact `dist/` project in WeChat Developer Tools**

Open the project whose `projectpath` points to `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist` and whose AppID is `wxd72c311f3711eda9`. Compile once, verify the login screen reads `v1.0.8`, then upload version `1.0.8` with a note such as `我的页冻结隔离诊断：移除液态合成并恢复原生导航`.

Expected: Developer Tools reports a successful upload for the exact AppID and version. This is a publication receipt, not yet a fix verdict.

- [ ] **Step 9: Perform true-device acceptance in order**

On the experience build, refresh/reopen until the login screen visibly reads `v1.0.8`, then:

1. Log in with the existing public account.
2. Open “我的” and capture the diagnostic phase shown.
3. Tap “工作区”, “任务”, and “功能”; each must switch immediately.
4. Return to “我的” and tap “退出登录”; it must reach the login page immediately even if remote logout fails.
5. Log in again, disable network before opening “我的”, wait at least 15 seconds, and verify `读取失败` plus retry/login controls remain tappable.
6. Restore network, tap “重新读取”, and verify the phase reaches `账号资料已渲染`.

Expected: if all steps pass, the `1.0.7` freeze is isolated to the old profile liquid-composition path. If the page still freezes, record the last visible phase and collect true-device Console/Network evidence; do not continue changing visuals by guesswork.

- [ ] **Step 10: Record the acceptance boundary**

Document the uploaded version, upload receipt time, device/WeChat version, last diagnostic phase, navigation result, logout result, timeout/retry result, and whether the issue is confirmed fixed. Do not record credentials or request contents.

## Plan Self-Review

- Every spec requirement is assigned to a task: material isolation and native TabBar (Task 1), safe phases/categories (Task 2), deduplication/timeout escape/retry/late-response protection (Task 3), `v1.0.8` identity (Task 4), and build/upload/true-device acceptance (Task 5).
- The plan introduces no new dependency, server contract, storage key, credential logging, global material rollback, or unrelated visual redesign.
- Every production change begins with a failing focused test and names the expected failure and passing command.
- Types and ownership are explicit: diagnostics are pure, profile request identity stays page-local, and the existing service retains the 15-second deadline.
- No placeholder steps remain; commands, expected outcomes, exact files, public interfaces, and acceptance evidence are specified.
- Commit commands enumerate scoped files and explicitly exclude the dirty worktree’s unrelated changes and `.env.production`.

