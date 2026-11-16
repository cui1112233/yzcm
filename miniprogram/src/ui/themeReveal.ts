export function revealRadius(x: number, y: number, width: number, height: number) {
  return Math.ceil(Math.hypot(Math.max(x, width-x), Math.max(y, height-y))) + 1;
}

export function revealClipRadius(_next: 'light' | 'dark', ready: boolean, radius: number) {
  return ready ? radius : 0;
}
