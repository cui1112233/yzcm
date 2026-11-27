# Personal Center Access Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the old three-role JSON compatibility boundary with durable four-role teams, grants, audits, lifecycle controls, and safe account switching.

**Architecture:** Add new MySQL tables beside the rigid legacy `accounts` table, migrate compatible profile/team data idempotently, and expose a typed authorization service used by every sensitive route. Account switching keeps both actor and subject identities in request context and never mutates the login session owner.

**Tech Stack:** Go 1.26, MySQL 8 migrations, existing `auth.Store`, `net/http`, React 18, Ant Design 5.

## Global Constraints

- Formal roles are `founder`, `developer`, `manager`, `member`, rendered as `开发创始`, `开发者`, `管理者`, `组员`.
- Only the founder grants or revokes developer status.
- Developers can create teams and manager/member accounts but cannot modify the founder or create developers.
- Managers manage only their own members; members manage nobody.
- Actor and subject must both be audited during account switching.
- Archive and disable are recoverable; permanent deletion is founder-only and blocked by balance, in-flight work, pending reconciliation, or retention requirements.

---

### Task 1: Add durable identity, team, grant, audit, and impersonation storage

**Files:**
- Create: `backend/internal/storage/account_center_schema.go`
- Create: `backend/internal/storage/account_center_schema_test.go`
- Create: `backend/internal/accountcenter/store.go`
- Create: `backend/internal/accountcenter/store_test.go`
- Modify: `backend/internal/storage/local_executor_schema.go:115-134`

**Interfaces:**
- Produces: `accountcenter.Store` with `Identity`, `Team`, `Membership`, `Grant`, `AuditEvent`, and `Impersonation` methods.
- Migration versions: `8800403` for identity/team/audit tables and `8800404` for idempotent legacy import markers.

- [ ] **Step 1: Write migration tests** asserting tables `account_center_profiles`, `account_center_teams`, `account_center_memberships`, `account_center_grants`, `account_center_audit_events`, `account_center_impersonations`, unique founder enforcement, unique active team membership, and indexes for team/time audit queries.

```go
func TestAccountCenterMigrationsContainAuthorizationBoundaries(t *testing.T) {
    joined := strings.ToLower(strings.Join(AccountCenterMigrations()[0].SQL, "\n"))
    for _, want := range []string{"account_center_profiles", "account_center_teams", "account_center_memberships", "account_center_grants", "account_center_audit_events", "account_center_impersonations"} {
        if !strings.Contains(joined, want) { t.Fatalf("missing %s", want) }
    }
}
```

- [ ] **Step 2: Run the storage test and verify failure.**

Run: `go test ./internal/storage -run AccountCenter -count=1` from `backend`
Expected: FAIL because `AccountCenterMigrations` does not exist.

- [ ] **Step 3: Implement migrations and typed store.** Keep `accounts` unchanged; store display profile, formal role, team relation, archive state and grants in the new tables. Import old `dev` owner as `founder`, other `dev` profiles as `developer`, `manager` as `manager`, and `member` as `member`; record the import so restart is idempotent.

```go
type Identity struct { Username, DisplayName, Role string; Active bool }
type RequestIdentity struct { Actor Identity; Subject Identity; ImpersonationID string }
type Store interface {
    ResolveIdentity(context.Context, string) (Identity, error)
    ListTeams(context.Context, TeamFilter) ([]TeamSummary, error)
    AppendAudit(context.Context, AuditEvent) error
}
```

- [ ] **Step 4: Run focused storage/store tests.**

Run: `go test ./internal/storage ./internal/accountcenter -count=1` from `backend`
Expected: PASS, including duplicate-founder and duplicate-membership rejection.

- [ ] **Step 5: Commit only Task 1 files.**

```powershell
git add backend/internal/storage/account_center_schema.go backend/internal/storage/account_center_schema_test.go backend/internal/storage/local_executor_schema.go backend/internal/accountcenter/store.go backend/internal/accountcenter/store_test.go
git commit -m "feat(accounts): add durable role and team foundation"
```

### Task 2: Centralize authorization policy

**Files:**
- Create: `backend/internal/accountcenter/policy.go`
- Create: `backend/internal/accountcenter/policy_test.go`
- Modify: `backend/internal/httpapi/auth.go`
- Modify: `backend/internal/httpapi/router.go:540-575`

**Interfaces:**
- Consumes: `accountcenter.Store.ResolveIdentity`.
- Produces: `accountcenter.Policy.Can(actor, action, target)` and `httpapi.AccountCenterIdentityFromContext`.

- [ ] **Step 1: Write table-driven policy tests** for every permitted and denied role pair, including founder-only developer grants, developer team creation, manager-own-member restriction, and member denial.

```go
func TestPolicyMatrix(t *testing.T) {
    cases := []struct{ actor, action, target string; want bool }{
        {"founder", "grant_developer", "developer", true},
        {"developer", "grant_developer", "manager", false},
        {"manager", "reset_password", "own_member", true},
        {"manager", "reset_password", "other_member", false},
    }
    // Assert each case through Policy.Can.
}
```

- [ ] **Step 2: Run and verify failure.**

Run: `go test ./internal/accountcenter -run Policy -count=1` from `backend`
Expected: FAIL because `Policy` is undefined.

- [ ] **Step 3: Implement policy and middleware context.** Resolve the authenticated actor once, attach actor=subject for normal requests, and return 403 without revealing target existence when policy denies access.

