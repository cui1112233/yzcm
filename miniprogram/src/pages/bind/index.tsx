import Taro, { useDidHide, useDidShow } from "@tarojs/taro";
import { useEffect, useRef, useState } from "react";
import { hasAccountSession, loginAccount, readAccountSession } from "../../services/session";
import { readLoginPreferences, saveLoginPreferences } from "../../services/loginPreferences";
import BrandHeader from "../../components/BrandHeader";
import LoginScene from "../../components/LoginScene";
import { ThemedPage } from "../../theme";

export default function BindPage() {
  const [preferences] = useState(readLoginPreferences);
  const [username, setUsername] = useState(preferences.username);
  const [password, setPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [remember, setRemember] = useState(preferences.remember);
  const [automatic, setAutomatic] = useState(preferences.automatic);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [active, setActive] = useState(true);
  const busyRef = useRef(false);
  const activeRef = useRef(true);
  const attemptedAuto = useRef(false);
  const generation = useRef(0);
  const rememberedSession = remember && username.trim() === preferences.username && hasAccountSession();

  useDidShow(() => { activeRef.current = true; setActive(true); });
  useDidHide(() => {
    activeRef.current = false;
    generation.current += 1;
    busyRef.current = false;
    setBusy(false); setActive(false); setPassword(""); setMfaCode("");
  });
  useEffect(() => () => { activeRef.current = false; generation.current += 1; }, []);

  async function submit(resume = false) {
    if (busyRef.current || !activeRef.current) return;
    busyRef.current = true;
    const attempt = ++generation.current;
    setBusy(true); setError("");
    try {
      if (resume || (!password && rememberedSession)) {
        const session = await readAccountSession();
        if (session.account.username !== username.trim()) throw new Error("登录身份已变化，请重新输入账号密码");
      } else {
        await loginAccount(username, password, mfaCode, { remember });
      }
      if (!activeRef.current || attempt !== generation.current) return;
      saveLoginPreferences({ username, remember, automatic });
      setPassword(""); setMfaCode("");
      await Taro.switchTab({ url: "/pages/workspace/index" });
    } catch (cause) {
      if (activeRef.current && attempt === generation.current) setError(cause instanceof Error ? cause.message : "账号登录失败，请重试");
    } finally {
      if (activeRef.current && attempt === generation.current) { busyRef.current = false; setBusy(false); }
    }
  }
  useEffect(() => {
    if (attemptedAuto.current) return;
    attemptedAuto.current = true;
    if (preferences.automatic && preferences.username && hasAccountSession()) void submit(true);
  }, []);

  return <ThemedPage className="mp-page native-login-page">
    <BrandHeader title="账号登录" showBack />
    <LoginScene username={username} password={password} mfaCode={mfaCode}
      remember={remember} automatic={automatic} busy={busy} error={error}
      active={active} reduced={false} rememberedSession={rememberedSession}
      onUsername={setUsername} onPassword={setPassword} onMfa={setMfaCode}
      onRemember={() => { setRemember(value => !value); setAutomatic(false); }}
      onAutomatic={() => { setAutomatic(value => !value); setRemember(true); }}
      onSubmit={() => { void submit(); }} />
  </ThemedPage>;
}
