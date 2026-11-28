# Account Switch, Logout, and Scroll Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make logout permanently visible, make long personal-center navigation and content scroll correctly, and replace free-text account switching with a server-authorized searchable account dropdown.

**Architecture:** The Go account-impersonation service exposes a read-only target list derived from the same `accountcenter.Policy` checks used when a switch starts. A focused React switch component fetches that list, searches locally over the authorized results, confirms the selected target, and reuses the existing start/stop switch APIs. CSS keeps the sidebar profile and action footer fixed while only the middle navigation and right-hand content scroll.

**Tech Stack:** Go 1.26 HTTP API, React, Ant Design, Node test runner, Go `httptest`, CSS Grid/Flexbox, Go embed release build.

## Global Constraints

- Work directly on the maintained `v88` branch; do not create a feature branch.
- Preserve unrelated dirty files and stage only files listed in each task.
- The backend is the only authority for eligible switch targets; the browser must never construct permissions itself.
- “退出切换” returns to the actor account; “退出登录” ends the whole login session.
- The switch dropdown must show authorized accounts immediately and also support search by display name or username.
- The dropdown must not provide a free-text fallback for arbitrary usernames.
- The sidebar footer stays visible without scrolling; only the middle navigation scrolls.
- The right-hand personal-center content scrolls independently to its final control.
- Test first, observe the expected failure, then implement the smallest passing change.
- Rebuild React and the Go embedded binary; verify the exact Git SHA locally before any push or production deployment.

---

## File Map

- `backend/internal/httpapi/account_impersonation.go`: register and serve the authorized target-list endpoint.
- `backend/internal/httpapi/account_impersonation_targets_test.go`: exercise the four-role target matrix and target filtering.
- `frontend/src/shared/api/accountImpersonation.js`: expose `getAccountImpersonationTargets`.
- `frontend/src/shared/layouts/AccountSwitchMenu.jsx`: own dropdown loading, searching, selection, confirmation, retry, and start-switch behavior.
- `frontend/src/shared/layouts/AccountCenterLayout.jsx`: compose the switch menu and keep logout/exit-switch actions distinct.
- `frontend/src/shared/layouts/accountImpersonation.test.js`: test the API contract and rendered dropdown states.
- `frontend/src/shared/layouts/AccountCenterLayout.source.test.js`: lock the three-zone sidebar and independent scroll contract.
- `frontend/src/shared/styles/account-center-pixel-parity.css`: implement fixed sidebar header/footer and scrollable navigation/main content.
- `docs/acceptance/personal-center-20261128/design-qa.md`: record exact SHA, tests, build, viewport checks, and screenshots.

---

### Task 1: Server-authorized switch targets

**Files:**
- Create: `backend/internal/httpapi/account_impersonation_targets_test.go`
- Modify: `backend/internal/httpapi/account_impersonation.go`

**Interfaces:**
- Consumes: `accountCenterAdmin.withDirectory`, `adminSnapshot.directory()`, and row action `switch_account`.
- Produces: `GET /api/account-impersonation/targets` returning `{ groups: [{ teamId, teamName, accounts: [{ username, displayName, role, avatarUrl }] }] }`.

- [ ] **Step 1: Write the failing endpoint tests**

Create table-driven tests that build `directoryTestStore` fixtures and assert exact usernames per role:

```go
func TestImpersonationTargetsRespectActorScope(t *testing.T) {
    tests := []struct {
        actor string
        want  []string
    }{
        {"founder", []string{"developer", "manager-a", "member-a", "manager-b", "member-b"}},
        {"developer", []string{"manager-a", "member-a"}},
        {"manager-a", []string{"member-a"}},
    }
    // Request GET /api/account-impersonation/targets and flatten group accounts.
    // Compare sorted usernames with want.
}
```

Add separate assertions that the actor, disabled, archived, cross-team, and unauthorized developer targets are absent; a member receives `403`; and an active impersonation cannot read a new target list. An active account whose old membership ended remains available to the founder as an ungrouped account, but not to the old team manager or developer.

