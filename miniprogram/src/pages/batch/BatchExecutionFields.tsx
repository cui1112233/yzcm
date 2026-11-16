import { Picker, Text, View } from "@tarojs/components";
import type { BatchFactoryAutomationPreset } from "../../services/batchFactoryV11";

const MODES = ["视频生成，不自动上传", "全流程并自动上传", "只生成剧本分镜"];
const MODE_VALUES = ["video_no_submit", "full_submit", "storyboard_only"];
const CONCURRENCY = [1, 2, 4];
export type BatchExecutionFieldsValue = { presetId: string; runMode: string; concurrency: number; scheduleDate: string; scheduleTime: string };

export default function BatchExecutionFields({ value, presets, disabled = false, showSchedule = true, onChange }: { value: BatchExecutionFieldsValue; presets: BatchFactoryAutomationPreset[]; disabled?: boolean; showSchedule?: boolean; onChange: (patch: Partial<BatchExecutionFieldsValue>) => void }) {
  return <View>
    <View className="mp-batch-form-row">
      <Picker disabled={disabled} range={presets.map(item => item.name || item.id)} value={Math.max(0, presets.findIndex(item => item.id === value.presetId))} onChange={event => onChange({ presetId: presets[Number(event.detail.value)]?.id || "" })}><View className="mp-picker-field"><Text className="mp-field-label">自动化预设</Text><Text className="mp-picker-value">{presets.find(item => item.id === value.presetId)?.name || (presets.length ? "请选择预设" : "暂无预设")}</Text></View></Picker>
      <Picker disabled={disabled} range={MODES} value={Math.max(0, MODE_VALUES.indexOf(value.runMode))} onChange={event => onChange({ runMode: MODE_VALUES[Number(event.detail.value)] || "video_no_submit" })}><View className="mp-picker-field"><Text className="mp-field-label">执行范围</Text><Text className="mp-picker-value">{MODES[Math.max(0, MODE_VALUES.indexOf(value.runMode))]}</Text></View></Picker>
    </View>
    <View className="mp-batch-form-row">
      <Picker disabled={disabled} range={CONCURRENCY.map(count => `${count} 本`)} value={Math.max(0, CONCURRENCY.indexOf(value.concurrency))} onChange={event => onChange({ concurrency: CONCURRENCY[Number(event.detail.value)] || 2 })}><View className="mp-picker-field"><Text className="mp-field-label">并发数</Text><Text className="mp-picker-value">{value.concurrency} 本</Text></View></Picker>
      {showSchedule ? <View className="mp-picker-field"><Text className="mp-field-label">定时时区</Text><Text className="mp-picker-value">北京时间 UTC+8</Text></View> : null}
    </View>
    {showSchedule ? <View className="mp-batch-form-row">
      <Picker disabled={disabled} mode="date" value={value.scheduleDate} onChange={event => onChange({ scheduleDate: event.detail.value })}><View className="mp-picker-field"><Text className="mp-field-label">日期</Text><Text className="mp-picker-value">{value.scheduleDate || "选择日期"}</Text></View></Picker>
      <Picker disabled={disabled} mode="time" value={value.scheduleTime} onChange={event => onChange({ scheduleTime: event.detail.value })}><View className="mp-picker-field"><Text className="mp-field-label">时间</Text><Text className="mp-picker-value">{value.scheduleTime || "选择时间"}</Text></View></Picker>
    </View> : null}
  </View>;
}
