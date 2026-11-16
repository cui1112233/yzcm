export function greetingAt(hour: number) {
  return hour < 6 ? '夜深了，注意休息' : hour < 12 ? '早上好，创作者' : hour < 18 ? '下午好，创作者' : '晚上好，创作者';
}
export function boundPosition(x: number, width: number) {
  return Math.max(edgePeekPosition(0, width), Math.min(edgePeekPosition(width, width), x));
}
/** Stage spans 680rpx inside the card. Peek atlas cut is at 102/144rpx,
 * or 42rpx after mirroring; include the card's 34rpx margin + 1rpx border. */
export function edgePeekPosition(x: number, width: number) {
  const scale = width / 680;
  return x < width / 2 ? -77 * scale : width - 67 * scale;
}
export function releasePose(x: number, width: number): 'peek' | 'idle' {
  return x < 8 || x > width - 80 ? 'peek' : 'idle';
}
