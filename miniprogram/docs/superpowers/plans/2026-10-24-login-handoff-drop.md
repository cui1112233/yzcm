# Login Handoff Drop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the native-video black frame and hand off into a single 280 ms drop-and-settle animation for the characters and real login card.

**Architecture:** Add a short `handoff` phase between `entering` and `ready`. During handoff, an opaque real-login-scene wrapper is painted above the still-mounted native video while a shared character/card group drops into place; only after 280 ms is the video unmounted and the form enabled.

**Tech Stack:** Taro 4, React, TypeScript, SCSS, Vitest, WeChat Developer Tools.

## Global Constraints

- Keep the existing crash video and media download path unchanged.
- Do not add image/video assets or increase the near-2 MB upload package.
- Keep one real login form; never add a fake form to the video or UI.
- Move the characters and login card together; keep title and version fixed.
- Skip, media error, reduced motion, and inactive-page fallbacks must not wait for the handoff animation.
- Do not change authentication, remembered-session, MFA, release label, or business-page behavior.
- Do not upload or publish a new version without a separate explicit instruction.

---

### Task 1: Add the tested native-video handoff and drop animation

**Files:**
- Modify: `src/components/LoginScene.test.tsx`
- Modify: `src/components/LoginScene.tsx`
- Modify: `src/components/LoginScene.scss`

**Interfaces:**
- Consumes: `LoginSceneProps`, the local `introPath` returned by `loadLoginIntroMedia`, and current phases `loading | entering | ready`.
- Produces: phase union `loading | entering | handoff | ready`; `.nl-login-scene`; `.nl-drop-group`; and `@keyframes nl-handoff-drop`.

- [ ] **Step 1: Write the failing handoff lifecycle test**

Replace the immediate-unmount assertions in the existing approved-video test and add a dedicated lifecycle assertion:

```tsx
video.props.onEnded();
let scene = render();
expect(scene.props.className).toContain("nl-handoff");
expect(findByClass(scene, "nl-intro-video")).toBeDefined();
expect(findByClass(scene, "nl-submit")?.props.disabled).toBe(true);

harness.effects.forEach(effect => effect());
vi.advanceTimersByTime(279);
expect(render().props.className).toContain("nl-handoff");
vi.advanceTimersByTime(1);

scene = render();
expect(scene.props.className).toContain("nl-ready");
expect(findByClass(scene, "nl-intro-video")).toBeUndefined();
expect(findByClass(scene, "nl-submit")?.props.disabled).toBe(false);
```

Keep the source/path/autoplay assertions proving the approved local video is still used.

- [ ] **Step 2: Write the failing visual-contract test**

Read `LoginScene.scss` and assert the structural and motion contract:

```tsx
const scene = render();
expect(findByClass(scene, "nl-login-scene")).toBeDefined();
expect(findByClass(scene, "nl-drop-group")).toBeDefined();
expect(styles).toMatch(/\.nl-login-scene\{[^}]*position:absolute[^}]*background:/);
expect(styles).toMatch(/\.nl-handoff \.nl-login-scene[^}]*z-index:8/);
expect(styles).toMatch(/\.nl-handoff \.nl-drop-group\{animation:nl-handoff-drop 280ms/);
expect(styles).toContain("@keyframes nl-handoff-drop");
expect(styles).toContain("translate3d(0,-28px,0)");
expect(styles).toContain("translate3d(0,-4px,0)");
```

- [ ] **Step 3: Run the focused test and verify RED**

Run:

```bash
npx vitest run src/components/LoginScene.test.tsx
```

Expected: FAIL because `onEnded` still changes directly to `nl-ready`, removes the video, and the new wrapper/classes/keyframes do not exist.

- [ ] **Step 4: Implement the minimal `handoff` state**

In `LoginScene.tsx`, extend the phase type and keep the video mounted during handoff:

```tsx
const [stage, setStage] = useState<"loading" | "entering" | "handoff" | "ready">("loading");

useEffect(() => {
  if (stage !== "handoff") return;
  const timer = setTimeout(() => setStage("ready"), 280);
  return () => clearTimeout(timer);
}, [stage]);

const videoMounted = (stage === "entering" || stage === "handoff") && Boolean(introPath);
```

Render `<Video>` when `videoMounted` is true. Change only `onEnded` to `setStage("handoff")`; keep `onError`, form touch skip, inactivity, reduced motion, and the 5-second safety deadline going directly to `ready`. Show “跳过动画” only in `loading` or `entering`.

