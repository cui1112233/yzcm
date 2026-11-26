# Personal Center Glass UI and Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the approved glass energy visual system to every personal-center page and prove the complete feature in the embedded Go runtime on local 4001.

**Architecture:** Use one account-center shell, one role-icon component, and shared visual tokens rather than page-specific themes. Acceptance combines source tests, focused browser tests, full frontend/backend builds, embedded runtime identity, authenticated role checks and screenshot review.

**Tech Stack:** React 18, Ant Design 5, CSS, Vite 6, Go embed, existing PowerShell build/check scripts.

## Global Constraints

- Background is dynamic deep navy/black with blue, cyan, purple and restrained magenta energy particles.
- Glass panels stay visibly translucent with readable local scrims.
- Role icons are brand-derived, 24–28 px, and sit beside the username.
- Formal labels remain `开发创始`, `开发者`, `管理者`, `组员`.
- Respect `prefers-reduced-motion`.
- Do not publish; local 4001 acceptance is the terminal state of this plan unless the user separately authorizes release.

---

### Task 1: Consolidate navigation, role presentation, and shared shell

**Files:**
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- Modify: `frontend/src/shared/layouts/accountCenterNavigation.js`
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.source.test.js`
- Modify: `frontend/src/user/pages/accountCenterShared.jsx`
- Create: `frontend/src/user/components/RoleEnergyIcon.jsx`
- Create: `frontend/src/user/components/RoleEnergyIcon.test.js`
- Modify: `frontend/src/shared/styles/profile-grey-glass.css`
- Modify: `frontend/src/shared/styles/account-center-visual-rebuild.css`

**Interfaces:**
- `RoleEnergyIcon({role, size=26, decorative=false})` maps the four roles to four brand-derived variants.
- Navigation is projected from server capabilities and contains overview, profile, energy/bills, security, team, API, YZCM external, accounts/roles as allowed.

- [ ] **Step 1: Write failing source/component tests** for exact labels, member API-config absence, small role icon placement, logout/return controls and no member-center link.
- [ ] **Step 2: Run the targeted Node tests and verify failure.**
- [ ] **Step 3: Implement the shared shell and icon component.** Keep identity animation to opacity/highlight/orbit transforms; do not use a large role card.
- [ ] **Step 4: Run targeted tests; expect PASS, then commit.**

```powershell
git add frontend/src/shared/layouts/AccountCenterLayout.jsx frontend/src/shared/layouts/accountCenterNavigation.js frontend/src/shared/layouts/AccountCenterLayout.source.test.js frontend/src/user/pages/accountCenterShared.jsx frontend/src/user/components/RoleEnergyIcon.jsx frontend/src/user/components/RoleEnergyIcon.test.js frontend/src/shared/styles/profile-grey-glass.css frontend/src/shared/styles/account-center-visual-rebuild.css
git commit -m "feat(profile): unify account center navigation and identity"
```

### Task 2: Apply the compact glass layout to all pages

**Files:**
- Modify: `frontend/src/user/pages/ProfilePage.jsx`
- Modify: `frontend/src/user/pages/AccountOverviewPage.jsx`
- Modify: `frontend/src/user/pages/EnergyBillingPage.jsx`
- Modify: `frontend/src/user/pages/SecurityPage.jsx`
- Modify: `frontend/src/user/pages/TeamPage.jsx`
- Modify: `frontend/src/user/pages/ApiConfigPage.jsx`
- Modify: `frontend/src/user/pages/AccountRolePage.jsx`
- Create: `frontend/src/user/pages/personal-center-layout.source.test.js`
- Modify: `frontend/src/shared/styles/member-center.css`
- Modify: `frontend/src/shared/styles/team-governance.css`
- Modify: `frontend/src/shared/styles/team-collaboration.css`

**Interfaces:**
- Every page uses `PageHeader`, `Panel`, `MetricCard`, `MemberIdentity`, and the same account-center tokens; no page creates a second shell.

- [ ] **Step 1: Add failing source tests** for compact profile grid, visible account/billing error states, team grouping, API modal-only selector, security sections, and no oversized identity panel.
- [ ] **Step 2: Run `node --test src/user/pages/personal-center-layout.source.test.js src/shared/styles/contrast-contract.test.js`; verify failure.**
- [ ] **Step 3: Implement the layouts and CSS tokens.** Use a 12-column desktop grid, 8/4 profile split, translucent 45–60% surfaces and denser table scrims.
- [ ] **Step 4: Add `@media (prefers-reduced-motion: reduce)` to stop background travel, looping particles and identity orbits while retaining static highlights.**
- [ ] **Step 5: Run targeted tests and `npm run build`; expect PASS, then commit.**

```powershell
git add frontend/src/user/pages/ProfilePage.jsx frontend/src/user/pages/AccountOverviewPage.jsx frontend/src/user/pages/EnergyBillingPage.jsx frontend/src/user/pages/SecurityPage.jsx frontend/src/user/pages/TeamPage.jsx frontend/src/user/pages/ApiConfigPage.jsx frontend/src/user/pages/AccountRolePage.jsx frontend/src/user/pages/personal-center-layout.source.test.js frontend/src/shared/styles/member-center.css frontend/src/shared/styles/team-governance.css frontend/src/shared/styles/team-collaboration.css
git commit -m "feat(profile): apply energy glass personal center layout"
```

### Task 3: Run full source, build, and embedded-runtime verification

**Files:**
- Modify only if a test exposes an in-scope defect; use the earlier task's file list and add a regression test beside the fix.
- Create: `docs/acceptance/2026-11-26-personal-center-energy-roles.md`

**Interfaces:**
- Acceptance record contains tested SHA, frontend entry asset, runtime SHA, role accounts used, API responses, screenshots, failures and explicit exclusions.

- [ ] **Step 1: Run backend focused and full tests.**

```powershell
Set-Location backend
go test ./internal/accountcenter ./internal/accountsecurity ./internal/yzcmmodels ./internal/yzcmusage ./internal/httpapi ./internal/storage -count=1
go test ./... -count=1
```

Expected: PASS. Record any unrelated pre-existing failure separately; do not hide it or edit unrelated code.

- [ ] **Step 2: Run frontend targeted tests, full test suite, and production build.**

```powershell
Set-Location frontend
node --test src/shared/layouts/AccountCenterLayout.source.test.js src/user/pages/personal-center-layout.source.test.js src/user/pages/api-config-role-policy.test.js src/user/pages/SecurityPage.source.test.js
npm test
npm run build
```

Expected: PASS and a new `dist/index.html` entry asset.

- [ ] **Step 3: Build the Go-embedded local release without deployment.**

```powershell
Set-Location ..
powershell -ExecutionPolicy Bypass -File scripts/deploy-go-embedded.ps1
```

Expected: a local `.release/go-embedded` package; no SSH, upload or production change.

- [ ] **Step 4: Restart only the confirmed local review runtime and verify identity.** Use the project’s existing local review start/check process, then run:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/check-local-go.ps1 -BaseUrl http://127.0.0.1:4001 -ExpectedSHA (git rev-parse HEAD)
```

Expected: `/health` 200 and `/api/runtime-build-info` reports the tested SHA and current frontend entry.

- [ ] **Step 5: Perform authenticated role-matrix acceptance on local 4001.** Verify founder, developer, manager and member navigation/data; manager YZCM-only modal and independent inline Keys; all permitted/denied switches; energy balance/ranking; reset-password boundary; archive/restore; reduced motion; and refresh persistence. Do not use a real paid call unless separately authorized.

- [ ] **Step 6: Capture screenshots** for overview, profile, energy/bills, security, team, manager API config modal, founder accounts/roles, switched-account banner and reduced-motion state. Record exact paths and observed data in the acceptance document.

- [ ] **Step 7: Commit only the acceptance record and any scoped regression fixes.**

```powershell
git add docs/acceptance/2026-11-26-personal-center-energy-roles.md
git commit -m "test(profile): record local personal center acceptance"
```
