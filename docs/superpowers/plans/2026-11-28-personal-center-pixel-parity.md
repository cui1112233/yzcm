# Personal Center Pixel-Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild every personal-center route so the real 4001 UI faithfully matches the nine approved visual targets, including branded energy imagery and four distinct role badges, without faking business data or permissions.

**Architecture:** Keep the existing Go APIs, React route contracts, role projection, and form behaviors. Replace the layered historical styling with one account-center visual system, add committed raster brand assets plus small reusable visual components, then reshape each page around its existing data. Validate every task with source-contract tests, local browser states, and final same-viewport image comparisons.

**Tech Stack:** React 18, Ant Design 5, Lucide React for standard semantic controls, generated PNG/WebP brand assets for non-standard imagery, CSS, Node built-in test runner, Vite 6, Go embed release tooling.

## Global Constraints

- Work directly on Git branch `v88`; do not create another branch or worktree.
- Preserve every unrelated dirty file and stage only files named by the current task.
- Visual truth is `docs/design/personal-center-20261126/approved/01-09` at 1672/1678 × 941/937.
- Formal role copy is exactly `开发创始`, `开发者`, `管理者`, `组员`.
- Never fabricate balance, consumption, ranking, devices, security features, API keys, members, or success states.
- General icons use one consistent 24px line-icon grammar; the logo, energy scene, and role badges are real committed image assets, not CSS art, emoji, text glyphs, or handmade inline SVG.
- Keep API model addresses hidden from 管理者 and 组员; only 开发创始 and authorized 开发者 can add custom endpoints.
- Keep `prefers-reduced-motion` support and design loading, empty, error, disabled, and unauthorized states.
- Local-first release: build and restart 4001, complete nine-image QA, obtain user confirmation, then push and deploy the exact Git SHA with `scripts/deploy-go-embedded.ps1`.

---

## File Structure

**New focused visual files**

- `frontend/src/user/assets/personal-center/energy-space.webp`: shared decorative energy scene.
- `frontend/src/user/assets/personal-center/brand-energy-mark.webp`: large translucent brand mark.
- `frontend/src/user/assets/personal-center/role-founder.png`: 开发创始 badge.
- `frontend/src/user/assets/personal-center/role-developer.png`: 开发者 badge.
- `frontend/src/user/assets/personal-center/role-manager.png`: 管理者 badge.
- `frontend/src/user/assets/personal-center/role-member.png`: 组员 badge.
- `frontend/src/user/components/PersonalCenterIcon.jsx`: consistent semantic icon wrapper.
- `frontend/src/user/components/PersonalCenterVisuals.jsx`: energy background, glass surface, metric and empty-state primitives.
- `frontend/src/user/components/personal-center-visuals.source.test.js`: asset and component contract.
- `frontend/src/shared/styles/account-center-pixel-parity.css`: the only new visual authority for the personal-center shell and pages.
- `frontend/src/user/pages/PlatformTeamConsumptionPage.jsx`: founder/developer all-team consumption projection when the existing route capability is present.
- `docs/acceptance/personal-center-20261128/`: normalized implementation screenshots and comparison evidence.

**Existing files retained for business behavior**

- `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- `frontend/src/user/pages/AccountOverviewPage.jsx`
- `frontend/src/user/pages/EnergyBillingPage.jsx`
- `frontend/src/user/pages/ProfilePage.jsx`
- `frontend/src/user/pages/SecurityPage.jsx`
- `frontend/src/user/pages/TeamPage.jsx`
- `frontend/src/user/pages/AccountRolePage.jsx`
- `frontend/src/user/pages/ApiConfigPage.jsx`
- `frontend/src/user/pages/accountCenterEnergyShared.jsx`
- `frontend/src/user/pages/accountCenterShared.jsx`
- `frontend/src/user/App.jsx`

---

### Task 1: Lock the visual asset and component contract

**Files:**
- Create: `frontend/src/user/components/personal-center-visuals.source.test.js`
- Modify: `frontend/src/user/components/RoleEnergyIcon.test.js`

**Interfaces:**
- Produces: source-level requirements for `PersonalCenterIcon`, `PersonalCenterVisuals`, four raster role assets, energy scene assets, reduced motion, and the pixel-parity stylesheet.

- [ ] **Step 1: Write the failing source contract**

```js
test('uses committed brand assets instead of CSS or inline SVG stand-ins', () => {
  for (const asset of ['role-founder.png', 'role-developer.png', 'role-manager.png', 'role-member.png', 'energy-space.webp', 'brand-energy-mark.webp']) {
    assert.equal(existsSync(resolve(assetRoot, asset)), true, asset);
  }
  assert.match(roleIconSource, /<img/);
  assert.doesNotMatch(roleIconSource, /<svg|linearGradient|clipPath/);
});

