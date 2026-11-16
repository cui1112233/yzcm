import { expect, test } from "vitest";
import { loginEntryMotion } from "./loginEntryMotion";

test("wrists stay on the panel rim throughout the lift, with shoulders attached to the body", () => {
  const m = loginEntryMotion;
  for (let step = 0; step <= 100; step++) {
    const rise = step / 100;
    const bodyY = m.actorY + m.reach * (1 - rise);
    const panelY = m.panelY + m.travel * (1 - rise);
    const armScale = m.armReached + (m.armRaised - m.armReached) * rise;
    const wristY = bodyY + m.shoulder + m.armLength * armScale;
    expect(wristY).toBeCloseTo(panelY + m.gripInset, 6);
    expect(armScale).toBeGreaterThan(0);
    expect(armScale).toBeLessThan(1.4);
  }
});
