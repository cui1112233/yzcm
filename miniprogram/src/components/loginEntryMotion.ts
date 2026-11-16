// All distances use the 375px design canvas. Arm sprites retain the original
// character's shoulders; only their length changes as the wrist follows the rim.
const characterScale = 154 / 160 * .335;
const shoulder = 354 * characterScale;
const armLength = (480 - 354) * characterScale;
const actorY = 203, panelY = 317, travel = 93, reach = 44, gripInset = 3;
export const loginEntryMotion = {
  actorY, panelY, travel, reach, gripInset, shoulder, armLength,
  armReached: (panelY + travel + gripInset - actorY - reach - shoulder) / armLength,
  armRaised: (panelY + gripInset - actorY - shoulder) / armLength,
};