test('provides the nine approved page/state visual contracts', () => {
  for (const name of ['overview', 'profile', 'security', 'team', 'platform-consumption', 'api-manager', 'api-modal', 'api-founder', 'accounts']) {
    assert.match(cssSource, new RegExp(`pc-${name}`));
  }
});
```

- [ ] **Step 2: Run the contract and verify RED**

Run: `node --test frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/components/RoleEnergyIcon.test.js`

Expected: FAIL because the assets and components do not exist and `RoleEnergyIcon` still draws an inline SVG.

- [ ] **Step 3: Commit the red test**

```powershell
git add frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/components/RoleEnergyIcon.test.js
git commit -m "test(profile): define pixel-parity visual assets"
```

### Task 2: Produce the brand energy and role-badge assets

**Files:**
- Create: `frontend/src/user/assets/personal-center/energy-space.webp`
- Create: `frontend/src/user/assets/personal-center/brand-energy-mark.webp`
- Create: `frontend/src/user/assets/personal-center/role-founder.png`
- Create: `frontend/src/user/assets/personal-center/role-developer.png`
- Create: `frontend/src/user/assets/personal-center/role-manager.png`
- Create: `frontend/src/user/assets/personal-center/role-member.png`
- Modify: `frontend/src/user/components/RoleEnergyIcon.jsx`
- Create: `frontend/src/user/components/PersonalCenterIcon.jsx`
- Create: `frontend/src/user/components/PersonalCenterVisuals.jsx`

**Interfaces:**
- Produces: `RoleEnergyIcon({ role, size = 26, className = '', animated = true })`, `PersonalCenterIcon({ name, size = 20 })`, `EnergyScene()`, `GlassPanel`, `MetricTile`, and `DesignedEmptyState`.

- [ ] **Step 1: Generate real source assets from the approved references**

Use Image Gen with `01-account-overview.png`, `04-team-management.png`, and `07-accounts-by-team.png` as visual references. Generate the energy scene and brand mark as separate wide decorative assets, then generate four square transparent role badges using the exact role descriptions in the design spec. Inspect every output at 24px and 48px before accepting it.

- [ ] **Step 2: Place and optimize the accepted files**

Store only final assets under `frontend/src/user/assets/personal-center/`. Preserve transparency on badges, use WebP for wide backgrounds, and keep every asset sharp at its consuming size.

- [ ] **Step 3: Replace the inline role drawing and add primitives**

```jsx
const ROLE_ASSETS = {
  开发创始: founderBadge,
  开发者: developerBadge,
  管理者: managerBadge,
  组员: memberBadge,
};

export default function RoleEnergyIcon({ role, size = 26, animated = true, className = '' }) {
  const label = normalizeRole(role);
  return <img className={`pc-role-badge ${animated ? 'is-animated' : ''} ${className}`} src={ROLE_ASSETS[label]} width={size} height={size} alt="" aria-label={`${label}身份图标`} />;
}
```

- [ ] **Step 4: Run the focused contract and verify GREEN**

Run: `node --test frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/components/RoleEnergyIcon.test.js`

Expected: PASS.

- [ ] **Step 5: Commit the asset layer**

```powershell
git add frontend/src/user/assets/personal-center frontend/src/user/components/RoleEnergyIcon.jsx frontend/src/user/components/PersonalCenterIcon.jsx frontend/src/user/components/PersonalCenterVisuals.jsx frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/components/RoleEnergyIcon.test.js
git commit -m "feat(profile): add branded energy and role assets"
```

### Task 3: Replace the layered shell styling with one visual authority

**Files:**
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- Create: `frontend/src/shared/styles/account-center-pixel-parity.css`
- Modify: `frontend/src/user/main.jsx`
- Modify: `frontend/src/user/previewApiSettings.jsx`
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.source.test.js`
- Modify: `frontend/src/user/pages/personal-center-layout.source.test.js`

