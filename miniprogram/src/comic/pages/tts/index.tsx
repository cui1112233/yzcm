import Taro from "@tarojs/taro";
import { Button, Picker, Slider, Text, Textarea, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import { ThemedPage } from "../../../theme";
import { DEFAULT_TTS_OPTIONS, TTS_STYLES, TTS_VOICES, readTtsOptions, synthesizeSpeech } from "../../../services/tts";

type AudioContext = ReturnType<typeof Taro.createInnerAudioContext>;

export default function TtsPage() {
  const [input, setInput] = useState("");
  const [voice, setVoice] = useState(TTS_VOICES[0].value);
  const [style, setStyle] = useState(TTS_STYLES[0].value);
  const [speed, setSpeed] = useState(DEFAULT_TTS_OPTIONS.speed || 1.8);
  const [pitch, setPitch] = useState(DEFAULT_TTS_OPTIONS.pitch || 10);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [filePath, setFilePath] = useState("");
  const [playing, setPlaying] = useState(false);
  const audio = useRef<AudioContext | null>(null);
  const version = useRef(0);

  useEffect(() => {
    const current = ++version.current;
    void readTtsOptions()
      .then((options) => {
        if (current !== version.current) return;
        if (options.voice) setVoice(options.voice);
        if (options.style) setStyle(options.style);
        if (typeof options.speed === "number") setSpeed(options.speed);
        if (typeof options.pitch === "number") setPitch(options.pitch);
      })
      .catch(() => undefined);
    return () => { version.current += 1; audio.current?.destroy(); audio.current = null; };
  }, []);

  const stop = () => {
    audio.current?.stop();
    audio.current?.destroy();
    audio.current = null;
    setPlaying(false);
  };

  const generate = async () => {
    if (busy) return;
    if (!input.trim()) { setError("请输入要配音的文字"); return; }
    const current = ++version.current;
    setBusy(true);
    setError("");
    stop();
    setFilePath("");
    try {
      const path = await synthesizeSpeech(input, { voice, style, speed, pitch });
      if (current !== version.current) return;
      setFilePath(path);
      Taro.showToast({ title: "配音已生成", icon: "success" });
    } catch (cause: any) {
      if (current === version.current) setError(cause?.message || "配音生成失败");
    } finally {
      if (current === version.current) setBusy(false);
    }
  };

  const togglePlay = () => {
    if (!filePath) return;
    if (audio.current) {
      if (playing) audio.current.pause(); else audio.current.play();
      setPlaying(!playing);
      return;
    }
    const context = Taro.createInnerAudioContext();
    audio.current = context;
    context.onEnded(() => setPlaying(false));
    context.onPause(() => setPlaying(false));
    context.onError(() => { setPlaying(false); setError("配音文件无法播放"); });
    context.src = filePath;
    context.play();
    setPlaying(true);
  };

  return (
    <ThemedPage className="mp-page">
      <BrandHeader eyebrow="配音" title="独立配音" showBack />
      <Text className="mp-title">独立配音</Text>
      <Text className="mp-subtitle">输入文字，调用账号配音服务生成音频并在本机播放。</Text>
      <View className="mp-card">
        <Text className="mp-field-label">配音文字</Text>
        <Textarea className="mp-textarea" maxlength={-1} value={input} disabled={busy} placeholder="粘贴需要配音的文字" onInput={(event) => setInput(event.detail.value)} />
        <Text className="mp-row-description">当前 {input.length} 字</Text>
      </View>
      <View className="mp-card">
        <Picker range={TTS_VOICES.map((item) => item.label)} value={Math.max(0, TTS_VOICES.findIndex((item) => item.value === voice))} onChange={(event) => setVoice(TTS_VOICES[Number(event.detail.value)]?.value || voice)}>
          <View className="mp-row"><Text className="mp-row-title">音色</Text><Text className="mp-row-description">{TTS_VOICES.find((item) => item.value === voice)?.label || "服务端音色"}</Text></View>
        </Picker>
        <Picker range={TTS_STYLES.map((item) => item.label)} value={Math.max(0, TTS_STYLES.findIndex((item) => item.value === style))} onChange={(event) => setStyle(TTS_STYLES[Number(event.detail.value)]?.value || style)}>
          <View className="mp-row"><Text className="mp-row-title">风格</Text><Text className="mp-row-description">{TTS_STYLES.find((item) => item.value === style)?.label || "服务端风格"}</Text></View>
        </Picker>
        <Text className="mp-field-label">语速 · {speed.toFixed(1)}</Text>
        <Slider min={0.5} max={3} step={0.1} value={speed} onChange={(event) => setSpeed(Number(event.detail.value))} />
        <Text className="mp-field-label">音调 · {pitch}</Text>
        <Slider min={0} max={20} step={1} value={pitch} onChange={(event) => setPitch(Number(event.detail.value))} />
      </View>
      {error ? <Text className="mp-error">{error}</Text> : null}
      <Button className="mp-primary" loading={busy} disabled={busy} onClick={() => { void generate(); }}>生成配音</Button>
      <Button className="mp-secondary" disabled={!filePath} onClick={togglePlay}>{playing ? "暂停" : "播放配音"}</Button>
      {filePath ? <Text className="mp-row-description">已生成到当前设备缓存，可直接播放或重新生成。</Text> : null}
    </ThemedPage>
  );
}
