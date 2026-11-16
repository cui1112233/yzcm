import { Button, Image, ScrollView, Switch, Text, View } from "@tarojs/components";
import Taro, { useDidShow } from "@tarojs/taro";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../components/BrandHeader";
import AppIcon from "../../components/AppIcon";
import ExecutorStatusCard from "../../components/ExecutorStatusCard";
import { buildProfileSections } from "../../config/profile";
import { profileFacts, formatTokenAmount as formatTokens, type PublicMemberCenter } from "../../config/profileData";
import { readMemberProfile } from "../../services/memberProfile";
import { apiUrl } from "../../config/runtime";
import { logoutAccount, hasAccountSession } from "../../services/session";
import { ThemedPage } from "../../theme";
import { uploadProfileAvatar } from "../../services/profileEditor";
import { sessionRevision } from "../../services/sessionBoundary";
import { GLASS_CHANGE, GLASS_PREFERENCE } from '../../ui/liquidGlass';
import { publishGlassLayout, publishGlassScroll } from '../../ui/glassScrollBridge';
import { classifyProfileFailure, type ProfileFailureCategory, type ProfilePhase } from './profileDiagnostics';

const failureLabels: Record<ProfileFailureCategory, string> = {
  none: '',
  'session-missing': '未找到可用登录会话',
  'session-expired': '登录状态已失效',
  timeout: '读取超时，请检查网络后重试',
  domain: '当前网络域名配置不可用',
  network: '网络连接失败，请稍后重试',
  'invalid-data': '服务端未返回完整账号资料',
  unknown: '账号资料暂时无法读取',
};

function hasMember(center: PublicMemberCenter | null) {
  return Boolean(center?.member && typeof center.member === "object" && !Array.isArray(center.member));
}