**Interfaces:**
- Consumes: `EnergyScene`, `RoleEnergyIcon`, `PersonalCenterIcon`.
- Produces: `.pc-shell`, `.pc-topbar`, `.pc-sidebar`, `.pc-main`, `.pc-glass`, and the shared desktop/tablet/mobile layout contract.

- [ ] **Step 1: Extend shell tests to reject the old override stack**

```js
assert.match(layoutSource, /className="pc-shell"/);
assert.match(mainSource, /account-center-pixel-parity\.css/);
assert.doesNotMatch(mainSource, /account-center-visual-rebuild\.css/);
assert.match(cssSource, /grid-template-columns:\s*220px minmax\(0, 1fr\)/);
assert.match(cssSource, /prefers-reduced-motion/);
```

- [ ] **Step 2: Run shell tests and verify RED**

Run: `node --test frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js`

- [ ] **Step 3: Refactor the shell markup and import the new stylesheet**

Keep navigation gates and return behavior unchanged. Replace text brand marks and historical class names with the new image-backed energy shell and consistent icon wrapper.

- [ ] **Step 4: Implement the visual tokens and responsive shell**

Define the approved palette, glass layers, typography, 220px sidebar, 58px topbar, three breakpoints, focus rings, scrollbars, hover/selected states, and reduced-motion fallback in the new stylesheet. Do not append more rules to the 1,272-line historical stylesheet.

- [ ] **Step 5: Run tests and commit**

Run: `node --test frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/shared/layouts/accountCenterNavigation.test.js frontend/src/user/pages/personal-center-layout.source.test.js`

```powershell
git add frontend/src/shared/layouts/AccountCenterLayout.jsx frontend/src/shared/styles/account-center-pixel-parity.css frontend/src/user/main.jsx frontend/src/user/previewApiSettings.jsx frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js
git commit -m "feat(profile): rebuild the pixel-parity account shell"
```

### Task 4: Rebuild account overview and energy pages

**Files:**
- Modify: `frontend/src/user/pages/AccountOverviewPage.jsx`
- Modify: `frontend/src/user/pages/EnergyBillingPage.jsx`
- Modify: `frontend/src/user/pages/accountCenterEnergyShared.jsx`
- Replace visual rules in: `frontend/src/user/pages/account-center-energy.css`
- Modify: `frontend/src/user/pages/account-center-energy.source.test.js`

**Interfaces:**
- Consumes: existing `getAccountOverview`, `getEnergyRanking`, `getEnergyTeams`, and trusted YZCM console URL.
- Produces: `.pc-overview` and `.pc-platform-consumption` structures with truthful loading, error, empty, and populated projections.

- [ ] **Step 1: Write failing structure tests for reference images 01 and 05**

Assert the overview contains identity, balance, trend, ranking, connection, model availability and bills regions. Assert platform consumption contains scope tabs, metrics, trend, team ranking, team balance table and details region, all bound to real API values or explicit empty states.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `node --test frontend/src/user/pages/account-center-energy.source.test.js frontend/src/shared/api/accountCenterEnergy.test.js`

- [ ] **Step 3: Reshape both pages without changing fetch behavior**

Use `Intl.NumberFormat`, existing loading/error state and role gates. Keep unavailable values as `暂不可用`; do not insert the mockup's sample numbers.

- [ ] **Step 4: Implement matching grids, charts/tables and empty states**

Use semantic HTML/CSS for charts backed by returned series. When no series exists, show `DesignedEmptyState` inside the same glass frame.

- [ ] **Step 5: Run tests and commit**

Run: `node --test frontend/src/shared/api/accountCenterEnergy.test.js frontend/src/user/pages/account-center-energy.source.test.js`

```powershell
git add frontend/src/user/pages/AccountOverviewPage.jsx frontend/src/user/pages/EnergyBillingPage.jsx frontend/src/user/pages/accountCenterEnergyShared.jsx frontend/src/user/pages/account-center-energy.css frontend/src/user/pages/account-center-energy.source.test.js
git commit -m "feat(profile): match overview and energy dashboards"
```

### Task 5: Rebuild profile and security pages

**Files:**
- Modify: `frontend/src/user/pages/ProfilePage.jsx`
- Modify: `frontend/src/user/pages/SecurityPage.jsx`
- Modify: `frontend/src/user/pages/personal-center-layout.source.test.js`

**Interfaces:**
- Consumes: existing profile, password, MFA, passkey, email verification, device and recovery APIs.
- Produces: `.pc-profile` and `.pc-security` structures matching references 02 and 03 without fake success states.

