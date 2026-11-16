# Real Server Data Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align the four mini-program primary tabs with the web information hierarchy while rendering only real server-backed account, task, permission, and state data.

**Architecture:** Keep `Taro.request`, WeChat session binding, and the existing HTTPS API origin. Add a pure task-summary projection service that independently loads four existing task sources and reports per-source success or failure. Primary pages consume these stable view models and display loading, content, empty, configuration, and retry states without filling in missing business facts.

**Tech Stack:** Taro 4, React 18, TypeScript, Vitest, WeChat Mini Program.

## Global Constraints

- Modify only `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram`; do not change `/Users/ming/Downloads/小程序开发/yizhanchengming/server-main`.
- Retain `Taro.request`, existing business-token storage, WeChat binding routes, and the existing API paths.
- The private API origin must remain private and HTTPS. Do not inspect, print, or commit `.env.development`, `project.private.config.json`, tokens, passwords, WeChat codes, or AppSecret values.
- `127.0.0.1:5176` is a visual reference only. The Mini Program may call only the compiled `TARO_APP_API_ORIGIN` HTTPS source.
- Missing, unrecognized, unauthorized, or failed server data must render as such; it must not become a fake success, default account, default time, default quota, or 100% progress.
- This standalone Mini Program directory has no `.git`; do not use the adjacent dirty Git repository to commit these changes. After each task, record the changed paths and run the specified verification command.

---

## File Structure

- `src/config/runtime.ts`: exposes a typed guard for the deliberately invalid default API origin.
- `src/config/runtime.test.ts`: proves the origin guard distinguishes a configured HTTPS build from the safe placeholder.
- `src/services/api.ts`: rejects ordinary API requests before a request is attempted when the build has no server origin.
- `src/services/api.test.ts`: proves an unconfigured origin produces the actionable error instead of a simulated empty result.
- `src/services/session.ts`: applies the same origin guard to WeChat session and account binding calls.
- `src/services/session.test.ts`: proves WeChat login cannot submit a code to the placeholder origin.
- `src/services/taskSummary.ts`: pure, typed projection and independent loader for the four existing task sources.
- `src/services/taskSummary.test.ts`: exercises real response-shaped fixtures, unknown statuses, history video progress, sorting, and partial failure behavior.
- `src/services/taskPageState.ts`: derives the task page's configuration, initial-load, all-failed, empty, and partial-content states from a `TaskSummary`.
- `src/services/taskPageState.test.ts`: protects the distinction between a source-local error and a whole-page empty/error state.
- `src/pages/tasks/index.tsx`: renders the task-summary view model with per-source errors and retry.
- `src/config/profileData.ts`: converts member-center responses into optional facts without inventing zero usage, permissions, or quota.
- `src/config/profileData.test.ts`: protects against regressions that turn absent server fields into hard-coded account facts.
- `src/config/profile.ts` and `src/config/profile.test.ts`: remove client-derived creator levels and retain only menus with a verified data source or explicit local-preference purpose.
- `src/pages/profile/index.tsx` and `src/pages/profile-detail/index.tsx`: render the real member-center model, server-supported details, and clear unavailable states for unsupported details.
- `src/pages/workspace/index.tsx`: reads the most recent real history entry for “继续工作” and labels device-only navigation preference accurately.
- `src/pages/functions/index.tsx`: uses the web-like one-row function hierarchy and does not claim static module definitions are server facts.
- `src/app.scss`: adjusts the task, profile, workspace, and function row states without changing global navigation or secondary workbenches.

## Task 1: Reject an unconfigured API origin before user data requests

**Files:**
- Modify: `src/config/runtime.ts`
- Modify: `src/config/runtime.test.ts`
- Modify: `src/services/api.ts`
- Modify: `src/services/api.test.ts`
- Modify: `src/services/session.ts`
- Modify: `src/services/session.test.ts`

**Interfaces:**
- Produces `isApiOriginConfigured(): boolean` and `requireApiOrigin(): void` from `src/config/runtime.ts`.
- `requireApiOrigin()` throws `Error("服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建")` only for `https://api.example.invalid`.
- `apiRequest()` and the session module's private `postSession()` call the guard before `Taro.request`.