- [ ] **Step 5: Add the opaque scene wrapper and shared drop group**

In `LoginScene.tsx`, wrap the current title, actors, ready artwork, wheel, card, version, and effects in:

```tsx
<View className="nl-login-scene">
  <View className="nl-title">...</View>
  <View className="nl-drop-group">
    <View className="nl-originals">...</View>
    <View className="nl-new-art">...</View>
    <View className="nl-card-assembly">...</View>
  </View>
  <View className="nl-wheel-track">...</View>
  <Text className="nl-version">...</Text>
  <View className="nl-effect">...</View>
</View>
```

Keep the wheel and title outside `.nl-drop-group`, so only the characters and card move. Preserve all event handlers and form contents exactly.

- [ ] **Step 6: Implement the cover and one-shot drop styles**

In `LoginScene.scss`, add:

```scss
.nl-login-scene{position:absolute;inset:0;background:radial-gradient(ellipse at 50% 28%,#fffefa,#f5f8fd 50%,#e9f1fc)}
.nl-drop-group{position:absolute;inset:0}
.nl-entering .nl-login-scene{visibility:hidden}
.nl-handoff .nl-login-scene,.nl-ready .nl-login-scene{z-index:8;visibility:visible}
.nl-handoff .nl-originals{display:none}
.nl-handoff .nl-new-art{opacity:1}
.nl-handoff .nl-drop-group{animation:nl-handoff-drop 280ms both}
@keyframes nl-handoff-drop{
  0%{transform:translate3d(0,-28px,0)}
  72%{transform:translate3d(0,0,0)}
  86%{transform:translate3d(0,-4px,0)}
  100%{transform:translate3d(0,0,0)}
}
```

Use an ease-out timing function on the handoff rule. Do not add opacity animation to the native video, because fading a black native surface would reintroduce the defect.

- [ ] **Step 7: Run the focused test and verify GREEN**

Run:

```bash
npx vitest run src/components/LoginScene.test.tsx
```

Expected: all `LoginScene` tests PASS, including the handoff lifecycle and style contract.

- [ ] **Step 8: Run static and full regression verification**

Run:

```bash
npx tsc --noEmit
npm test -- --run
npm run build:weapp
git diff --check
```

Expected: TypeScript passes, the complete Vitest suite passes, Taro generates `dist`, and no whitespace errors are reported.

- [ ] **Step 9: Commit the implementation**

```bash
git add src/components/LoginScene.test.tsx src/components/LoginScene.tsx src/components/LoginScene.scss
git commit -m "fix: smooth login video handoff"
```

### Task 2: Verify the real WeChat transition

**Files:**
- Verify: `dist/components/LoginScene.*` generated equivalents
- Verify: `src/config/release.ts`

**Interfaces:**
- Consumes: generated `dist`, the existing WeChat Developer Tools project, and the current remembered login state.
- Produces: visual acceptance evidence for the simulator; no source API.

- [ ] **Step 1: Confirm build identity before opening the simulator**

Run:

```bash
git status --short
git rev-parse --short=7 HEAD
rg -n "nl-handoff|nl-handoff-drop|280ms" dist
```

Expected: only intended generated-file status, current commit identity is known, and generated output contains the new phase and animation.

- [ ] **Step 2: Compile once in WeChat Developer Tools**

Open the existing project at `/Users/ming/Downloads/小程序开发/yizhanchengming/miniprogram/dist`, choose “普通编译”, and wait for `pages/bind/index` to complete the entrance.

Expected: no WXSS/JavaScript error; the current version label remains visible after the transition.

- [ ] **Step 3: Perform visual and interaction acceptance**

Observe the final video-to-form handoff at normal speed and, if needed, capture frames around the transition. Confirm:

- no black frame appears;
- characters and login card move together from `-28px` to the final position;
- exactly one small `-4px` rebound occurs;
- title and version do not move;
- the real username/password fields and submit button become usable after settling;
- clicking “跳过动画” still reveals an immediately usable form.

Expected: all six checks pass in the simulator. Record true-device verification as pending unless it is separately performed; do not call simulator evidence true-device acceptance.

- [ ] **Step 4: Record final repository state**

Run:

```bash
git status --short
git log -2 --oneline
```

Expected: source changes are committed, no unrelated file is changed, and the design and implementation commits are visible.
