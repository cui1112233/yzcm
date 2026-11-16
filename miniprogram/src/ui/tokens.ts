/** Shared by native chrome and each native page root. Values are logical CSS px. */
const palettes = {
  light: {
    canvas: "#FBFAF7", surface: "#EFEEE8", surfaceRaised: "#FFFFFF", accentSoft: "#E7EEE6", text: "#242C29", textMuted: "#626D64",
    divider: "#E1E2D9", accent: "#315C4C", onAccent: "#FFFFFF", controlBorder: "#7A857C",
    disabledSurface: "#E7E8DF", disabledText: "#59645B", success: "#416957", warning: "#805200",
    error: "#B42318", info: "#175CD3",
  },
  dark: {
    canvas: "#141517", surface: "#202225", surfaceRaised: "#292C30", accentSoft: "#362722", text: "#F2F1ED", textMuted: "#A2A5A8",
    divider: "#303236", accent: "#FA825F", onAccent: "#26180F", controlBorder: "#7E8792",
    disabledSurface: "#292C30", disabledText: "#A2A5A8", success: "#A2C5AC", warning: "#F2C66D",
    error: "#FF9A91", info: "#9AC2FF",
  },
} as const;

export function uiPalette(theme: "light" | "dark") { return palettes[theme]; }

export function uiVariables(theme: "light" | "dark", variant?: "studio"): Record<string, string> {
  const colors = Object.fromEntries(Object.entries(uiPalette(theme)).map(([key, value]) => [`--ui-${key}`, value]));
  return {
    ...colors,
    "--ui-space-1": "4px", "--ui-space-2": "8px", "--ui-space-3": "12px",
    "--ui-space-4": "16px", "--ui-space-6": "24px", "--ui-space-8": "32px",
    "--ui-page-size": "24px", "--ui-page-line": "32px",
    "--ui-section-size": "18px", "--ui-section-line": "26px",
    "--ui-body-size": "16px", "--ui-body-line": "26px",
    "--ui-control-size": "14px", "--ui-control-line": "20px",
    "--ui-caption-size": "12px", "--ui-caption-line": "18px",
    "--ui-radius": "8px", "--ui-radius-panel": "16px",
    "--ui-feedback": "120ms", "--ui-ease": "cubic-bezier(.2,0,0,1)",
    ...(variant === "studio" ? {
      "--ui-page-size": "26px", "--ui-page-line": "34px",
      "--ui-section-size": "16px", "--ui-section-line": "24px",
      "--ui-body-size": "14px", "--ui-body-line": "24px",
      "--ui-control-size": "13px", "--ui-control-line": "20px",
    } : {}),
  };
}
