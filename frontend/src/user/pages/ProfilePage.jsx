import { Avatar, Button, Form, Input, Modal, Skeleton, Tag, message } from 'antd';
import { Camera, CheckCircle2, Mail, Pencil, Phone, ShieldCheck, UserRound } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { getMemberCenter, updateMemberProfile, uploadMemberAvatar } from '../../shared/api/member';
import { getEmailVerificationStatus, requestEmailVerification } from '../../shared/api/accountRecovery';
import { PageHeader, Panel, RoleBadge, avatarFallback, formatDate } from './accountCenterShared';
import { canonicalRoleLabel, mergeProfileMember } from './accountCenterMemberState.js';
import { Link } from '../../shared/components/Link';
import RoleEnergyIcon from '../components/RoleEnergyIcon.jsx';

// 上传接口返回成功只代表文件已落盘；还要确认浏览器真的能取到这张图。
// 取不到时 Antd Avatar 会静默回退到首字母占位，所以这里先回读一次。
function verifyAvatarImage(url) {
  return new Promise((resolve, reject) => {
    if (!url) {
      reject(new Error('头像已保存，但服务端未返回头像地址，请刷新页面重试'));
      return;
    }
    const probe = new Image();
    const timer = window.setTimeout(() => {
      probe.onload = null;
      probe.onerror = null;
      reject(new Error('头像已保存，但图片加载超时，请刷新页面重试'));
    }, 15000);
    probe.onload = () => {
      window.clearTimeout(timer);
      resolve();
    };
    probe.onerror = () => {
      window.clearTimeout(timer);
      reject(new Error('头像已保存，但图片加载失败，请刷新页面重试'));
    };
    probe.src = url;
  });
}

