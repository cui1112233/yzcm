# Personal Center Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn password, MFA, Passkey, recovery email, device sessions, account recovery, and admin resets into protected and auditable production features.

**Architecture:** Move security state out of local JSON into MySQL, encrypt MFA secrets through the existing secrets cipher, enforce unique verified recovery email, and centralize session revocation. Admin resets generate one-time temporary passwords and force a password change on next login.

**Tech Stack:** Go 1.26, MySQL 8, existing `apisecrets` cipher, WebAuthn compatibility code, React 18, Ant Design 5.

## Global Constraints

- Remove the formula-based security score.
- One verified email belongs to one account; one account has one verified recovery email.
- Founder resets developer/manager/member; authorized developer resets manager/member; manager resets own members only.
- Admin reset revokes old sessions and forces a password change at next login.
- Temporary passwords are shown once or sent to verified email and never stored in audit plaintext.
- MFA secrets must not remain plaintext JSON.

---

### Task 1: Add protected security persistence

**Files:**
- Create: `backend/internal/storage/account_security_schema.go`
- Create: `backend/internal/storage/account_security_schema_test.go`
- Create: `backend/internal/accountsecurity/store.go`
- Create: `backend/internal/accountsecurity/store_test.go`
- Modify: `backend/internal/storage/local_executor_schema.go`
- Modify: `backend/internal/httpapi/member_profile.go`
- Modify: `backend/internal/httpapi/account_passkey_compat.go`

**Interfaces:**
- Migration version `8800407` creates `account_recovery_emails`, `account_mfa`, `account_passkeys`, `account_security_events`, and `account_password_requirements`.
- `accountsecurity.Store` reads/writes encrypted MFA ciphertext, recovery hashes, Passkey public credentials, forced-change state, and audit-safe events.

- [ ] **Step 1: Write failing migration/store tests** for unique normalized email, encrypted MFA round-trip, duplicate Passkey prevention, and forced-password-change persistence.
- [ ] **Step 2: Run `go test ./internal/storage ./internal/accountsecurity -count=1`; verify failure.**
- [ ] **Step 3: Implement storage and one-time JSON migration.** Abort migration if encryption is unavailable; never copy plaintext MFA secrets into the new table.
- [ ] **Step 4: Adapt member MFA and Passkey routes to the store, then run focused tests; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/storage/account_security_schema.go backend/internal/storage/account_security_schema_test.go backend/internal/storage/local_executor_schema.go backend/internal/accountsecurity/store.go backend/internal/accountsecurity/store_test.go backend/internal/httpapi/member_profile.go backend/internal/httpapi/account_passkey_compat.go
git commit -m "feat(security): protect MFA and Passkey state"
```

### Task 2: Enforce unique recovery and real account lookup

**Files:**
- Modify: `backend/internal/httpapi/account_recovery_compat.go`
- Modify: `backend/internal/httpapi/account_recovery_compat_test.go`
- Modify: `backend/internal/httpapi/security_hardening_test.go`
- Modify: `frontend/src/shared/api/accountRecovery.js`
- Modify: `frontend/src/user/pages/RecoveryPage.jsx`
- Modify: `frontend/src/shared/layouts/UserLayout.jsx`

**Interfaces:**
- Produces non-enumerating forgot-username and forgot-password requests using the verified email store.
- Recovery tokens remain hashed, single-use, expiring and rate-limited.

- [ ] **Step 1: Add failing tests** for duplicate email rejection, email normalization, generic responses for unknown email, token replay denial, expiry, forgot username and password reset session revocation.
- [ ] **Step 2: Run `go test ./internal/httpapi -run 'Recovery|Forgot' -count=1`; verify failures.**
- [ ] **Step 3: Replace JSON email authority with `accountsecurity.Store`, preserve generic public responses, add “忘记账号” beside the existing login recovery link, and wire both recovery modes through `RecoveryPage.jsx`.**
- [ ] **Step 4: Run security tests and focused frontend source tests; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/httpapi/account_recovery_compat.go backend/internal/httpapi/account_recovery_compat_test.go backend/internal/httpapi/security_hardening_test.go frontend/src/shared/api/accountRecovery.js frontend/src/user/pages/RecoveryPage.jsx frontend/src/shared/layouts/UserLayout.jsx
git commit -m "feat(security): enforce unique account recovery email"
```

### Task 3: Implement role-scoped administrator password reset

**Files:**
- Create: `backend/internal/httpapi/account_password_admin.go`
- Create: `backend/internal/httpapi/account_password_admin_test.go`
- Modify: `backend/internal/auth/mysql_store.go`
- Modify: `backend/internal/auth/mysql_store_test.go`
- Modify: `frontend/src/shared/api/accountAdmin.js`
- Modify: `frontend/src/shared/api/teamAdmin.js`
- Modify: `frontend/src/user/pages/AccountRolePage.jsx`
- Modify: `frontend/src/user/pages/TeamPage.jsx`

**Interfaces:**
- Produces `POST /api/account-center/accounts/{username}/reset-password` returning `{delivery:"email"}` or `{delivery:"display_once", temporaryPassword:"..."}` exactly once.
- Login returns `passwordChangeRequired: true` until the target changes the temporary password.

- [ ] **Step 1: Write failing tests** for the complete role matrix, no self-reset through admin API, strong random temporary password, session revocation, forced change, email delivery, one-time display and audit redaction.
- [ ] **Step 2: Run `go test ./internal/auth ./internal/httpapi -run 'AdminPassword|ForcedPassword' -count=1`; verify failure.**
- [ ] **Step 3: Implement reset service and login gate.** Hash the password immediately, persist only the hash, and prevent all protected routes except own password change while forced change is active.
- [ ] **Step 4: Update both management pages to show the one-time result safely; clear it when the modal closes.**
- [ ] **Step 5: Run focused tests; expect PASS, then commit.**

```powershell
git add backend/internal/httpapi/account_password_admin.go backend/internal/httpapi/account_password_admin_test.go backend/internal/auth/mysql_store.go backend/internal/auth/mysql_store_test.go frontend/src/shared/api/accountAdmin.js frontend/src/shared/api/teamAdmin.js frontend/src/user/pages/AccountRolePage.jsx frontend/src/user/pages/TeamPage.jsx
git commit -m "feat(security): add audited administrator password reset"
```

### Task 4: Correct sessions and the security page

**Files:**
- Modify: `backend/internal/httpapi/auth.go`
- Modify: `backend/internal/httpapi/member_profile.go`
- Modify: `backend/internal/httpapi/member_profile_test.go`
- Modify: `frontend/src/user/pages/SecurityPage.jsx`
- Create: `frontend/src/user/pages/SecurityPage.source.test.js`

**Interfaces:**
- Security overview returns browser, OS, fuzzy IP, issued time, last activity when known, current flag, and stable session identifier; it never invents values.

- [ ] **Step 1: Add failing tests** for User-Agent parsing, fuzzy IP, current device detection, last activity, revoke-other behavior, and absence of a numeric security score.
- [ ] **Step 2: Run Go and Node focused tests; verify failures.**
- [ ] **Step 3: Capture session metadata at login, expose safe session rows, and rebuild the page around password, MFA, Passkey, email, devices and security activity.**
- [ ] **Step 4: Run focused tests plus `npm run build`; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/httpapi/auth.go backend/internal/httpapi/member_profile.go backend/internal/httpapi/member_profile_test.go frontend/src/user/pages/SecurityPage.jsx frontend/src/user/pages/SecurityPage.source.test.js
git commit -m "feat(security): show real account security state"
```
