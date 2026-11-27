# Personal Center Rollout Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the approved personal center, energy display, four-role authorization, YZCM-only manager model selection, account security, and unified glass UI without regressing the existing Go runtime.

**Architecture:** Implement the shared identity and team authorization boundary first, then build energy, API configuration, and security on that boundary. Finish with the shared visual shell and local 4001 end-to-end acceptance; each child plan is independently reviewable and must land on `v88` before the next begins.

**Tech Stack:** Go 1.26, MySQL migrations, Redis for short-lived locks/rate limits only, React 18, Ant Design 5, Vite 6, Go embed, Node test runner.

## Global Constraints

- Work directly on `v88`; do not create a feature branch for this work.
- Preserve unrelated dirty files and stage only the files named by the active task.
- Four formal role labels are exactly `开发创始`, `开发者`, `管理者`, `组员`.
- `1 能量 = 1 元 YZCM 可用额度`; do not create a second wallet or payment flow.
- YZCM API remains an external relay/console and is not redesigned.
- All authorization is enforced by Go; hidden React controls are not a security boundary.
- Source is authoritative; never copy `frontend/dist` back into source.
- Local 4001 build, restart, runtime identity, authenticated behavior, and visual acceptance must pass before any production release.
- Do not deploy or push unless the user explicitly authorizes it.

---

## Child Plans and Order

1. `2026-11-26-personal-center-access-foundation.md` — roles, teams, grants, account switching, archive and audit.
2. `2026-11-26-personal-center-energy.md` — team balance, consumption, ranking, overview and bills.
3. `2026-11-26-personal-center-api-config.md` — YZCM catalog selection and role-aware Key configuration.
4. `2026-11-26-personal-center-security.md` — protected MFA, recovery email, sessions and admin reset.
5. `2026-11-26-personal-center-glass-acceptance.md` — navigation, identity icons, unified glass UI and local 4001 acceptance.

Each child plan ends with focused tests and a commit. Do not begin a later plan while an earlier plan has failing tests or unresolved authorization review findings.