- [ ] **Step 2: Run the tests and verify the endpoint is missing**

Run:

```powershell
go test ./internal/httpapi -run 'TestImpersonationTargets' -count=1
```

Expected: FAIL because `GET /api/account-impersonation/targets` is not registered and returns `404`.

- [ ] **Step 3: Implement the minimal target endpoint**

Add response types and register the new route with the same session-only account-center middleware:

```go
type impersonationTarget struct {
    Username, DisplayName, Role, AvatarURL string
}
type impersonationTargetGroup struct {
    TeamID, TeamName string
    Accounts         []impersonationTarget
}

func (s accountImpersonation) targets(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    admin := accountCenterAdmin{auth: s.auth}
    admin.withDirectory(w, r, false, func(_ accountcenter.AdminTx, snapshot adminSnapshot) (any, error) {
        directory := snapshot.directory()
        groups := make([]impersonationTargetGroup, 0, len(directory.Teams)+1)
        // Keep only active, non-historical rows whose server-generated Actions
        // contain "switch_account". Preserve directory team grouping.
        return map[string]any{"groups": groups}, nil
    })
}
```

Use `slices.Contains(row.Actions, "switch_account")`; never duplicate the role matrix in this handler. Add the ungrouped group only when at least one authorized target exists. Sort accounts by display name then username for stable UI output.

- [ ] **Step 4: Run focused and neighboring backend tests**

Run:

```powershell
go test ./internal/httpapi -run 'TestImpersonation(Targets|Permissions|Status)' -count=1
go test ./internal/accountcenter -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the backend contract**

```powershell
git add -- backend/internal/httpapi/account_impersonation.go backend/internal/httpapi/account_impersonation_targets_test.go
git commit -m "feat(profile): list authorized account switch targets"
```

---

### Task 2: Searchable populated account dropdown

**Files:**
- Create: `frontend/src/shared/layouts/AccountSwitchMenu.jsx`
- Modify: `frontend/src/shared/api/accountImpersonation.js`
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- Modify: `frontend/src/shared/layouts/accountImpersonation.test.js`

**Interfaces:**
- Consumes: `GET /api/account-impersonation/targets`, `startAccountImpersonation(username)`, and `reloadForAccountSwitch()`.
- Produces: `AccountSwitchMenu({ status })`, visible only when `status.canSwitch && !status.active`.

- [ ] **Step 1: Write failing API and component tests**

Extend the API request test to expect:

```js
const targets = { groups: [{ teamId: 'one', teamName: '一战晟铭', accounts: [{ username: 'member-a', displayName: '组员甲', role: 'member' }] }] };
assert.deepEqual(await getAccountImpersonationTargets(request), targets);
assert.deepEqual(requests[1].slice(0, 2), ['/api/account-impersonation/targets', { credentials: 'same-origin', method: 'GET', cache: 'no-store', suppressGlobalError: true }]);
```

Add component tests using the existing esbuild loader and React server rendering/source assertions to require all of these behaviors:

```js
assert.match(source, /getAccountImpersonationTargets/);
assert.match(source, /placeholder="搜索组员名称或用户名"/);
assert.match(source, /displayName.*username|username.*displayName/s);
assert.match(source, /确认切换/);
assert.match(source, /无法读取可切换账号/);
assert.doesNotMatch(source, /输入目标账号用户名/);
```

- [ ] **Step 2: Run the frontend tests and verify the new contract is absent**

Run:

```powershell
node --test src/shared/layouts/accountImpersonation.test.js
```

Expected: FAIL because the target API and `AccountSwitchMenu` do not exist and the old free-text input is still present.

- [ ] **Step 3: Add the target API helper**

Add:

```js
export function getAccountImpersonationTargets(request = apiRequest) {
  return request(`${path}/targets`, { ...options, method: 'GET', cache: 'no-store' });
}
```

- [ ] **Step 4: Implement `AccountSwitchMenu`**

The component must:

1. fetch once when the dropdown opens and refetch on retry;
2. immediately render returned groups and account buttons;
3. filter with `normalize('NFKC').trim().toLocaleLowerCase()` over both display name and username;
4. keep selection separate from confirmation;
5. show loading, empty, error/retry, and busy states;
6. call `startAccountImpersonation(selected.username)` only after confirmation;
7. call `reloadForAccountSwitch()` only after a successful response.

Use Ant Design `Dropdown`, `Input`, `Avatar`, `Button`, `Spin`, and `Alert`; render a custom dropdown panel so search and grouped account rows remain inside one popup.

- [ ] **Step 5: Replace the old manual-input entry**

Remove the local `AccountSwitchEntry` implementation from `AccountCenterLayout.jsx`, import `AccountSwitchMenu`, and render:

```jsx
<AccountSwitchMenu status={impersonation} />
```

Keep `AccountImpersonationBanner` and its “退出切换” action unchanged.

- [ ] **Step 6: Run focused frontend tests**

Run:

```powershell
node --test src/shared/layouts/accountImpersonation.test.js src/shared/layouts/AccountCenterLayout.source.test.js
```

Expected: PASS.

- [ ] **Step 7: Commit the dropdown**

```powershell
git add -- frontend/src/shared/api/accountImpersonation.js frontend/src/shared/layouts/AccountSwitchMenu.jsx frontend/src/shared/layouts/AccountCenterLayout.jsx frontend/src/shared/layouts/accountImpersonation.test.js
git commit -m "feat(profile): add searchable member account switcher"
```

---

### Task 3: Fixed logout footer and independent scrolling

**Files:**
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.source.test.js`
- Modify: `frontend/src/shared/styles/account-center-pixel-parity.css`

