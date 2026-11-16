import { Button, Image, Input, Text, Video, View } from "@tarojs/components";
import { useEffect, useState } from "react";
import brand from "../assets/brand-logo-black.png";
import box from "../assets/login/box.png";
import pair from "../assets/login/pair.png";
import tail from "../assets/login/tail.png";
import parts from "../assets/login/parts.png";
import wheel from "../assets/login/wheel.png";
import blinkGirl from "../assets/login/blink-girl.png";
import blinkBoy from "../assets/login/blink-boy.png";
import boy from "../assets/login/boy.png";
import girl from "../assets/login/girl.png";
import boyArms from "../assets/login/boy-arms.png";
import girlArms from "../assets/login/girl-arms.png";
import grips from "../assets/login/grips.png";
import charmBoy from "../assets/login/charm-boy.png";
import charmGirl from "../assets/login/charm-girl.png";
import smoke from "../assets/login/smoke.png";
import AppIcon from "./AppIcon";
import { loginEntryMotion } from "./loginEntryMotion";
import { MINIPROGRAM_VERSION_LABEL } from "../config/release";
import { loadLoginIntroMedia } from "../services/loginIntroMedia";
import { apiUrl } from "../config/runtime";
import "./LoginScene.scss";

// 入场编排的等待窗口只决定“视频能否叠加到 CSS 编排之上”，超过窗口仍会照常播 CSS 编排；
// 窗口必须小于 ENTRANCE_MAX_MS（入场总时长上限），两者不能自相矛盾。
const ENTRANCE_WAIT_MS = 3000;
const ENTRANCE_MAX_MS = 5000;
// 视频放在服务端静态目录，随 __MINIPROGRAM_API_ORIGIN__ 切换环境；文件名是素材 sha256 前 8 位
const introVideo = apiUrl("/assets/login-intro-88258223.mp4");

export type LoginSceneProps = {
  username: string; password: string; mfaCode: string; remember: boolean; automatic: boolean;
  busy: boolean; error: string; active: boolean; reduced: boolean; rememberedSession: boolean;
  onUsername: (value: string) => void; onPassword: (value: string) => void;
  onMfa: (value: string) => void; onRemember: () => void; onAutomatic: () => void; onSubmit: () => void;
};

