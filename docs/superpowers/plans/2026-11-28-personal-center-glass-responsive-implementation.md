# Personal Center Glass and Responsive Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the personal center visual foundation so all approved pages use workbench-consistent neutral glass, a continuous animated energy background, aligned responsive grids, and reliable main-content scrolling.

**Architecture:** Keep existing React page behavior and permissions intact. Consolidate the visual implementation in `account-center-pixel-parity.css`, extend the existing `EnergyScene` primitive with explicit far/mid/near layers, and guard the contract with source tests before browser QA at four viewports.

**Tech Stack:** React, Ant Design, CSS Grid, CSS custom properties, Node `node:test`, Vite, Go embed, authenticated in-app browser QA.

## Global Constraints

- Work directly on canonical branch `v88`; do not create a branch or worktree.
- Preserve all unrelated dirty files and stage only personal-center files.
- Keep the nine approved images in `docs/design/personal-center-20261126/approved/` as the visual targets.
- Preserve real data, permissions, API calls, scrolling behavior, and existing controls.
- Do not use `zoom` for responsive scaling.
- Do not use opaque blue fills for primary glass surfaces.
- Dynamic motion must stop under `prefers-reduced-motion: reduce`.
- Verify locally before any push or production deployment; this plan does not authorize deployment.

---

### Task 1: Lock the neutral-glass and responsive contract with tests

**Files:**
- Modify: `frontend/src/user/pages/personal-center-layout.source.test.js`
- Modify: `frontend/src/user/components/personal-center-visuals.source.test.js`

**Interfaces:**
- Consumes: existing `EnergyScene`, `.pc-shell`, `.pc-main`, and `.pc-glass` contracts.
- Produces: source-level acceptance rules for neutral glass tokens, three scene layers, no `zoom`, main scrolling, and responsive grid breakpoints.

- [ ] **Step 1: Add failing material and layout assertions**

```js
test('personal-center glass stays neutral and responsive without zoom scaling', () => {
  const css = read('shared/styles/account-center-pixel-parity.css');
  assert.match(css, /--pc-glass-1:\s*rgba\(5,\s*9,\s*18,\s*\.2[0-8]\)/);
  assert.match(css, /--pc-glass-2:\s*rgba\(8,\s*13,\s*24,\s*\.3[0-8]\)/);
  assert.doesNotMatch(css, /zoom:\s*(?:1\.125|1\.5)/);
  assert.match(css, /grid-template-columns:\s*repeat\(12,\s*minmax\(0,\s*1fr\)\)/);
});

test('energy scene exposes far, flow and near-particle layers', () => {
  const visuals = read('user/components/PersonalCenterVisuals.jsx');
  for (const layer of ['pc-energy-far', 'pc-energy-flow', 'pc-energy-near']) {
    assert.match(visuals, new RegExp(layer));
  }
});
```

- [ ] **Step 2: Run focused tests and verify they fail**

Run:

```powershell
node --test frontend/src/user/pages/personal-center-layout.source.test.js frontend/src/user/components/personal-center-visuals.source.test.js
```

Expected: FAIL because the new neutral tokens, layer class names, and twelve-column grid do not yet exist and the old CSS still contains `zoom`.

- [ ] **Step 3: Commit only after Tasks 2 and 3 make the assertions pass**

The tests and implementation are committed together after the visual contract is complete so the branch never contains a deliberately failing intermediate commit.

---

### Task 2: Rebuild the shared background and glass material system

**Files:**
- Modify: `frontend/src/user/components/PersonalCenterVisuals.jsx`
- Modify: `frontend/src/shared/styles/account-center-pixel-parity.css`

**Interfaces:**
- Consumes: committed assets `energy-space.webp` and `brand-energy-mark.webp`.
- Produces: `EnergyScene` layers `.pc-energy-far`, `.pc-energy-flow`, `.pc-energy-near`; glass tokens `--pc-glass-1`, `--pc-glass-2`, `--pc-glass-input`, `--pc-glass-hover`; shared `.pc-glass-surface` visual behavior applied through existing selectors.

- [ ] **Step 1: Give the existing energy scene explicit depth layers**

Update `EnergyScene` to keep its current assets while grouping motion semantically:

```jsx
export function EnergyScene({ compact = false }) {
  return <div className={`pc-energy-scene ${compact ? 'is-compact' : ''}`} aria-hidden="true">
    <div className="pc-energy-far"><img className="pc-energy-scene-space" src={energySpace} alt="" draggable="false" /></div>
    <div className="pc-energy-flow"><img className="pc-energy-scene-mark" src={brandEnergyMark} alt="" draggable="false" /></div>
    <div className="pc-energy-near">
      {Array.from({ length: 8 }, (_, index) => <i key={index} className={`pc-energy-particle pc-energy-particle-${index + 1}`} />)}
    </div>
  </div>;
}
```

- [ ] **Step 2: Replace blue surface variables with neutral workbench-aligned tokens**

Define the material tokens at `:root`:

```css
--pc-glass-1: rgba(5, 9, 18, .24);
--pc-glass-2: rgba(8, 13, 24, .34);
--pc-glass-input: rgba(10, 15, 27, .38);
--pc-glass-hover: rgba(255, 255, 255, .07);
--pc-glass-line: rgba(205, 232, 255, .34);
--pc-glass-highlight: rgba(255, 255, 255, .18);
```

Use `--pc-glass-1` for primary cards and sidebar, `--pc-glass-2` for nested cards, and `--pc-glass-input` for inputs and model credential fields. Keep blue only for active navigation, buttons, icon accents, and energy light.

- [ ] **Step 3: Consolidate all primary surface selectors**

Apply one material recipe to `.pc-glass`, `.ac-panel`, `.ac-metric-card`, `.account-role-team-card`, `.pc-team-members`, `.pc-team-ranking`, `.pc-team-audit`, `.pc-accounts-team`, `.pc-api-founder > .settings-api-section`, and `.pc-api-manager > .settings-api-section`:

```css
background: linear-gradient(145deg, rgba(255,255,255,.045), transparent 42%), var(--pc-glass-1);
border-color: var(--pc-glass-line);
box-shadow: inset 0 1px var(--pc-glass-highlight), 0 20px 44px rgba(0, 3, 18, .24);
backdrop-filter: blur(24px) saturate(118%);
```

Remove later selector overrides that restore blue gradients.

- [ ] **Step 4: Implement restrained motion and reduced-motion behavior**

Animate only transforms and opacity:

```css
@keyframes pc-far-drift { to { transform: scale(1.025) translate3d(-.6%, -.35%, 0); } }
@keyframes pc-energy-flow { 50% { transform: translate3d(-2%, 2%, 0) scale(1.025); opacity: .42; } }
@keyframes pc-near-float { 50% { transform: translate3d(18px, -22px, 0) scale(1.25); opacity: .55; } }
```

Under `prefers-reduced-motion: reduce`, set all three scene layer animations and role icon animation to `none !important`.

- [ ] **Step 5: Run focused tests**

Run the Task 1 test command. Expected: energy-layer and neutral-material assertions PASS; the responsive assertions may remain failing until Task 3.

---

### Task 3: Replace zoom scaling with aligned fluid grids

**Files:**
- Modify: `frontend/src/shared/styles/account-center-pixel-parity.css`
- Modify: `frontend/src/user/pages/personal-center-layout.source.test.js`

**Interfaces:**
- Consumes: material tokens and scene layers from Task 2.
- Produces: `.pc-page-grid`, twelve-column page grids, viewport-safe shell, stable main scrolling, aligned paired cards, and responsive rules at 1440, 1180, and 760 pixels.

- [ ] **Step 1: Remove both large-screen zoom media queries**

Delete:

```css
@media (min-width: 1850px) and (max-width: 2199px) { ... zoom: 1.125; }
@media (min-width: 2200px) { ... zoom: 1.5; }
```

Keep `.pc-shell` at `width: 100%; height: 100dvh` for every desktop viewport.

- [ ] **Step 2: Establish the common twelve-column page grid**

```css
.pc-profile-grid,
.pc-security-grid,
.pc-overview-dashboard,
.pc-overview-footer,
.pc-team-workspace,
.pc-platform-analysis,
.pc-platform-directory-grid {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: clamp(14px, 1.15vw, 20px);
  align-items: stretch;
}
```

Assign exact spans:

```css
.pc-profile-basic { grid-column: span 8; }
.pc-profile-side { grid-column: span 4; }
.pc-security-auth, .pc-security-devices { grid-column: span 8; }
.pc-security-recovery, .pc-security-activity { grid-column: span 4; }
.pc-overview-trend { grid-column: span 8; }
.pc-overview-ranking { grid-column: span 4; }
.pc-overview-footer > * { grid-column: span 4; }
.pc-team-members { grid-column: span 8; }
.pc-team-side { grid-column: span 4; }
.pc-platform-trend, .pc-platform-team-table { grid-column: span 8; }
.pc-platform-ranking, .pc-platform-detail { grid-column: span 4; }
```