**Interfaces:**
- Consumes: existing `.pc-sidebar-profile`, `.pc-sidebar-nav`, `.pc-sidebar-actions`, and `.pc-main` elements.
- Produces: a fixed header/footer sidebar with only `.pc-sidebar-nav` scrolling, plus an independently scrolling `.pc-main`.

- [ ] **Step 1: Write failing layout contract assertions**

Add exact source/CSS assertions:

```js
assert.match(source, /className="pc-sidebar-actions"[\s\S]*退出登录/);
assert.match(css, /\.pc-sidebar\s*\{[^}]*overflow:\s*hidden/s);
assert.match(css, /\.pc-sidebar-nav\s*\{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*auto/s);
assert.match(css, /\.pc-sidebar-actions\s*\{[^}]*flex:\s*0 0 auto/s);
assert.match(css, /\.pc-main\s*\{[^}]*overflow-y:\s*auto/s);
```

- [ ] **Step 2: Run the layout test and verify it fails on missing three-zone rules**

Run:

```powershell
node --test src/shared/layouts/AccountCenterLayout.source.test.js
```

Expected: FAIL because `.pc-sidebar-nav` is currently grid-sized without the explicit flex growth/vertical overflow contract and the sidebar does not clip its own three-zone layout.

- [ ] **Step 3: Implement the minimal CSS layout fix**

Update the authoritative pixel-parity stylesheet:

```css
.pc-sidebar { overflow: hidden; }
.pc-sidebar-profile { flex: 0 0 auto; }
.pc-sidebar-nav { flex: 1 1 auto; min-height: 0; overflow-x: hidden; overflow-y: auto; overscroll-behavior: contain; }
.pc-sidebar-actions { flex: 0 0 auto; margin-top: 0; }
.pc-main { min-height: 0; overflow-x: hidden; overflow-y: auto; }
```

Add `scrollbar-width: thin` and a small right padding to navigation without shrinking click targets. Keep the existing mobile icon layout; ensure mobile `.pc-sidebar-actions button` remains at least 42px high and has a `title` or accessible label supplied by the existing JSX.

- [ ] **Step 4: Run layout and personal-center visual tests**

Run:

