import { View } from "@tarojs/components";

const STARS = [
  [7, 12, 1], [18, 7, 0.6], [29, 22, 1.2], [43, 10, 0.8],
  [56, 17, 1.1], [71, 8, 0.7], [84, 24, 1.4], [94, 13, 0.8],
  [11, 38, 0.7], [25, 48, 1.1], [39, 34, 0.8], [53, 55, 1.3],
  [67, 42, 0.7], [79, 62, 1.1], [91, 51, 0.8], [16, 76, 1.2],
  [34, 86, 0.7], [49, 72, 1], [63, 92, 0.8], [88, 82, 1.2],
] as const;

export default function AmbientStarfield() {
  return (
    <View className="mp-ambient-starfield" aria-hidden="true">
      <View className="mp-nebula mp-nebula-a" />
      <View className="mp-nebula mp-nebula-b" />
      {STARS.map(([left, top, size], index) => (
        <View
          className="mp-star"
          key={`star-${index}`}
          style={{
            left: `${left}%`,
            top: `${top}%`,
            width: `${size * 2}rpx`,
            height: `${size * 2}rpx`,
            animationDelay: `${(index % 7) * -0.8}s`,
          }}
        />
      ))}
    </View>
  );
}