- [ ] **Step 1: Add failing structure and truthfulness tests**

Assert the profile has identity strip, basic information, account ownership and visibility regions. Assert security has authentication, account recovery, devices and activity regions and contains no fabricated security score.

- [ ] **Step 2: Run the test and verify RED**

Run: `node --test frontend/src/user/pages/personal-center-layout.source.test.js`

- [ ] **Step 3: Implement the reference layouts**

Keep every existing form submit and recovery call. Render unsupported MFA/passkey/device actions as unavailable or unconfigured instead of clickable success UI.

- [ ] **Step 4: Run tests and commit**

Run: `node --test frontend/src/user/pages/personal-center-layout.source.test.js`

```powershell
git add frontend/src/user/pages/ProfilePage.jsx frontend/src/user/pages/SecurityPage.jsx frontend/src/user/pages/personal-center-layout.source.test.js
git commit -m "feat(profile): match profile and security visuals"
```

### Task 6: Rebuild team management and account governance

**Files:**
- Modify: `frontend/src/user/pages/TeamPage.jsx`
- Modify: `frontend/src/user/pages/AccountRolePage.jsx`
- Modify: `frontend/src/user/pages/AccountRolePage.source.test.js`
- Modify: `frontend/src/user/pages/personal-center-layout.source.test.js`

**Interfaces:**
- Consumes: existing directory, audit, team creation, account creation, role change, password reset, disable/enable, and impersonation actions.
- Produces: `.pc-team` and `.pc-accounts` layouts matching references 04 and 07.

- [ ] **Step 1: Add failing tests for summary, filters, tables and side regions**

Assert team management renders team summary, member filters/list, energy ranking, recent actions and reset modal. Assert account governance renders four metrics, filters, team accordion rows, account actions and exact four role labels.

- [ ] **Step 2: Run tests and verify RED**

Run: `node --test frontend/src/user/pages/AccountRolePage.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js`

- [ ] **Step 3: Reshape the existing actions into the approved layouts**

Do not add client-only fake members or consumption. Preserve audit logging and server permission errors.

- [ ] **Step 4: Run tests and commit**

Run: `node --test frontend/src/user/pages/AccountRolePage.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js`

```powershell
git add frontend/src/user/pages/TeamPage.jsx frontend/src/user/pages/AccountRolePage.jsx frontend/src/user/pages/AccountRolePage.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js
git commit -m "feat(profile): match team and account governance visuals"
```

### Task 7: Rebuild API configuration for manager and founder states

**Files:**
- Modify: `frontend/src/user/pages/ApiConfigPage.jsx`
- Modify: `frontend/src/user/pages/ApiConfigPage.source.test.js`
- Modify: `frontend/src/user/pages/api-config-role-policy.test.js`
- Modify: `frontend/src/user/pages/api-config-preset-state.test.js`

**Interfaces:**
- Consumes: existing API capability projection, catalog loading, selected preset state, key drafts and custom endpoint actions.
- Produces: `.pc-api-manager`, `.pc-api-founder`, `.pc-api-preset-modal` visual states matching references 06, 08 and 09.

- [ ] **Step 1: Add failing role-state visual contracts**

Assert managers see YZCM selection and per-card key fields but no endpoint/address controls. Assert founder/developer full mode sees add-custom controls and expanded address/model-ID fields only inside the selected custom card. Assert the selection modal has search, available and selected columns.

- [ ] **Step 2: Run tests and verify RED**

Run: `node --test frontend/src/user/pages/ApiConfigPage.source.test.js frontend/src/user/pages/api-config-role-policy.test.js frontend/src/user/pages/api-config-preset-state.test.js frontend/src/user/pages/api-config-initial-load.test.js`

- [ ] **Step 3: Implement the three approved states**

Keep autosave, multiple key drafts and existing model-type accordions. Do not reveal provider addresses in restricted modes.

- [ ] **Step 4: Run tests and commit**

Run the command from Step 2 and expect all tests PASS.

```powershell
git add frontend/src/user/pages/ApiConfigPage.jsx frontend/src/user/pages/ApiConfigPage.source.test.js frontend/src/user/pages/api-config-role-policy.test.js frontend/src/user/pages/api-config-preset-state.test.js
git commit -m "feat(profile): match role-aware API configuration visuals"
```

### Task 8: Build, restart 4001 and complete nine-image design QA

