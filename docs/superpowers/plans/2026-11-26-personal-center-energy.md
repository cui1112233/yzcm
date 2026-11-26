# Personal Center Energy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show real YZCM team energy, reconciled consumption, member rankings, bills, and all-team consumption with one consistent data contract.

**Architecture:** Extend the existing `yzcmusage` ledger with stable team and real-actor attribution, then serve role-scoped summary and detail queries. The frontend merges the useful parts of member center and usage into account overview plus energy/bills; balance always comes from the bound YZCM wallet and is never reconstructed locally.

**Tech Stack:** Go 1.26, MySQL 8, existing `backend/internal/yzcmusage`, React 18, Ant Design charts/tables already available in the project.

## Global Constraints

- `1 能量 = 1 元 YZCM 可用额度`.
- Every role sees its own team balance and same-team member ranking.
- Founder and developer see all-team consumption and per-team balances, never a combined wallet balance.
- Unknown balance is `暂无法获取`, not zero; unreconciled calls remain `待核账`.
- Account switching charges the subject and audits the actor.
- Recharge only links to the existing YZCM API console.

---

### Task 1: Add team and actor attribution to the usage ledger

**Files:**
- Create: `backend/internal/storage/account_center_energy_schema.go`
- Create: `backend/internal/storage/account_center_energy_schema_test.go`
- Modify: `backend/internal/storage/local_executor_schema.go:115-135`
- Modify: `backend/internal/yzcmusage/store.go`
- Modify: `backend/internal/yzcmusage/store_test.go`
- Modify: `backend/internal/yzcmusage/tracker.go`

**Interfaces:**
- Migration version `8800405` adds nullable `team_id` and `actor_username` to `yzcm_usage_requests`, then backfills team IDs from active memberships without changing `owner`.
- `RecordRequest` accepts subject owner, team ID, actor username, feature and credential hash.

- [ ] **Step 1: Write failing schema/store tests** that require owner=`member`, actor=`founder`, team=`team-a` to persist independently and duplicate receipts to remain idempotent.
- [ ] **Step 2: Run `go test ./internal/storage ./internal/yzcmusage -run 'Energy|Actor|Team' -count=1` and verify failure.**
- [ ] **Step 3: Implement migration, tracker attribution and indexed queries.** Never move an existing receipt to another owner/team during reconciliation.
- [ ] **Step 4: Run `go test ./internal/storage ./internal/yzcmusage -count=1`; expect PASS.**
- [ ] **Step 5: Commit the ledger change.**

```powershell
git add backend/internal/storage/account_center_energy_schema.go backend/internal/storage/account_center_energy_schema_test.go backend/internal/storage/local_executor_schema.go backend/internal/yzcmusage/store.go backend/internal/yzcmusage/store_test.go backend/internal/yzcmusage/tracker.go
git commit -m "feat(energy): attribute YZCM usage to teams and actors"
```

### Task 2: Build role-scoped energy services and APIs

**Files:**
- Create: `backend/internal/accountcenter/energy.go`
- Create: `backend/internal/accountcenter/energy_test.go`
- Create: `backend/internal/httpapi/account_center_energy.go`
- Create: `backend/internal/httpapi/account_center_energy_test.go`
- Modify: `backend/internal/httpapi/member_usage.go`
- Modify: `backend/internal/httpapi/router.go`

**Interfaces:**
- Produces `GET /api/account-center/overview`, `/api/account-center/energy`, `/api/account-center/energy/ledger`, `/api/account-center/energy/ranking`, and privileged `/api/account-center/energy/teams`.
- Response money fields use decimal strings, for example `{ "amount": "12680.00", "unit": "energy", "updatedAt": "..." }`.

- [ ] **Step 1: Write service and HTTP tests** for same-team visibility, cross-team denial, founder/developer all-team access, no total balance field, filters shared by summary/list/export, and stale-balance error state.
- [ ] **Step 2: Run the focused tests and verify missing service/routes fail.**

Run: `go test ./internal/accountcenter ./internal/httpapi -run Energy -count=1` from `backend`

- [ ] **Step 3: Implement service queries** using `actual_cost` only for reconciled totals; return pending count separately. Add a YZCM wallet balance adapter with explicit `Unavailable` and `UpdatedAt` fields; do not derive balance from ledger totals.
- [ ] **Step 4: Register routes behind shared authorization and run focused tests; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/accountcenter/energy.go backend/internal/accountcenter/energy_test.go backend/internal/httpapi/account_center_energy.go backend/internal/httpapi/account_center_energy_test.go backend/internal/httpapi/member_usage.go backend/internal/httpapi/router.go
git commit -m "feat(energy): expose role-scoped balances and bills"
```

### Task 3: Merge member center and usage into real account pages

**Files:**
- Create: `frontend/src/shared/api/accountCenterEnergy.js`
- Create: `frontend/src/shared/api/accountCenterEnergy.test.js`
- Create: `frontend/src/user/pages/AccountOverviewPage.jsx`
- Create: `frontend/src/user/pages/EnergyBillingPage.jsx`
- Create: `frontend/src/user/pages/account-center-energy.source.test.js`
- Modify: `frontend/src/user/App.jsx:90-110`
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- Modify: `frontend/src/shared/layouts/accountCenterNavigation.js`
- Modify: `frontend/src/user/pages/MemberCenterPage.jsx`
- Modify: `frontend/src/user/pages/UsageStatsPage.jsx`

**Interfaces:**
- `/account` renders overview; `/usage` becomes “能量与账单”; `/member` redirects inside the SPA to `/account` without a second data implementation.
- `loadEnergyDashboard(filters)` fetches summary, ledger and ranking with one normalized filter object.

- [ ] **Step 1: Write source/API tests** asserting navigation labels, member redirect, no fake membership copy, no combined team balance, and visible team ranking for members.
- [ ] **Step 2: Run `node --test src/shared/api/accountCenterEnergy.test.js src/user/pages/account-center-energy.source.test.js` and verify failure.**
- [ ] **Step 3: Implement API normalization and pages.** Show last updated time, pending reconciliation, empty states, and “前往 YZCM API 补充能量”; use the existing external URL rather than a payment form.
- [ ] **Step 4: Run focused tests and `npm run build`; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add frontend/src/shared/api/accountCenterEnergy.js frontend/src/shared/api/accountCenterEnergy.test.js frontend/src/user/pages/AccountOverviewPage.jsx frontend/src/user/pages/EnergyBillingPage.jsx frontend/src/user/pages/account-center-energy.source.test.js frontend/src/user/App.jsx frontend/src/shared/layouts/AccountCenterLayout.jsx frontend/src/shared/layouts/accountCenterNavigation.js frontend/src/user/pages/MemberCenterPage.jsx frontend/src/user/pages/UsageStatsPage.jsx
git commit -m "feat(energy): add account overview and energy billing pages"
```