- [ ] **Step 1: Write failing tests for the observable configuration boundary**

Add a runtime test that imports a fresh module with no injected `__MINIPROGRAM_API_ORIGIN__`, calls `requireApiOrigin()`, and expects the exact actionable error. Add an API test that verifies the rejection occurs before the mocked `Taro.request` receives a call. Add a session test that calls `bootstrapWechatSession()` with a login code and verifies its failure is the same configuration error.

```ts
await expect(apiRequest("/api/member/me")).rejects.toThrow(
  "服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建",
);
expect(taro.request).not.toHaveBeenCalled();
```

For the existing successful API/session tests, mock `../config/runtime` with a configured `apiUrl()` and a no-op `requireApiOrigin()` so their existing HTTP-envelope coverage stays focused on request behavior.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
npm test -- src/config/runtime.test.ts src/services/api.test.ts src/services/session.test.ts
```

Expected: new tests fail because `requireApiOrigin` does not exist and the placeholder build still calls `Taro.request`.

- [ ] **Step 3: Implement the smallest origin guard**

In `runtime.ts`, export a single placeholder constant and derive the guard from `API_ORIGIN`:

```ts
export const PLACEHOLDER_API_ORIGIN = "https://api.example.invalid";

export function isApiOriginConfigured() {
  return API_ORIGIN !== PLACEHOLDER_API_ORIGIN;
}

export function requireApiOrigin() {
  if (!isApiOriginConfigured()) {
    throw new Error("服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建");
  }
}
```

Call this guard as the first statement in `apiRequest()` and `postSession()`. Do not change headers, token storage, request methods, binding payloads, or the configured-origin behavior.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run:

```bash
npm test -- src/config/runtime.test.ts src/services/api.test.ts src/services/session.test.ts
```

Expected: all three files pass; normal configured-origin tests still verify the Authorization header and 401 cleanup.

- [ ] **Step 5: Record task verification**

Record the six changed paths and the passing command in the implementation summary. Do not create a Git commit in either directory.

## Task 2: Add a pure four-source task-summary projection

**Files:**
- Create: `src/services/taskSummary.ts`
- Create: `src/services/taskSummary.test.ts`

**Interfaces:**
- Consumes a request function compatible with `apiRequest<T>(path)`.
- Produces `loadTaskSummary(request): Promise<TaskSummary>`.
- `TaskSummary` contains `entries: AggregatedTask[]` and four `TaskSourceResult` values, each with `state: "ready" | "error"` and an optional error message.
- `AggregatedTask` contains `source`, `sourceLabel`, optional `id`, optional `title`, optional `createdAt`, `status: "running" | "failed" | "completed" | "unknown"`, optional `rawStatus`, and optional `{ completed, total }` progress.
- Produces `filterTaskEntries(entries, filter)` where `filter` is `"全部" | "进行中" | "失败" | "已完成"`.

- [ ] **Step 1: Write failing tests for projection behavior**

Create four complete response-shaped fixtures: a history response with `entries` and `videoTasks`, an Agent response with `tasks`, a novel-fetch response with `tasks`, and a batch response with `batches`. Write independent assertions for:

```ts
expect(summary.entries.map((entry) => [entry.source, entry.title, entry.status])).toEqual([
  ["script", "第 2 集", "running"],
  ["agent", "修订对白", "completed"],
  ["fetch", "原著 A", "failed"],
  ["batch", "漫剧批次", "unknown"],
]);

