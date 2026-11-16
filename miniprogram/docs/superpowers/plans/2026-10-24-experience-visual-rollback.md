# Experience Visual Rollback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the ordinary `v1.0.11` experience-build material without changing the current login intro or business features.

**Architecture:** Keep `ThemedPage` as the material boundary and gate liquid material mounting with a single exported feature flag. Hide the liquid-only transparency preference while that flag is disabled.

**Tech Stack:** Taro, React, TypeScript, SCSS, Vitest, WeChat Mini Program Canvas 2D

## Global Constraints

- Do not edit `src/components/LoginScene.tsx`, login media services, or `src/assets/login/**`.
- Do not restore whole historical page files because they contain older business logic.
- Do not commit, upload, or publish from this dirty checkout.

---

### Task 1: Disable Public Liquid Material

**Files:**
- Modify: `src/ui/liquidGlass.ts`
- Test: `src/ui/liquidGlass.test.ts`
- Test: `src/theme.test.ts`

**Interfaces:**
- Produces: `PUBLIC_LIQUID_GLASS_ENABLED: boolean`
- Produces: `liquidEnabled(className: string): boolean`

- [ ] Add failing tests proving all public roots use ordinary material and no refraction renderer mounts.
- [ ] Run focused tests and confirm the old liquid expectations fail.
- [ ] Add the disabled experience-mode flag and gate `liquidEnabled` with it.
- [ ] Run focused tests and confirm they pass.

### Task 2: Hide the Inert Transparency Preference

**Files:**
- Modify: `src/pages/profile/index.tsx`
- Test: `src/pages/profile/profile-palette.test.tsx`

**Interfaces:**
- Consumes: `PUBLIC_LIQUID_GLASS_ENABLED`

- [ ] Add a failing assertion that experience mode does not render `降低透明度`.
- [ ] Guard the liquid-only preference with the public material flag.
- [ ] Run profile and theme tests.

### Task 3: Build and Visual Verification

**Files:**
- Verify only: `src/components/LoginScene.tsx`
- Build output: `dist/**`

- [ ] Record hashes for login intro source and login assets before the change.
- [ ] Run the complete Vitest suite and `npm run build:weapp`.
- [ ] Confirm the login hashes are unchanged.
- [ ] Compile in WeChat Developer Tools and visually verify login intro, workspace, tasks, functions, profile, and navigation.