Set `min-width: 0` on every grid child to prevent overflow. Keep the existing security row order: authentication/recovery in row one and devices/activity in row two.

- [ ] **Step 3: Make dimensions fluid**

Use these fluid ranges: page inline padding `clamp(16px, 1.8vw, 30px)`, page heading `clamp(26px, 2vw, 32px)`, primary card padding `clamp(16px, 1.5vw, 24px)`, identity avatar `clamp(84px, 7vw, 112px)`, and grid gap `clamp(14px, 1.15vw, 20px)`. Remove the fixed `365px` and `355px` side columns by applying the exact eight/four spans above.

- [ ] **Step 4: Add responsive reflow rules**

- At `max-width: 1440px`, retain the eight/four compositions, set page inline padding to 20 pixels, and cap gaps at 16 pixels.
- At `max-width: 1180px`, make profile, security, team, and platform detail regions span all twelve columns; keep the three overview footer cards at four columns while they fit; reduce the sidebar to 190 pixels.
- At `max-width: 760px`, use the compact 72-pixel navigation and make every page region, including all three overview footer cards, span all twelve columns.
- Keep `.pc-main { min-height: 0; overflow-y: auto; overflow-x: hidden; }` in all breakpoints.

- [ ] **Step 5: Run focused tests and verify all pass**

Run:

```powershell
node --test frontend/src/user/pages/personal-center-layout.source.test.js frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/pages/account-center-energy.source.test.js frontend/src/user/pages/ApiConfigPage.source.test.js
```

Expected: all tests PASS.

- [ ] **Step 6: Commit the material, background, responsive CSS, component, and tests**

```powershell
git add -- frontend/src/shared/styles/account-center-pixel-parity.css frontend/src/user/components/PersonalCenterVisuals.jsx frontend/src/user/components/personal-center-visuals.source.test.js frontend/src/user/pages/personal-center-layout.source.test.js
git commit -m "fix(profile): unify glass and responsive layout"
```

---

### Task 4: Build and perform blocking visual QA

**Files:**
- Modify: `docs/acceptance/personal-center-20261128/design-qa.md`
- Create: `docs/acceptance/personal-center-20261128/screenshots/*-neutral-glass.png`

**Interfaces:**
- Consumes: exact committed source from Task 3 and the nine approved references.
- Produces: same-state screenshots and a QA report whose final result is either `passed` or `blocked`; it must never claim success without browser evidence.

- [ ] **Step 1: Run the React production build**

```powershell
Set-Location frontend
npm run build
```

Expected: exit code 0. Existing Vite chunk warnings are recorded but do not fail this visual change.

- [ ] **Step 2: Build the Go embedded local binary**

```powershell
Set-Location ..
.\scripts\build-go-local.ps1
```

Expected: exit code 0 and an updated local Go binary containing the new frontend assets.

- [ ] **Step 3: Open the authenticated local review runtime without touching unrelated 4001 work**

Use a dedicated local review port when 4001 is occupied. Verify `/api/runtime-build-info` identifies the exact Task 3 commit before visual acceptance.

- [ ] **Step 4: Capture and compare all approved states**

Capture account overview, profile, security, team, all-team consumption, manager preset modal, accounts-by-team, manager API main, and founder API main at 1678×937. Compare each screenshot to its matching approved PNG.

- [ ] **Step 5: Verify responsive behavior**

Repeat representative profile, team, and API pages at 1440×900, 1280×800, and 720×900. Check column alignment, equal paired-card boundaries, navigation stability, long-page scrolling, and no horizontal overflow.

- [ ] **Step 6: Update the design QA report**

Record P0/P1/P2/P3 findings in `docs/acceptance/personal-center-20261128/design-qa.md`. Fix every P0/P1/P2 and repeat capture. Set `final result: passed` only after the latest screenshots prove the result; otherwise set `final result: blocked` and state the concrete blocker.

- [ ] **Step 7: Commit acceptance evidence only after QA passes**

```powershell
git add -- docs/acceptance/personal-center-20261128/design-qa.md docs/acceptance/personal-center-20261128/screenshots
git commit -m "test(profile): verify neutral glass visual parity"
```

No push or deployment occurs in this task. User approval of the local visual result remains the release gate.