expect(summary.sources.find((source) => source.source === "batch")).toMatchObject({
  state: "ready",
});
```

Add a partial-failure test where only `/api/agent/tasks` rejects. Assert the other three sources remain `ready`, the Agent source is `error`, and the three successful entries remain. Add a test that an absent status is `unknown`, has no invented title/time/progress, and does not appear in the completed filter.

- [ ] **Step 2: Run the new test file and verify RED**

Run:

```bash
npm test -- src/services/taskSummary.test.ts
```

Expected: fail because `taskSummary.ts` does not exist.

- [ ] **Step 3: Implement the projection with source-local normalization**

Define the source table once:

```ts
const TASK_SOURCE_DEFINITIONS = [
  { source: "script", sourceLabel: "剧本", path: "/api/history" },
  { source: "agent", sourceLabel: "Agent", path: "/api/agent/tasks" },
  { source: "fetch", sourceLabel: "小说获取", path: "/api/novel-fetch-workshop/tasks" },
  { source: "batch", sourceLabel: "批量工厂", path: "/api/batch-factory/v12/batches" },
] as const;
```

Use `Promise.allSettled` over this table. Extract only real fields (`id`, `title`, `name`, `bookName`, `preview`, `createdAt`, `created_at`, `updatedAt`, `updated_at`, `status`, `aiStatus`, `ai_status`, `originalStatus`, `original_status`). Preserve missing display fields as `undefined`.

Map explicit completed values (`done`, `completed`, `success`, `succeeded`, `finished`, `已完成`, `完成`, `成功`, `已合并`), explicit failure values (`failed`, `error`, `canceled`, `cancelled`, `失败`, `异常`), and explicit in-progress values (`queued`, `pending`, `running`, `processing`, `executing`, `waiting`, `进行中`, `处理中`, `排队中`, `等待中`). Any other value is `unknown`.

For history, compute a progress object only when `videoTasks` is an object with at least one task. Map each video's explicit status with the same rules; derive history state from failed video tasks first, then running video tasks, then completed video tasks. If neither `status` nor video tasks express a known state, preserve `unknown`.

Sort entries by a real parseable timestamp descending; keep original source-table order and response order for ties or absent timestamps. `filterTaskEntries` includes unknown entries only under `全部`.

- [ ] **Step 4: Run the task-summary tests and verify GREEN**

Run:

```bash
npm test -- src/services/taskSummary.test.ts
```

Expected: all cases pass, including partial failure and unknown-state preservation.

- [ ] **Step 5: Record task verification**

Record the two new files and passing focused command. Do not commit.

## Task 3: Render task aggregation with source-local errors and retry

**Files:**
- Create: `src/services/taskPageState.ts`
- Create: `src/services/taskPageState.test.ts`
- Modify: `src/pages/tasks/index.tsx`
- Modify: `src/app.scss`

**Interfaces:**
- Consumes `loadTaskSummary(apiRequest)` and `filterTaskEntries()` from `src/services/taskSummary.ts`.
- The page keeps `TaskSummary | null`, `loading`, `refreshing`, and selected filter state.
- Source errors call the same refresh function; no page-level request code parses endpoint payloads.

- [ ] **Step 1: Add a failing page-adjacent behavior test**

Create `src/services/taskPageState.test.ts` and first add a tiny pure `taskPageState.ts` reducer/selector if needed. Test the user-visible states rather than source text:

```ts
expect(deriveTaskPageState({ loading: false, summary: null, configurationError: "服务端地址未配置" }))
  .toEqual({ kind: "configuration-error", message: "服务端地址未配置" });

expect(deriveTaskPageState({ loading: false, summary: partialSummary, configurationError: "" }))
  .toMatchObject({ kind: "content", failedSourceCount: 1 });
```

The mutation this protects is replacing a source-local failure with a global empty/error page.

- [ ] **Step 2: Run the new page-state test and verify RED**

Run:

```bash
npm test -- src/services/taskPageState.test.ts
```

Expected: fail because the selector does not exist.

- [ ] **Step 3: Implement the selector and update the page**

Implement the smallest `deriveTaskPageState()` that distinguishes configuration error, first-load skeleton, all-source failure, real empty result, and content with zero or more failed sources.

Replace the page-local `Promise.all` and `listFrom` helper with `loadTaskSummary(apiRequest)`. Render:

- A compact web-like title/refresh row instead of the static metrics hero.
- The existing four filters, where only known statuses enter the non-`全部` filters.
- Source label, real title or `服务端未返回标题`, real timestamp when supplied, exact status label, and progress only when the projection provides it.
- A source-local error row with its source label and a retry button when that source fails.
- A full-page retry only when all four sources fail.

Add focused styles for a compact task heading, progress row, unknown status pill, source error row, and disabled/loading refresh button. Remove styles used only by the fabricated task count metrics; leave the global tab bar unchanged.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
npm test -- src/services/taskSummary.test.ts src/services/taskPageState.test.ts
```