export default function ProfilePage() {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [mailing, setMailing] = useState(false);
  const [editing, setEditing] = useState(false);
  const [center, setCenter] = useState(null);
  const [emailStatus, setEmailStatus] = useState(null);
  const fileRef = useRef(null);

  async function load() {
    setLoading(true);
    try {
      const [result, status] = await Promise.all([
        getMemberCenter(),
        getEmailVerificationStatus().catch(() => null)
      ]);
      setCenter(result);
      setEmailStatus(status);
      form.setFieldsValue({
        displayName: result.member?.displayName,
        bio: result.member?.bio || '',
        phone: result.member?.phone || '',
        email: result.member?.email || '',
        teamTitle: result.member?.teamTitle || ''
      });
    } catch (error) {
      message.error(error.message || '个人资料加载失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, []);

  async function save(values) {
    setSaving(true);
    try {
      const previousEmail = center?.member?.email || '';
      const result = await updateMemberProfile(values);
      setCenter(current => ({ ...current, member: mergeProfileMember(current.member, result.member) }));
      setEditing(false);
      window.dispatchEvent(new CustomEvent('qiantie:profile-updated'));
      if ((result.member?.email || '') !== previousEmail) {
        setEmailStatus(await getEmailVerificationStatus().catch(() => null));
        message.success('个人资料已保存；邮箱发生变化，需要重新验证');
      } else {
        message.success('个人资料已保存');
      }
    } catch (error) {
      message.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  }

  async function sendVerification() {
    setMailing(true);
    try {
      const result = await requestEmailVerification();
      message.success(`验证邮件已发送到 ${result.email}`);
    } catch (error) {
      message.error(error.message || '验证邮件发送失败');
    } finally { setMailing(false); }
  }

  async function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) return message.error('请选择 PNG、JPG 或 WebP 图片');
    if (file.size > 2 * 1024 * 1024) return message.error('头像不能超过 2MB');
    const reader = new FileReader();
    reader.onload = async () => {
      setSaving(true);
      try {
        const result = await uploadMemberAvatar(reader.result);
        const avatarUrl = result?.member?.avatarUrl;
        await verifyAvatarImage(avatarUrl);
        setCenter(current => ({ ...current, member: mergeProfileMember(current.member, result.member) }));
        window.dispatchEvent(new CustomEvent('qiantie:profile-updated'));
        message.success('头像已更新');
      } catch (error) {
        message.error(error.message || '头像上传失败，请重试');
      } finally {
        setSaving(false);
      }
    };
    reader.readAsDataURL(file);
  }

  const member = center?.member;
  if (loading) return <div className="account-center-page"><Skeleton active paragraph={{ rows: 9 }} /></div>;
  if (!member) return <div className="account-center-page"><div className="ac-empty">无法读取资料</div></div>;

  const membershipRole = canonicalRoleLabel(member.canonicalRole);

  return <div className="account-center-page profile-page pc-profile">
    <PageHeader title="个人资料" subtitle="管理您的公开资料与团队名片" />
    <section className="pc-profile-identity" aria-label="个人身份名片">
      <button className="pc-profile-avatar" type="button" onClick={() => fileRef.current?.click()} disabled={saving} aria-label="更换头像"><Avatar size={86} src={member.avatarUrl}>{avatarFallback(member)}</Avatar><i><Camera size={14} /></i></button>
      <div className="pc-profile-name"><div><strong>{member.displayName || member.username}</strong><RoleEnergyIcon role={membershipRole} size={34} /></div><small>@{member.username}</small><span><CheckCircle2 size={15} /> AI 服务已启用</span></div>
      <div className="pc-profile-role"><RoleBadge canonicalRole={member.canonicalRole} /><small>身份由系统分配，不可自行修改</small></div>
      <Button className="pc-profile-avatar-action" loading={saving} onClick={() => fileRef.current?.click()} icon={<Camera size={15} />}>更换头像</Button>
    </section>

    <div className="pc-profile-grid">
      <Panel title="基础资料" className="pc-profile-basic" action={<Button type="text" icon={<Pencil size={15} />} onClick={() => setEditing(true)}>编辑</Button>}>
        <div className="pc-profile-fields">
          <div><span>显示名称</span><strong>{member.displayName || '未填写'}</strong></div>
          <div><span>手机号码</span><strong>{member.phone || '未填写'}</strong></div>
          <div><span>邮箱地址</span><strong>{member.email || '未填写'}</strong>{emailStatus?.verified ? <Tag color="green">已验证</Tag> : member.email ? <Button type="link" loading={mailing} disabled={!emailStatus?.deliveryConfigured} onClick={sendVerification}>发送验证</Button> : null}</div>
          <div><span>团队职务</span><strong>{member.teamTitle || membershipRole}</strong></div>
          <div className="pc-profile-bio"><span>个人简介</span><p>{member.bio || '暂未填写个人简介'}</p></div>
        </div>
        <Button type="primary" icon={<Pencil size={15} />} onClick={() => setEditing(true)}>修改资料</Button>
      </Panel>

      <div className="pc-profile-side">
        <Panel title="账号归属" className="pc-profile-ownership">
          <div className="pc-profile-list"><div><span>登录账号</span><strong>{member.username}</strong></div><div><span>所属团队</span><strong>{center?.team?.name || '未加入团队'}</strong></div><div><span>加入时间</span><strong>{formatDate(member.createdAt).slice(0, 10)}</strong></div><div><span>当前身份</span><RoleBadge canonicalRole={member.canonicalRole} compact /></div></div>
        </Panel>
        <Panel title="资料使用范围" className="pc-profile-visibility">
          <div className="pc-profile-notice"><UserRound size={18} /><p>您的显示名称和头像将用于团队排行、账单记录和协作记录，帮助团队成员识别与协作。</p></div>
          <div className="pc-profile-list"><div><span>手机号码</span><strong>仅自己可见</strong></div><div><span>邮箱地址</span><strong>仅自己可见</strong></div></div>
          <Link href="/security" className="pc-profile-security-link"><ShieldCheck size={16} />前往账号安全</Link>
        </Panel>
      </div>
    </div>
    <input ref={fileRef} className="member-hidden-file" type="file" accept="image/png,image/jpeg,image/webp" onChange={upload} />
    <Modal className="pg-edit-modal" title="编辑个人资料" open={editing} onCancel={() => setEditing(false)} onOk={() => form.submit()} okText="保存修改" confirmLoading={saving}>
      <Form form={form} layout="vertical" onFinish={save}><Form.Item label="显示名称" name="displayName" rules={[{ required: true, whitespace: true, max: 40 }]}><Input maxLength={40} /></Form.Item><Form.Item label="手机号" name="phone"><Input prefix={<Phone size={15} />} maxLength={32} /></Form.Item><Form.Item label="邮箱" name="email"><Input prefix={<Mail size={15} />} maxLength={120} /></Form.Item><Form.Item label="团队职务" name="teamTitle"><Input maxLength={60} /></Form.Item><Form.Item label="个人简介" name="bio"><Input.TextArea rows={4} maxLength={240} showCount /></Form.Item></Form>
    </Modal>
  </div>;
}