export default function ProfilePage() {
  const [reducedGlass, setReducedGlass] = useState(() => Taro.getStorageSync(GLASS_PREFERENCE) === true);
  useDidShow(() => setReducedGlass(Taro.getStorageSync(GLASS_PREFERENCE) === true));
  const [center, setCenter] = useState<PublicMemberCenter | null>(null);
  const [diagnostic, setDiagnostic] = useState<{ phase: ProfilePhase; failure: ProfileFailureCategory; elapsedMs: number }>({ phase: 'session-check', failure: 'none', elapsedMs: 0 });
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const nextAttempt = useRef(0);
  const activeAttempt = useRef<number | null>(null);
  const watchdog = useRef<ReturnType<typeof setTimeout>>();
  const mounted = useRef(true);
  const avatarLock = useRef(false);
  useEffect(() => () => {
    clearTimeout(watchdog.current);
    mounted.current = false;
    activeAttempt.current = null;
    nextAttempt.current += 1;
  }, []);

  const load = () => {
    if (!mounted.current || activeAttempt.current !== null) return;
    const attempt = ++nextAttempt.current;
    const revision = sessionRevision();
    const startedAt = Date.now();
    setDiagnostic({ phase: 'session-check', failure: 'none', elapsedMs: 0 });
    if (!hasAccountSession()) {
      setCenter(null);
      setLoading(false);
      setDiagnostic({ phase: 'failed', failure: 'session-missing', elapsedMs: Date.now() - startedAt });
      void Taro.navigateTo({ url: "/pages/bind/index" });
      return;
    }
    activeAttempt.current = attempt;
    setLoading(true);
    setDiagnostic({ phase: 'request-started', failure: 'none', elapsedMs: 0 });
    const current = () => mounted.current
      && activeAttempt.current === attempt
      && sessionRevision() === revision;
    clearTimeout(watchdog.current);
    watchdog.current = setTimeout(() => {
      if (!current()) return;
      activeAttempt.current = null;
      setCenter(null);
      setLoading(false);
      setDiagnostic({ phase: 'failed', failure: 'timeout', elapsedMs: Date.now() - startedAt });
    }, 15000);
    void readMemberProfile()
      .then((result) => {
        if (!current()) return;
        setDiagnostic({ phase: 'response-received', failure: 'none', elapsedMs: Date.now() - startedAt });
        if (!hasMember(result)) {
          setCenter(null);
          setDiagnostic({ phase: 'failed', failure: 'invalid-data', elapsedMs: Date.now() - startedAt });
          return;
        }
        setCenter(result);
        setDiagnostic({ phase: 'rendered', failure: 'none', elapsedMs: Date.now() - startedAt });
      })
      .catch((cause) => {
        if (!current()) return;
        setDiagnostic({ phase: 'failed', failure: classifyProfileFailure(cause), elapsedMs: Date.now() - startedAt });
      })
      .finally(() => {
        clearTimeout(watchdog.current);
        if (!mounted.current || activeAttempt.current !== attempt) return;
        activeAttempt.current = null;
        setLoading(false);
        if (sessionRevision() !== revision) {
          nextAttempt.current += 1;
          setCenter(null);
          setDiagnostic({ phase: 'failed', failure: 'session-expired', elapsedMs: Date.now() - startedAt });
        }
      });
  };

  useDidShow(load);
  useEffect(() => {
    // A native did-show event can be missed while replacing an experience build.
    if (nextAttempt.current === 0) load();
  }, []);

  const cancelAndLogin = () => {
    clearTimeout(watchdog.current);
    activeAttempt.current = null;
    nextAttempt.current += 1;
    setCenter(null);
    setLoading(false);
    void logoutAccount().catch(() => undefined);
    void Taro.reLaunch({ url: '/pages/bind/index' });
  };

  const facts = profileFacts(center);
  const memberLoaded = hasMember(center);
  const displayName = facts.displayName || facts.username;
  const roleLabel = facts.role ? facts.role.toUpperCase() : "服务端未返回角色";
  const serviceLabel = facts.apiEnabled === true ? "已启用" : facts.apiEnabled === false ? "未启用" : "服务端未返回";
  const profileSections = buildProfileSections(facts);
  const openDetail = (key: string) => Taro.navigateTo({ url: `/pages/profile-detail/index?key=${encodeURIComponent(key)}` });

  const chooseAvatar = async () => {
    if (!memberLoaded || avatarLock.current) return;
    avatarLock.current = true;
    const session = sessionRevision();
    try {
      const picked = await Taro.chooseImage({ count: 1, sizeType: ["compressed"], sourceType: ["album", "camera"] });
      const path = picked.tempFilePaths?.[0];
      if (!path) return;
      setUploading(true);
      const file = await new Promise<{ data: string }>((resolve, reject) => Taro.getFileSystemManager().readFile({ filePath: path, encoding: "base64", success: resolve as any, fail: reject }));
      if (session !== sessionRevision()) return;
      const type = path.toLowerCase().endsWith(".png") ? "image/png" : path.toLowerCase().endsWith(".webp") ? "image/webp" : "image/jpeg";
      const result = await uploadProfileAvatar(`data:${type};base64,${file.data}`, facts.username || "");
      if (session !== sessionRevision()) return;
      setCenter(result);
      Taro.showToast({ title: "头像已更新并核对", icon: "success" });
    } catch (cause: any) {
      if (cause?.errMsg?.includes("cancel")) return;
      Taro.showToast({ title: cause?.message || "头像上传失败", icon: "none" });
    } finally {
      setUploading(false);
      avatarLock.current = false;
    }
  };

  useEffect(() => {
    Taro.nextTick?.(() => publishGlassLayout('profile'));
  }, [loading, memberLoaded, center]);

  return (
    <ThemedPage className="mp-page mp-profile-page">
      <BrandHeader eyebrow="" title="我" />
      <ScrollView className="mp-profile-scroll" scrollY scrollWithAnimation={false} onScroll={event => publishGlassScroll('profile', event.detail.scrollTop)}>
        {loading ? <View className="mp-card mp-profile-state"><Text className="mp-row-title">正在读取账号资料</Text><Text className="mp-row-description">正在连接服务端，最多等待 15 秒；失败后可重试。</Text><Button className="mp-secondary mp-profile-cancel" onClick={cancelAndLogin}>取消并返回登录</Button></View> : null}
        {!loading && !memberLoaded ? <View className="mp-card mp-profile-state"><Text className="mp-row-title">无法读取当前账号资料</Text><Text className="mp-error">{failureLabels[diagnostic.failure]}</Text><View className="mp-profile-actions"><Button className="mp-primary mp-profile-retry" onClick={load}>重新读取</Button><Button className="mp-secondary mp-profile-cancel" onClick={cancelAndLogin}>返回登录</Button></View></View> : null}

        {memberLoaded ? <>
          <View className="mp-profile-hero liquid-glass--primary" data-glass-role="primary">
            <View className="mp-profile-avatar" onClick={chooseAvatar}>{facts.avatarUrl ? <Image className="mp-profile-avatar-image" src={apiUrl(facts.avatarUrl)} mode="aspectFill" /> : <Text>{displayName ? displayName.slice(0, 1).toUpperCase() : "?"}</Text>}<View className="mp-profile-camera">{uploading ? <Text className="mp-profile-camera-progress">上传中</Text> : <AppIcon name="camera" size={15} />}</View></View>
            <View className="mp-profile-identity">
              <View className="mp-profile-name-line"><Text className={displayName ? "mp-profile-name" : "mp-profile-name mp-missing-data"}>{displayName || "服务端未返回名称"}</Text><Text className="mp-profile-role">{roleLabel}</Text></View>
              <Text className={facts.username ? "mp-profile-handle" : "mp-profile-handle mp-missing-data"}>{facts.username ? `@${facts.username}` : "服务端未返回用户名"}</Text>
              <Text className="mp-profile-meta">{facts.teamName || "服务端未返回团队"} · AI 服务 {serviceLabel}</Text>
            </View>
          </View>

          <Button className="mp-secondary" onClick={() => openDetail("level")}>我的等级与成长</Button>
          <View className="mp-usage-card liquid-glass--secondary" data-glass-role="secondary" onClick={() => openDetail("usage")}>
            <View className="mp-usage-topline"><Text className="mp-usage-title">本月用量</Text><View className="mp-usage-link"><Text>查看详情</Text><AppIcon name="chevron-right" size={14} /></View></View>
            <View className="mp-usage-values"><View><Text className="mp-usage-label">已使用 Token</Text><Text className={facts.monthlyTokenUsage === undefined ? "mp-usage-number mp-missing-data" : "mp-usage-number"}>{formatTokens(facts.monthlyTokenUsage)}</Text></View><View><Text className="mp-usage-label">月度额度</Text><Text className={facts.monthlyTokenLimit === undefined ? "mp-usage-number mp-missing-data" : "mp-usage-number"}>{formatTokens(facts.monthlyTokenLimit)}</Text></View></View>
            <Text className="mp-usage-caption">仅展示账号中心实际返回的用量与额度</Text>
          </View>

          <ExecutorStatusCard />

          {profileSections.map((section) => (
            <View className="mp-profile-section liquid-glass--list" key={section.key}>
              <View className="mp-profile-section-head"><Text>{section.title}</Text>{section.note ? <Text>{section.note}</Text> : null}</View>
              {section.key === "service" ? <View className="liquid-preference">
                <View><Text className="mp-profile-item-title">降低透明度</Text><Text className="mp-profile-item-description">增强界面文字与背景的对比度</Text></View>
                <Switch ariaLabel="降低透明度" checked={reducedGlass} color="#53788c" onChange={event => {
                  const enabled = event.detail.value;
                  Taro.setStorageSync(GLASS_PREFERENCE, enabled);
                  setReducedGlass(enabled);
                  Taro.eventCenter?.trigger(GLASS_CHANGE);
                }} />
              </View> : null}
              {section.items.map((item) => (
                <Button className="mp-profile-item" key={item.key} onClick={() => openDetail(item.key)}>
                  <View className="mp-profile-item-icon"><AppIcon name={item.icon} size={21} /></View><View className="mp-profile-item-copy"><Text className="mp-profile-item-title">{item.title}</Text><Text className="mp-profile-item-description">{item.description}</Text></View><AppIcon name="chevron-right" size={18} className="mp-profile-item-arrow" />
                </Button>
              ))}
            </View>
          ))}
          {diagnostic.failure !== 'none' ? <View className="mp-profile-inline-error"><Text className="mp-error">{failureLabels[diagnostic.failure]}</Text><View className="mp-profile-actions"><Button className="mp-profile-retry" onClick={load}>重新读取</Button><Button className="mp-profile-cancel" onClick={cancelAndLogin}>返回登录</Button></View></View> : null}
        </> : null}
        {hasAccountSession() ? <Button className="mp-profile-logout" onClick={cancelAndLogin}>退出登录</Button> : null}
        <View className="mp-profile-bottom-space" />
      </ScrollView>
    </ThemedPage>
  );
}