Expected: both files pass, and no task entry with a missing status becomes completed.

- [ ] **Step 5: Record task verification**

Record changed paths and the passing focused command. Do not commit.

## Task 4: Project member-center data without defaults or derived levels

**Files:**
- Modify: `src/config/profileData.ts`
- Modify: `src/config/profileData.test.ts`
- Modify: `src/config/profile.ts`
- Modify: `src/config/profile.test.ts`
- Modify: `src/pages/profile/index.tsx`
- Modify: `src/pages/profile-detail/index.tsx`
- Modify: `src/app.scss`

**Interfaces:**
- `profileFacts(center)` returns only optional real facts: display name, username, role, team name, `apiEnabled`, scopes, monthly token usage, and monthly limit.
- `monthlyTokenUsage(center)` returns `number | undefined`; it must not turn a missing field into zero.
- `PROFILE_SECTIONS` contains only member-center-backed routes, model-backed API configuration, and explicitly local mobile-preference routes.

- [ ] **Step 1: Write failing tests for missing member-center fields**

Replace the existing level test with behavior tests:

```ts
const facts = profileFacts({ member: { role: "member" } });
expect(facts.displayName).toBeUndefined();
expect(facts.monthlyTokenUsage).toBeUndefined();
expect(facts.monthlyTokenLimit).toBeUndefined();
expect(facts.apiEnabled).toBeUndefined();
```

Add a populated fixture and assert literal values from `member`, `team`, and `usage.month`. Add menu assertions that `账号安全`, `联合治理`, `组员管理`, `账号与角色`, and `管理后台` are not presented as supported detail routes without verified endpoints. Preserve `个人资料`, `会员中心`, `用量与制作`, `API 配置`, `主题与外观`, and `通用设置`.

- [ ] **Step 2: Run profile-focused tests and verify RED**

Run:

```bash
npm test -- src/config/profileData.test.ts src/config/profile.test.ts
```

Expected: fail because the current normalizer invents zero usage and role-derived API authorization, and the current menu includes unsupported claims.

- [ ] **Step 3: Implement real-fact profile rendering**

Refactor `profileData.ts` to preserve missing fields as `undefined`; do not infer `apiEnabled` from role. Remove `getProfileLevel()` and its token-threshold table from `profile.ts`.

In the primary profile page:

- Add initial loading and retry states around `/api/member/me`.
- Do not render a profile hero until the server returns a member object.
- Use only a real display name or username; when neither is returned, show `服务端未返回名称` as missing-data text, not a user identity.
- Replace “创作等级” with a same-footprint “本月用量” card. It shows actual monthly Tokens and quota when supplied, otherwise `服务端未返回`.
- Keep avatar upload only after a member is present; preserve its real success/error responses.

In `profile-detail`, render actual member-center/model responses for the supported routes. For an unsupported route reached through a saved/deep link, show `该功能尚无已验证的小程序服务端接口` and no security, permission, team, or admin fact. Label theme/general settings as local device preferences.

Update styles for the usage card and missing-data text, retaining the web-derived density and the existing safe-area behavior.

- [ ] **Step 4: Run profile-focused tests and verify GREEN**

Run:

```bash
npm test -- src/config/profileData.test.ts src/config/profile.test.ts
```

Expected: populated data remains visible, missing facts remain missing, no client-created creator level exists, and unsupported routes are absent from the primary menu.

- [ ] **Step 5: Record task verification**

Record changed paths and focused test output. Do not commit.

## Task 5: Align work area and functions while keeping navigation facts honest

