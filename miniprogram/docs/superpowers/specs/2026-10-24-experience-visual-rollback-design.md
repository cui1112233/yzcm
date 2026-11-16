# Experience Visual Rollback Design

## Goal

Restore the four native tab pages to the ordinary `v1.0.11` experience-build material while preserving the current login intro animation and all current business behavior.

## Scope

- Disable the liquid wallpaper, refraction Canvas, and liquid-only surface overrides on workspace, tasks, functions, profile, and profile detail pages.
- Keep the current page component trees, API calls, navigation, task loading, profile safeguards, batch features, and media fixes.
- Hide the `降低透明度` preference while liquid glass is disabled so the UI does not expose an inert control.
- Do not modify `LoginScene`, login media loading, login assets, wheel animation, login form behavior, or release version text.

## Approach

Use one reversible public-material feature flag in `src/ui/liquidGlass.ts`. `ThemedPage` remains the single owner of material mounting, so disabling the flag prevents both `LiquidGlassMaterial` and `DynamicGlassCanvas` from mounting without rewriting any business page. Existing liquid classes remain inert because all liquid styles are scoped below `.mp-liquid`.

## Acceptance

- The four tab pages render without `.mp-liquid`, liquid wallpaper, or dynamic refraction Canvas.
- Profile renders as an ordinary tab page and does not show `降低透明度`.
- Login intro source and assets remain byte-for-byte untouched by this rollback.
- Focused unit tests, production WeChat build, and WeChat Developer Tools visual checks pass.