**Files:**
- Create: `docs/acceptance/personal-center-20261128/*.png`
- Create or replace: `design-qa.md`

**Interfaces:**
- Consumes: all seven routes and nine approved visual states.
- Produces: same-viewport evidence and `final result: passed` before release.

- [ ] **Step 1: Run the focused personal-center suite**

Run:

```powershell
node --test frontend/src/user/components/RoleEnergyIcon.test.js frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/shared/layouts/accountCenterNavigation.test.js frontend/src/user/pages/account-center-energy.source.test.js frontend/src/user/pages/api-config-role-policy.test.js frontend/src/user/pages/ApiConfigPage.source.test.js frontend/src/user/pages/AccountRolePage.source.test.js frontend/src/shared/styles/contrast-contract.test.js
```

- [ ] **Step 2: Build and restart the exact committed version on 4001**

Run:

```powershell
npm --prefix frontend run build
powershell -ExecutionPolicy Bypass -File scripts/build-go-local.ps1
powershell -ExecutionPolicy Bypass -File scripts/start-local-go-review.ps1 -SkipBuild -Detached
powershell -ExecutionPolicy Bypass -File scripts/check-local-go.ps1 -BaseUrl http://127.0.0.1:4001 -ExpectedSHA (git rev-parse --short HEAD)
```

- [ ] **Step 3: Capture nine implementation states at source dimensions**

Use the in-app browser and the authenticated founder account. Capture each route/state at the matching 1672/1678 × 941/937 viewport. Save screenshots under `docs/acceptance/personal-center-20261128/`.

- [ ] **Step 4: Create actual side-by-side comparison evidence**

For each approved source and implementation capture, create a single comparison canvas and inspect full view plus focused typography/icon/table regions. Record viewport, density, state and asset paths in `design-qa.md`.

- [ ] **Step 5: Iterate until QA passes**

Fix every P0/P1/P2 difference, rebuild/restart, recapture at the same viewport and record the comparison history. `design-qa.md` must end with exactly `final result: passed`.

- [ ] **Step 6: Commit QA evidence**

```powershell
git add docs/acceptance/personal-center-20261128 design-qa.md
git commit -m "test(profile): record nine-screen visual parity"
```

### Task 9: User approval, remote push and exact-SHA production release

**Files:**
- Generated: `.release/go-embedded/*` (not source implementation)

**Interfaces:**
- Consumes: clean committed `v88` source and user approval of 4001.
- Produces: remote branch/ref, production binary, runtime manifest, public acceptance and rollback SHA.

- [ ] **Step 1: Obtain user confirmation of the visible 4001 result**

Do not push or deploy before the user confirms the nine-page result.

- [ ] **Step 2: Resolve the remote-history mismatch without overwriting `main`**

The target repository currently has only an unrelated-history `main`. Push local `v88` as a new `v88` branch without force; do not merge or overwrite `main`.

Run:

```powershell
git -c http.proxy= -c https.proxy= push -u origin v88:v88
git -c http.proxy= -c https.proxy= ls-remote --heads origin v88
```

- [ ] **Step 3: Build from a clean committed snapshot**

Because the primary checkout contains unrelated dirty work, clone the local committed `v88` into a temporary release directory and run the approved script there with `-RequireCleanFrontend`. The release SHA must equal the pushed `origin/v88` SHA.

- [ ] **Step 4: Deploy the exact SHA**

Run the clean snapshot's `scripts/deploy-go-embedded.ps1` with `-Deploy -TargetHost 115.190.156.223 -TargetUser root -ServiceName qiantie -RequireCleanFrontend`.

- [ ] **Step 5: Verify production and record rollback**

Check `qiantie.service`, `/health`, `/api/runtime-build-info`, public `/account`, `/profile`, `/security`, `/team`, `/api-config`, `/accounts`, and the served frontend entry. Confirm production `git_sha` equals remote `v88`, and record the previous stable SHA before declaring completion.

---

## Plan Self-Review

- Spec coverage: all ten sections of `2026-11-28-personal-center-pixel-parity-design.md` map to Tasks 1–9.
- Asset boundary: role badges and energy imagery are real committed assets; general semantic controls use a consistent icon component.
- Data boundary: every mock value is replaced by real API state or a designed unavailable/empty state.
- Release boundary: push and production deployment remain blocked until nine-image QA and user approval.
- Execution mode: inline execution is selected because the user said “执行” and repository rules prohibit creating extra branches; no subagent dispatch is used.