export default function LoginScene(props: LoginSceneProps) {
  const [loaded, setLoaded] = useState<Record<string, boolean>>({});
  const [introPath, setIntroPath] = useState("");
  const [stage, setStage] = useState<"loading" | "entering" | "ready">("loading");
  const [visiblePassword, setVisiblePassword] = useState(false);
  const [showMfa, setShowMfa] = useState(false);
  const [videoApplied, setVideoApplied] = useState(false);
  useEffect(() => { if (!props.active) setVisiblePassword(false); }, [props.active]);
  useEffect(() => {
    let active = true;
    loadLoginIntroMedia(introVideo)
      .then(path => { if (active) setIntroPath(path); })
      .catch(error => {
        if (!active) return;
        // 视频只是叠加层：失败时仍由等待窗口触发的 CSS 编排接管，这里留痕便于真机排查
        console.warn("登录入场视频加载失败，已降级为 CSS 入场动画", error);
      });
    return () => { active = false; };
  }, []);
  const artReady = ["box", "pair", "boy", "girl", "boyArms", "girlArms", "grips", "smoke", "tail", "blinkGirl", "blinkBoy", "parts"].every(key => loaded[key]);
  useEffect(() => {
    if (stage !== "loading" || !artReady || !introPath) return;
    setVideoApplied(true);
    setStage(props.reduced ? "ready" : "entering");
  }, [stage, artReady, introPath, props.reduced]);
  useEffect(() => {
    if (stage !== "entering") return;
    if (!props.active || props.reduced) { setStage("ready"); return; }
    const timer = setTimeout(() => setStage("ready"), ENTRANCE_MAX_MS);
    return () => clearTimeout(timer);
  }, [stage, props.active, props.reduced]);
  useEffect(() => {
    if (stage !== "loading" || (artReady && introPath)) return;
    // 部分设备本地图片回调不及时；到点后先让 CSS 编排照常开播，
    // 视频若在窗口内就绪会叠加其上（远程 URL 会被微信原生媒体域名策略拦，故必须先落本地）
    const timer = setTimeout(() => {
      setVideoApplied(Boolean(introPath));
      setStage(props.reduced ? "ready" : "entering");
    }, ENTRANCE_WAIT_MS);
    return () => clearTimeout(timer);
  }, [stage, artReady, introPath, props.reduced]);
  const ready = stage === "ready";
  const markLoaded = (key: string) => setLoaded(current => current[key] ? current : { ...current, [key]: true });
  return <View className={`nl-stage nl-${stage} ${!props.active ? "nl-paused" : ""} ${props.reduced ? "nl-reduced" : ""} ${props.error ? "nl-has-error" : ""}`} style={{ "--entry-reach": `${loginEntryMotion.reach * 2}rpx`, "--entry-travel": `${loginEntryMotion.travel * 2}rpx`, "--arm-reached": loginEntryMotion.armReached, "--arm-raised": loginEntryMotion.armRaised, "--arm-shoulder": `${loginEntryMotion.shoulder * 2}rpx` } as any}>
    {stage === "entering" && videoApplied && introPath ? <Video className="nl-intro-video" src={introPath}
      autoplay={stage === "entering" && props.active && !props.reduced}
      paused={stage !== "entering" || !props.active || props.reduced}
      muted controls={false} showPlayBtn={false} showCenterPlayBtn={false} showFullscreenBtn={false}
      objectFit="contain" onEnded={() => setStage("ready")}
      onError={error => { console.warn("登录入场视频播放失败，已切回收尾画面", error); setStage("ready"); }} /> : null}
    {!ready ? <Button className="nl-skip" onClick={() => setStage("ready")}>跳过动画</Button> : null}
    <View className="nl-title"><View className="nl-wordmark" ariaLabel="一战晟铭"><Image src={brand} mode="aspectFit" /><Text>战晟铭</Text></View><Text className="nl-tagline">让想象，成为作品</Text></View>
    <View className="nl-originals">
      {[{ key: "boy", body: boy, arms: boyArms }, { key: "girl", body: girl, arms: girlArms }].map(actor => <View className={`nl-actor nl-${actor.key}`} key={actor.key}>
        <Image src={actor.body} mode="scaleToFill" onLoad={() => markLoaded(actor.key)} onError={() => markLoaded(actor.key)} />
        <Image className="nl-arms" src={actor.arms} mode="scaleToFill" onLoad={() => markLoaded(`${actor.key}Arms`)} onError={() => markLoaded(`${actor.key}Arms`)} />
      </View>)}
    </View>
    <View className="nl-new-art" ariaLabel="会眨眼、摇尾巴的两只小家伙">
      <View className="nl-character-motion">
      <Image className="nl-tail" src={tail} mode="scaleToFill" onLoad={() => markLoaded("tail")} onError={() => markLoaded("tail")} />
      <Image className="nl-pair" src={pair} mode="scaleToFill" onLoad={() => markLoaded("pair")} onError={() => markLoaded("pair")} />
      <Image className="nl-blink nl-blink-girl" src={blinkGirl} mode="scaleToFill" onLoad={() => markLoaded("blinkGirl")} onError={() => markLoaded("blinkGirl")} />
      <Image className="nl-blink nl-blink-boy" src={blinkBoy} mode="scaleToFill" onLoad={() => markLoaded("blinkBoy")} onError={() => markLoaded("blinkBoy")} />
      {props.error ? <><View className="nl-tear nl-tear-girl" /><View className="nl-tear nl-tear-boy" /></> : null}
      </View>
      <Image className="nl-parts" src={parts} mode="scaleToFill" onLoad={() => markLoaded("parts")} onError={() => markLoaded("parts")} />
    </View>
    <View className="nl-wheel-track" ariaHidden>
      <Image className="nl-wheel" src={wheel} mode="aspectFit" />
    </View>
    <View className="nl-card-assembly">
      <Image className="nl-box" src={box} mode="scaleToFill" onLoad={() => markLoaded("box")} onError={() => markLoaded("box")} />
      <View className="nl-inner" />
      {!ready ? <Image className="nl-grips" src={grips} mode="scaleToFill" onLoad={() => markLoaded("grips")} onError={() => markLoaded("grips")} /> : null}
      <View className="nl-form" onTouchStart={() => { if (!ready) setStage("ready"); }}>
        <View className="nl-field"><View className="nl-water"><View /><View /></View><AppIcon name="user-round" size={17} /><Input placeholderStyle="color:#8399ad" ariaLabel="账号" disabled={props.busy || !ready} value={props.username} maxlength={128} placeholder="请输入公网账号" onInput={event => props.onUsername(event.detail.value)} /></View>
        <View className="nl-field nl-password"><View className="nl-water"><View /><View /></View><AppIcon name="key-round" size={17} /><Input placeholderStyle="color:#8399ad" ariaLabel="密码" disabled={props.busy || !ready} password={!visiblePassword} value={props.password} maxlength={1024} placeholder={props.rememberedSession ? "已记住登录，可直接继续" : "请输入密码"} confirmType="go" onConfirm={props.onSubmit} onInput={event => props.onPassword(event.detail.value)} />
          <Button className="nl-eye" ariaLabel={visiblePassword ? "隐藏密码" : "显示密码"} disabled={props.busy || !ready} onClick={() => setVisiblePassword(value => !value)}><View className={`nl-eye-shape ${visiblePassword ? "nl-eye-open" : ""}`}><View /></View></Button>
        </View>
        <View className="nl-options">
          <Button className="nl-option" ariaLabel="记住密码" disabled={props.busy || !ready} onClick={props.onRemember}><View className={`nl-check ${props.remember ? "nl-checked" : ""}`} />记住密码</Button>
          <Button className="nl-option" ariaLabel="自动登录" disabled={props.busy || !ready} onClick={props.onAutomatic}><View className={`nl-check ${props.automatic ? "nl-checked" : ""}`} />自动登录</Button>
        </View>
        <Button className="nl-submit" loading={props.busy} disabled={props.busy || !ready} onClick={props.onSubmit}>{props.busy ? "正在登录" : props.rememberedSession && !props.password ? "继续登录" : "登录，开始创作"}</Button>
      </View>
      <View className="nl-charms"><Image src={charmBoy} /><Image src={charmGirl} /></View>
      <View className="nl-below">
        {props.error ? <Text className="nl-error">{props.error}</Text> : null}
        <Button className="nl-mfa-toggle" disabled={props.busy} onClick={() => setShowMfa(value => !value)}>{showMfa ? "收起二步验证" : "使用二步验证码"}</Button>
        {showMfa ? <Input className="nl-mfa" ariaLabel="二步验证码" disabled={props.busy} type="number" maxlength={8} value={props.mfaCode} placeholder="填写二步验证码" onInput={event => props.onMfa(event.detail.value)} /> : null}
        {props.remember ? <Text className="nl-remember-note">仅在本机保留登录状态</Text> : null}
      </View>
    </View>
    <Text className="nl-version">{MINIPROGRAM_VERSION_LABEL}</Text>
    <View className="nl-effect"><Image className="nl-smoke" src={smoke} mode="scaleToFill" onLoad={() => markLoaded("smoke")} onError={() => markLoaded("smoke")} />{Array.from({length:10},(_,i) => {const angle=i*2.399, radius=(147+i%4*12)*375/390; return <View className={`nl-star nl-star-${i%3}`} key={i} style={{"--sx":`${Math.cos(angle)*62*375/390*2}rpx`,"--sy":`${Math.sin(angle)*62*.65*375/390*2}rpx`,"--dx":`${Math.cos(angle)*radius*2}rpx`,"--dy":`${(Math.sin(angle)*radius*.65-24*375/390)*2}rpx`,"--delay":`${i%3*.045}s` } as any}>★</View>;})}</View>
  </View>;
}
