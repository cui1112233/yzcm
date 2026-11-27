import { Button, Form, Input, Modal, Skeleton, Tag, message } from 'antd';
import { Copy, Fingerprint, KeyRound, Laptop, LockKeyhole, LogOut, Mail, RotateCcwKey, ShieldCheck, Smartphone, TimerReset, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  beginMfaSetup,
  changeOwnPassword,
  disableMfa,
  enableMfa,
  getSecurityOverview,
  revokeOtherSessions,
  rotateMfaRecoveryCodes
} from '../../shared/api/member';
import { listPasskeys, registerPasskey, removePasskey } from '../../shared/api/accountRecovery';
import { getEmailVerificationStatus, requestEmailVerification } from '../../shared/api/accountRecovery';
import { passkeySupported } from '../../shared/webauthn';
import { formatDate, PageHeader, Panel } from './accountCenterShared';

function deviceLabel(session) {
  const parts = [session?.browser, session?.os].filter(Boolean);
  return parts.length ? parts.join(' · ') : '未知设备';
}

export default function SecurityPage() {
  const [loading, setLoading] = useState(true);
  const [security, setSecurity] = useState(null);
  const [passkeys, setPasskeys] = useState([]);
  const [emailStatus, setEmailStatus] = useState(null);
  const [mailing, setMailing] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [mfaOpen, setMfaOpen] = useState(false);
  const [mfaSetup, setMfaSetup] = useState(null);
  const [recoveryCodes, setRecoveryCodes] = useState([]);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();
  const [mfaForm] = Form.useForm();

  async function load() {
    setLoading(true);
    try {
      const [nextSecurity, passkeyResult, nextEmailStatus] = await Promise.all([getSecurityOverview(), listPasskeys().catch(() => ({ passkeys: [] })), getEmailVerificationStatus().catch(() => null)]);
      setSecurity(nextSecurity);
      setPasskeys(passkeyResult.passkeys || []);
      setEmailStatus(nextEmailStatus);
    } catch (error) { message.error(error.message || '安全信息加载失败'); }
    finally { setLoading(false); }
  }

  useEffect(() => { load(); }, []);

  async function changePassword(values) {
    setSaving(true);
    try {
      await changeOwnPassword(values.currentPassword, values.newPassword);
      message.success('密码已修改，其他登录会话已退出');
      setPasswordOpen(false);
      form.resetFields();
      await load();
    } catch (error) { message.error(error.message || '密码修改失败'); }
    finally { setSaving(false); }
  }

  async function revokeOthers() {
    setSaving(true);
    try {
      await revokeOtherSessions();
      message.success('其他登录会话已退出');
      await load();
    } catch (error) { message.error(error.message || '退出其他会话失败'); }
    finally { setSaving(false); }
  }

  async function sendRecoveryVerification() {
    setMailing(true);
    try {
      const result = await requestEmailVerification();
      message.success(`验证邮件已发送到 ${result.email}`);
      setEmailStatus(await getEmailVerificationStatus().catch(() => emailStatus));
    } catch (error) { message.error(error.message || '验证邮件发送失败'); }
    finally { setMailing(false); }
  }

  function addPasskey() {
    let currentPassword = '';
    Modal.confirm({
      title: '绑定当前设备 Passkey',
      content: <div className="ac-passkey-confirm"><p className="ac-muted-copy">为防止已登录设备被他人直接绑定新的强认证凭据，请先再次验证当前密码。验证通过后浏览器会继续请求 Windows Hello、Touch ID、Face ID 或安全密钥确认。</p><Input.Password autoFocus autoComplete="current-password" placeholder="请输入当前密码" onChange={event => { currentPassword = event.target.value; }} /></div>,
      okText: '验证并继续',
      cancelText: '取消',
      async onOk() {
        if (!currentPassword) {
          message.warning('请输入当前密码');
          return Promise.reject(new Error('password required'));
        }
        setSaving(true);
        try {
          await registerPasskey(currentPassword, `Passkey ${passkeys.length + 1}`);
          message.success('Passkey 已绑定，现在登录页可以直接使用');
          await load();
        } catch (error) {
          message.error(error.message || 'Passkey 添加失败');
          return Promise.reject(error);
        } finally {
          setSaving(false);
        }
        return undefined;
      }
    });
  }

  async function deletePasskey(id) {
    setSaving(true);
    try {
      await removePasskey(id);
      message.success('Passkey 已移除');
      await load();
    } catch (error) { message.error(error.message || 'Passkey 删除失败'); }
    finally { setSaving(false); }
  }

  async function startMfa() {
    const currentPassword = mfaForm.getFieldValue('currentPassword');
    if (!currentPassword) return message.warning('先输入当前密码');
    setSaving(true);
    try {
      const setup = await beginMfaSetup(currentPassword);
      setMfaSetup(setup);
      message.success('MFA 密钥已生成，请添加到验证器');
    } catch (error) { message.error(error.message || 'MFA 设置失败'); }
    finally { setSaving(false); }
  }

  async function confirmMfa() {
    const code = mfaForm.getFieldValue('code');
    if (!code) return message.warning('请输入验证器中的 6 位动态码');
    setSaving(true);
    try {
      const result = await enableMfa(code);
      setRecoveryCodes(result.recoveryCodes || []);
      setMfaOpen(false);
      setMfaSetup(null);
      mfaForm.resetFields();
      message.success('MFA 已启用');
      await load();
    } catch (error) { message.error(error.message || '动态码验证失败'); }
    finally { setSaving(false); }
  }

  async function disableMfaNow() {
    const values = await mfaForm.validateFields(['currentPassword', 'code']).catch(() => null);
    if (!values) return;
    setSaving(true);
    try {
      await disableMfa(values.currentPassword, values.code);
      setMfaOpen(false);
      mfaForm.resetFields();
      message.success('MFA 已关闭');
      await load();
    } catch (error) { message.error(error.message || '关闭 MFA 失败'); }
    finally { setSaving(false); }
  }

  async function rotateRecovery() {
    const values = await mfaForm.validateFields(['currentPassword', 'code']).catch(() => null);
    if (!values) return;
    setSaving(true);
    try {
      const result = await rotateMfaRecoveryCodes(values.currentPassword, values.code);
      setRecoveryCodes(result.recoveryCodes || []);
      setMfaOpen(false);
      mfaForm.resetFields();
      message.success('恢复码已重新生成，旧恢复码全部失效');
      await load();
    } catch (error) { message.error(error.message || '恢复码刷新失败'); }
    finally { setSaving(false); }
  }

  const sessionCount = useMemo(() => (security?.sessions?.length || 0) + (security?.persistentSessions?.length || 0), [security]);
  const mfaEnabled = Boolean(security?.mfa?.enabled);
  const hasPasskey = passkeys.length > 0;

  if (loading) return <div className="account-center-page"><Skeleton active paragraph={{ rows: 9 }} /></div>;

  return <div className="account-center-page security-page pc-security">
    <PageHeader title="账号安全" subtitle="管理登录方式、账号找回与已登录设备" />

    <div className="pc-security-grid">
      <Panel title="身份验证" className="pc-security-auth">
        <div className="pc-security-row"><span><KeyRound size={20} /></span><div><strong>登录密码</strong><small>修改后将退出同账号的其他会话</small></div><Tag color="green">已设置</Tag><Button onClick={() => setPasswordOpen(true)}>修改密码</Button></div>
        <div className="pc-security-row"><span><LockKeyhole size={20} /></span><div><strong>二步验证 / MFA</strong><small>动态验证码与一次性恢复码</small></div><Tag color={mfaEnabled ? 'green' : 'gold'}>{mfaEnabled ? '已启用' : '未启用'}</Tag><Button type={mfaEnabled ? 'default' : 'primary'} onClick={() => { setMfaSetup(null); mfaForm.resetFields(); setMfaOpen(true); }}>{mfaEnabled ? '管理 MFA' : '开启 MFA'}</Button></div>
        <div className="pc-security-row pc-security-passkey"><span><Fingerprint size={20} /></span><div><strong>Passkey / 安全密钥</strong><small>使用 Windows Hello、Touch ID、Face ID 或硬件密钥</small></div><Tag color={hasPasskey ? 'green' : 'default'}>{hasPasskey ? `${passkeys.length} 个` : '未绑定'}</Tag><Button disabled={!passkeySupported()} loading={saving} onClick={addPasskey}>添加设备</Button></div>
        {passkeys.length ? <div className="ac-session-list ac-passkey-list">{passkeys.map(item => <div key={item.id}><span className="ac-session-icon"><Fingerprint size={17} /></span><div><strong>{item.name}</strong><small>创建于 {formatDate(item.createdAt)}{item.lastUsedAt ? ` · 最近使用 ${formatDate(item.lastUsedAt)}` : ''}</small></div><Button type="text" danger icon={<Trash2 size={15} />} disabled={saving} onClick={() => deletePasskey(item.id)} /></div>)}</div> : null}
      </Panel>

      <Panel title="账号找回" className="pc-security-recovery">
        <div className="pc-security-recovery-email"><span><Mail size={22} /></span><div><strong>{emailStatus?.email || '尚未设置恢复邮箱'}</strong><small>{emailStatus?.verified ? '邮箱已验证，可用于账号找回' : '验证邮箱后可安全找回账号'}</small></div>{emailStatus?.verified ? <Tag color="green">已验证</Tag> : <Button loading={mailing} disabled={!emailStatus?.email || !emailStatus?.deliveryConfigured} onClick={sendRecoveryVerification}>发送验证</Button>}</div>
        <div className="pc-security-recovery-actions"><a href="/recover?mode=account">忘记账号</a><a href="/recover?mode=password">忘记密码</a></div>
        <p>如果无法使用邮箱，管理员也可以在确认身份后为成员重置密码。</p>
      </Panel>

      <Panel title="当前设备和其他登录设备" className="pc-security-devices" action={<Button danger icon={<LogOut size={15} />} loading={saving} disabled={!security?.otherSessionCount} onClick={revokeOthers}>退出其他设备</Button>}>
        <div className="ac-session-list">
          {(security?.sessions || []).map(session => <div key={`runtime-${session.id}`}><span className="ac-session-icon"><Laptop size={17} /></span><div><strong>{session.current ? `当前设备 · ${deviceLabel(session)}` : deviceLabel(session)}</strong><small>{session.issuedAt ? `登录于 ${formatDate(session.issuedAt)}` : '活动登录会话'}{session.ipHint ? ` · ${session.ipHint}` : ''}</small></div><Tag color={session.current ? 'green' : 'blue'}>{session.current ? '当前设备' : '活动'}</Tag></div>)}
          {(security?.persistentSessions || []).map(session => <div key={`persist-${session.id}`}><span className="ac-session-icon"><TimerReset size={17} /></span><div><strong>{deviceLabel(session)}</strong><small>{session.issuedAt ? `登录于 ${formatDate(session.issuedAt)} · ` : ''}有效期至 {formatDate(session.expiresAt)}{session.ipHint ? ` · ${session.ipHint}` : ''}</small></div><Tag>{session.current ? '当前' : '已记住'}</Tag></div>)}
          {!sessionCount ? <div className="ac-empty">当前仅有本次登录会话</div> : null}
        </div>
      </Panel>

      <Panel title="安全活动" className="pc-security-activity">
        <div className="pc-security-activity-empty"><ShieldCheck size={28} /><strong>未发现异常活动</strong><p>这里仅展示系统能够确认的登录会话；详细安全活动记录暂未提供。</p></div>
      </Panel>
    </div>

    <Modal title="修改登录密码" open={passwordOpen} onCancel={() => setPasswordOpen(false)} onOk={() => form.submit()} okText="确认修改" confirmLoading={saving}>
      <Form form={form} layout="vertical" onFinish={changePassword}>
        <Form.Item label="当前密码" name="currentPassword" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
        <Form.Item label="新密码" name="newPassword" rules={[{ required: true, min: 8, message: '新密码至少 8 位' }]}><Input.Password autoComplete="new-password" /></Form.Item>
        <Form.Item label="确认新密码" name="confirmPassword" dependencies={['newPassword']} rules={[{ required: true }, ({ getFieldValue }) => ({ validator(_, value) { return !value || getFieldValue('newPassword') === value ? Promise.resolve() : Promise.reject(new Error('两次输入的新密码不一致')); } })]}><Input.Password autoComplete="new-password" /></Form.Item>
      </Form>
    </Modal>

    <Modal title={mfaEnabled ? '管理 MFA' : '开启 MFA'} open={mfaOpen} onCancel={() => { setMfaOpen(false); setMfaSetup(null); }} footer={null}>
      <Form form={mfaForm} layout="vertical">
        <Form.Item label="当前密码" name="currentPassword" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
        {!mfaEnabled ? <>
          {!mfaSetup ? <Button block type="primary" loading={saving} onClick={startMfa}>生成验证器密钥</Button> : <>
            <p className="ac-muted-copy">在验证器中添加以下密钥，或复制 otpauth URI 到支持导入的验证器。</p>
            <div className="ac-mfa-secret">{mfaSetup.secret}</div>
            <Button type="text" icon={<Copy size={15} />} onClick={() => navigator.clipboard?.writeText(mfaSetup.otpauthUri).then(() => message.success('otpauth URI 已复制'))}>复制 otpauth URI</Button>
            <Form.Item label="6 位动态验证码" name="code" rules={[{ required: true, pattern: /^\d{6}$/, message: '请输入 6 位动态码' }]}><Input inputMode="numeric" maxLength={6} /></Form.Item>
            <Button block type="primary" loading={saving} onClick={confirmMfa}>验证并启用 MFA</Button>
          </>}
        </> : <>
          <Form.Item label="动态码或恢复码" name="code" rules={[{ required: true }]}><Input autoComplete="one-time-code" /></Form.Item>
          <div className="ac-member-actions">
            <Button icon={<RotateCcwKey size={15} />} loading={saving} onClick={rotateRecovery}>重新生成恢复码</Button>
            <Button danger loading={saving} onClick={disableMfaNow}>关闭 MFA</Button>
          </div>
        </>}
      </Form>
    </Modal>

    <Modal title="请保存一次性恢复码" open={recoveryCodes.length > 0} onCancel={() => setRecoveryCodes([])} footer={<Button type="primary" onClick={() => setRecoveryCodes([])}>我已安全保存</Button>}>
      <p className="ac-muted-copy">每个恢复码只能使用一次。关闭窗口后不会再次显示这些明文恢复码。</p>
      <div className="ac-recovery-grid">{recoveryCodes.map(code => <div className="ac-recovery-code" key={code}>{code}</div>)}</div>
      <Button icon={<Copy size={15} />} onClick={() => navigator.clipboard?.writeText(recoveryCodes.join('\n')).then(() => message.success('恢复码已复制'))}>复制全部</Button>
    </Modal>
  </div>;
}