- [ ] **Step 4: Run focused authorization tests.**

Run: `go test ./internal/accountcenter ./internal/httpapi -run 'Policy|AccountCenterIdentity' -count=1` from `backend`
Expected: PASS.

- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/accountcenter/policy.go backend/internal/accountcenter/policy_test.go backend/internal/httpapi/auth.go backend/internal/httpapi/router.go
git commit -m "feat(accounts): enforce four-role authorization policy"
```

### Task 3: Implement safe account switching and audit

**Files:**
- Create: `backend/internal/httpapi/account_impersonation.go`
- Create: `backend/internal/httpapi/account_impersonation_test.go`
- Modify: `backend/internal/httpapi/auth.go`
- Modify: `backend/internal/httpapi/router.go`
- Create: `frontend/src/shared/api/accountImpersonation.js`
- Modify: `frontend/src/shared/layouts/AccountCenterLayout.jsx`
- Create: `frontend/src/shared/layouts/accountImpersonation.test.js`

**Interfaces:**
- Produces endpoints `POST /api/account-impersonation`, `DELETE /api/account-impersonation/current`, `GET /api/account-impersonation/current`.
- Produces request context `{actor, subject, impersonationId}`; business ownership uses subject, audit uses actor and subject.

- [ ] **Step 1: Write HTTP tests** for founder-any, developer-manager/member, manager-own-member, all denied cases, expiry, exit, and sensitive route blocking.
- [ ] **Step 2: Run `go test ./internal/httpapi -run Impersonation -count=1` and verify the new tests fail with missing routes.**
- [ ] **Step 3: Implement the routes and middleware.** Store only a random opaque token hash, set a short-lived HttpOnly cookie, and reject password/MFA/role/team ownership/full-Key mutations while switched.
- [ ] **Step 4: Add the persistent React banner** showing `正在以 {subject} 操作` and an exit button; never hide it through page navigation.
- [ ] **Step 5: Run Go and frontend tests.**

Run: `go test ./internal/httpapi -run Impersonation -count=1` from `backend`
Run: `node --test src/shared/layouts/accountImpersonation.test.js` from `frontend`
Expected: PASS.

- [ ] **Step 6: Commit.**

```powershell
git add backend/internal/httpapi/account_impersonation.go backend/internal/httpapi/account_impersonation_test.go backend/internal/httpapi/auth.go backend/internal/httpapi/router.go frontend/src/shared/api/accountImpersonation.js frontend/src/shared/layouts/AccountCenterLayout.jsx frontend/src/shared/layouts/accountImpersonation.test.js
git commit -m "feat(accounts): add audited account switching"
```

### Task 4: Replace global account and team compatibility routes

**Files:**
- Create: `backend/internal/httpapi/account_center_admin.go`
- Create: `backend/internal/httpapi/account_center_admin_test.go`
- Modify: `backend/internal/httpapi/account_admin_compat.go`
- Modify: `backend/internal/httpapi/team_admin_compat.go`
- Modify: `backend/internal/httpapi/account_transfer_compat.go`
- Modify: `frontend/src/shared/api/accountAdmin.js`
- Modify: `frontend/src/shared/api/teamAdmin.js`
- Create: `frontend/src/shared/api/accountAdmin.test.js`
- Create: `frontend/src/shared/api/teamAdmin.test.js`
- Modify: `frontend/src/user/pages/AccountRolePage.jsx`
- Modify: `frontend/src/user/pages/TeamPage.jsx`
- Create: `frontend/src/user/pages/AccountRolePage.source.test.js`

**Interfaces:**
- Produces team-grouped account list, create team/account, grant role, disable/restore, archive/restore, delete preflight, and audit list APIs.
- All mutations call `Policy.Can` and `Store.AppendAudit` in the same request outcome.

- [ ] **Step 1: Write route tests** covering team-grouped results, developer team creation, forbidden developer grants, recoverable archive, founder-only purge preflight, and audit events.
- [ ] **Step 2: Run `go test ./internal/httpapi -run 'AccountCenterAdmin|TeamLifecycle' -count=1` and verify failure.**
- [ ] **Step 3: Implement the typed routes** and leave old compatibility readers only as temporary adapters backed by the new store; do not keep JSON as writable authority.
- [ ] **Step 4: Update the React pages** to group accounts by team and render only policy-returned actions. Use the exact four Chinese role names.
- [ ] **Step 5: Run focused tests and build.**

Run: `go test ./internal/accountcenter ./internal/httpapi -run 'AccountCenter|Team|Policy' -count=1` from `backend`
Run: `node --test src/shared/api/accountAdmin.test.js src/shared/api/teamAdmin.test.js src/user/pages/AccountRolePage.source.test.js` from `frontend`
Expected: PASS.

- [ ] **Step 6: Commit.**

```powershell
git add backend/internal/httpapi/account_center_admin.go backend/internal/httpapi/account_center_admin_test.go backend/internal/httpapi/account_admin_compat.go backend/internal/httpapi/team_admin_compat.go backend/internal/httpapi/account_transfer_compat.go frontend/src/shared/api/accountAdmin.js frontend/src/shared/api/teamAdmin.js frontend/src/user/pages/AccountRolePage.jsx frontend/src/user/pages/TeamPage.jsx frontend/src/shared/api/accountAdmin.test.js frontend/src/shared/api/teamAdmin.test.js frontend/src/user/pages/AccountRolePage.source.test.js
git commit -m "feat(accounts): group account governance by team"
```
