# Liquid Glass Text Readability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the current liquid-glass transparency and refraction while making secondary copy readable over every wallpaper ribbon.

**Architecture:** Add theme-specific semantic contrast tokens in the shared liquid material. Apply those tokens only to secondary copy and restore the function-card copy shield that the shared override currently removes.

**Tech Stack:** Taro 4, React, SCSS, Vitest, WeChat Developer Tools

## Global Constraints

- Do not change wallpaper, Canvas refraction, glass edge optics, or whole-card opacity.
- Business detail pages remain ordinary material.
- Verify both light and dark themes in WeChat Developer Tools.
- Do not commit, upload, or publish.

---

### Task 1: Lock the readability contract

**Files:**
- Modify: `src/ui/liquidGlass.test.ts`
- Modify: `src/ui/liquidGlass.scss`

**Interfaces:**
- Consumes: `.mp-theme.mp-liquid`, `.tools-entry-copy`, existing public-page secondary-copy selectors.
- Produces: `--liquid-readable-muted`, `--liquid-copy-shield`, `--liquid-copy-shadow`.

- [ ] **Step 1: Write the failing style contract test**

Assert that the shared stylesheet contains theme-specific readable-muted and copy-shield tokens, maps `--glass-text-fill` to the shield, applies `font-weight:500` and a text shadow to secondary copy, and does not force `.tools-entry-copy` to a transparent background.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `npm test -- --run src/ui/liquidGlass.test.ts`

Expected: FAIL because the semantic contrast tokens and restored copy shield do not exist.

- [ ] **Step 3: Implement the minimal shared style change**

Add the three theme tokens, use the readable-muted token on secondary-copy selectors, restore the function copy gradient, and raise function section captions/descriptions to `22rpx`.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `npm test -- --run src/ui/liquidGlass.test.ts`

Expected: PASS.

- [ ] **Step 5: Verify generated WeChat styles and visuals**

Run `npm test -- --run`, `npm run build:weapp`, and `git diff --check`. Compile the generated `dist` in WeChat Developer Tools, inspect workspaces/tasks/functions/profile in light and dark themes, and confirm business detail pages do not gain `mp-liquid`.