**Files:**
- Modify: `src/pages/workspace/index.tsx`
- Modify: `src/pages/functions/index.tsx`
- Modify: `src/services/normalizers.ts`
- Modify: `src/services/normalizers.test.ts`
- Modify: `src/app.scss`

**Interfaces:**
- `latestHistoryEntry(payload)` returns the first real, valid history entry or `undefined`; it never creates a title.
- Workspace uses `apiRequest("/api/history")` only after its authenticated session gate is ready.
- Function page static module records are labeled navigation entries; a selected module is “本机上次打开” only.

- [ ] **Step 1: Write failing tests for real history selection**

Add tests to `normalizers.test.ts`:

```ts
expect(latestHistoryEntry({ entries: [{ id: "h2", preview: "第 2 集" }] }))
  .toMatchObject({ id: "h2", preview: "第 2 集" });
expect(latestHistoryEntry({ entries: [] })).toBeUndefined();
expect(latestHistoryEntry({ entries: [null, "bad"] })).toBeUndefined();
```

The mutation this protects is returning a fabricated default module or title when history is empty or malformed.

- [ ] **Step 2: Run the normalizer test and verify RED**

Run:

```bash
npm test -- src/services/normalizers.test.ts
```

Expected: fail because `latestHistoryEntry` is not implemented.

- [ ] **Step 3: Implement selection and page behavior**

Add `latestHistoryEntry()` using the existing `entriesFromHistory()` type guard. In `workspace/index.tsx`, request history only when the session gate resolves to ready. Render the latest server entry in “继续工作” when it exists, a specific empty state when it does not, and an inline retryable history error when the request fails. Keep `ACTIVE_MODULE_KEY` only as a local navigation preference, change its label to `本机上次打开`, and never call it server-synced.

In `functions/index.tsx`, replace the category tile grid with the web-reference hierarchy: one compact row per module, label, description, chevron, and a `本机上次打开` marker for the local preference. Do not claim an entry is enabled by the server until the destination request actually succeeds; destination 403/error remains its real destination state.

Update only relevant workspace/function CSS. Keep fixed dimensions for rows and ensure the four native tabs remain reachable above the safe area.

- [ ] **Step 4: Run normalizer tests and verify GREEN**

Run:

```bash
npm test -- src/services/normalizers.test.ts
```

Expected: latest history is real-or-absent, never a generated module or title.

- [ ] **Step 5: Record task verification**

Record changed paths and the passing focused command. Do not commit.

## Task 6: Type-check, build, and perform signed-in Mini Program acceptance

**Files:**
- Modify: `docs/superpowers/specs/2026-10-13-real-server-data-alignment-design.md` only if implementation reveals a contract conflict; otherwise no documentation change.

**Interfaces:**
- Consumes the finished primary pages and existing private developer configuration.
- Produces verification evidence without logging credentials, tokens, raw profile data, or private API-origin values.

- [ ] **Step 1: Run the full automated suite**

Run:

```bash
npx tsc --noEmit
npm test
npm run build:weapp
```

Expected: TypeScript, all Vitest tests, and Taro WeChat build pass.

- [ ] **Step 2: Inspect generated output without reading private configuration**

Run:

```bash
test -f dist/project.config.json
find dist -type f | wc -l
```

Expected: the build output has a project configuration and a nonzero file count. Do not print generated bundles because they may embed the private API origin.

- [ ] **Step 3: Validate behavior in WeChat Developer Tools**

Import/rebuild the existing `dist/` target, use the already logged-in test account, and verify each primary tab:

- Work area: real recent history, real no-history state, and history retry state.
- Tasks: all four sources, source-local failure, unknown status, filters, and refresh.
- Functions: row hierarchy and truthful local-preference marker.
- Profile: real account facts, missing-data rendering, quota/use card, unsupported route state, upload success/failure, and logout.
- Session: 401 returns to account binding; page safe areas remain usable.

Record only pass/fail observations and API path/status classes; do not include private values.

- [ ] **Step 4: Record final status**

State separately: automated test/build result, signed-in Developer Tools result, data sources verified, and any remaining external configuration or login blocker. Do not claim public release or true-device acceptance from a local build alone.