```powershell
node --test src/shared/layouts/AccountCenterLayout.source.test.js src/user/components/personal-center-visuals.source.test.js src/user/pages/personal-center-layout.source.test.js
```

Expected: PASS.

- [ ] **Step 5: Commit the scroll and logout visibility fix**

```powershell
git add -- frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/shared/styles/account-center-pixel-parity.css
git commit -m "fix(profile): keep logout visible and restore scrolling"
```

---

### Task 4: Build, runtime, and browser acceptance

**Files:**
- Modify: `docs/acceptance/personal-center-20261128/design-qa.md`
- Create: `docs/acceptance/personal-center-20261128/screenshots/account-switch-dropdown.png`
- Create: `docs/acceptance/personal-center-20261128/screenshots/account-switch-search.png`
- Create: `docs/acceptance/personal-center-20261128/screenshots/account-switched-exit-controls.png`
- Create: `docs/acceptance/personal-center-20261128/screenshots/account-center-low-height-scroll.png`

**Interfaces:**
- Consumes: the committed backend endpoint, React dropdown, CSS layout, and Go embed build scripts.
- Produces: exact-SHA local runtime and visual acceptance evidence ready for the later release gate.

- [ ] **Step 1: Run focused backend and frontend tests**

Run:

```powershell
go test ./internal/httpapi -run 'TestImpersonation' -count=1
npm --prefix frontend exec -- node --test src/shared/layouts/accountImpersonation.test.js src/shared/layouts/AccountCenterLayout.source.test.js src/user/components/personal-center-visuals.source.test.js src/user/pages/personal-center-layout.source.test.js
```

Expected: PASS.

- [ ] **Step 2: Run full build checks from a clean committed snapshot**

Run the frontend build and Go embedded build from a temporary detached worktree at the exact current SHA, so unrelated dirty files cannot enter the binary:

```powershell
npm --prefix frontend ci --no-audit --no-fund
npm --prefix frontend run build
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-go-local.ps1
```

Expected: both builds exit `0`; only documented existing Vite chunk warnings are allowed.

- [ ] **Step 3: Start and verify the exact Go runtime locally**

Do not terminate an unrelated active 4001 process until it is confirmed idle. Start the exact built binary on an available review port when necessary, then run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/check-local-go.ps1 -BaseUrl http://127.0.0.1:<review-port> -ExpectedSHA <full-git-sha>
```

Expected: `/health`, routed pages, and `/api/runtime-build-info` all pass with the exact full SHA.

- [ ] **Step 4: Perform browser acceptance**

Verify at desktop width and a low-height viewport:

1. sidebar navigation scrolls while “返回工作台” and “退出登录” remain visible;
2. right content reaches its last setting;
3. a manager opens the switch dropdown and immediately sees own-team members;
4. search by display name and username filters the same list;
5. selecting a member requires confirmation;
6. after switching, “退出切换” and “退出登录” are simultaneously visible and perform different actions;
7. a member account has no switch entry.

Capture the four screenshots listed above.

- [ ] **Step 5: Update and commit acceptance evidence**

Record the exact SHA, commands, pass counts, runtime port, viewport sizes, and any remaining limitation in `design-qa.md`, then commit only the report and screenshots:

```powershell
git add -- docs/acceptance/personal-center-20261128/design-qa.md docs/acceptance/personal-center-20261128/screenshots/account-switch-dropdown.png docs/acceptance/personal-center-20261128/screenshots/account-switch-search.png docs/acceptance/personal-center-20261128/screenshots/account-switched-exit-controls.png docs/acceptance/personal-center-20261128/screenshots/account-center-low-height-scroll.png
git commit -m "docs(profile): record switch and logout acceptance"
```

- [ ] **Step 6: Stop at the release gate if the branch moved or tests are red**

Before push, compare the verified SHA with `git rev-parse v88`. If they differ, or any declared test is red, do not push or deploy; retest the new stable SHA first. Only a locally verified, committed SHA may proceed to `origin` and `scripts/deploy-go-embedded.ps1`.
