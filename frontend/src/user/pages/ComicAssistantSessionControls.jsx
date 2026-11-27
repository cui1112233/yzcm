import { Button, Input, Modal, Popconfirm, Select, Space } from 'antd';
import { useState } from 'react';

export default function ComicAssistantSessionControls({sessions, current, select, readOnly, sending, manage, create, reload}) {
  const [form, setForm] = useState(null);
  const [saving, setSaving] = useState(false);
  const isDefault = current.id === 'default';
  const open = (action) => setForm({
    action, title: action === 'rename' ? current.title : '',
    visibility: 'private', mode: action === 'child' ? current.mode : 'confirm',
  });
  const setField = (field, value) => setForm((values) => ({...values, [field]: value}));
  async function save() {
    const title = form.title.trim();
    if (!title || saving) return;
    setSaving(true);
    try {
      const saved = form.action === 'rename' ? await manage('rename', title) : await create({title, mode: form.mode, visibility: form.visibility, ...(form.action === 'child' ? {parent_id: current.id} : {})});
      if (saved) setForm(null);
    } finally { setSaving(false); }
  }
  return <>
    <Space wrap>
      <Select aria-label="选择助手会话" value={current.id} onChange={select} style={{minWidth: 180}}
        options={sessions.map(item => ({value: item.id, label: item.title + (item.archived ? '（已归档）' : '')}))}/>
      <Button onClick={reload}>刷新会话</Button>
      {!readOnly && <Button onClick={() => open('new')}>新建会话</Button>}
      {!readOnly && (isDefault || current.active) && <Button onClick={() => open('child')}>创建子会话</Button>}
      {current.can_manage && <Button disabled={!!current.request_status} onClick={() => open('rename')}>改名</Button>}
      {current.can_manage && (current.archived
        ? <Button onClick={() => manage('restore')}>恢复会话</Button>
        : <Popconfirm title="归档这个会话？" description="本会话和下级会话的待确认方案会失效，进行中的回复会停止接收。历史消息会保留。" onConfirm={() => manage('archive')}><Button>归档会话</Button></Popconfirm>)}
      {!isDefault && (current.can_stop || sending) && <Popconfirm title="停止接收这次回复？" description="已发给模型的请求可能已产生费用。" onConfirm={() => manage('stop')}><Button>停止回复</Button></Popconfirm>}
    </Space>
    <Modal title={({new:'新建会话',child:'创建子会话',rename:'会话改名'})[form?.action]} open={!!form}
      okText="保存" cancelText="取消" onCancel={() => !saving && setForm(null)} confirmLoading={saving}
      okButtonProps={{disabled: !form?.title.trim()}} onOk={save}>
      {form && <Space direction="vertical" style={{width:'100%'}}>
        <Input aria-label="会话名称" placeholder="会话名称" maxLength={191} value={form.title} onChange={e => setField('title', e.target.value)}/>
        {form.action !== 'rename' && <>
          <Select aria-label="可见范围" value={form.visibility} onChange={v => setField('visibility',v)} options={[
            {value:'private',label:'仅自己可见'}, {value:'project',label:'作品成员可见', disabled:form.action==='child' && current.visibility==='private'},
          ]}/>
          <Select aria-label="会话模式" value={form.mode} onChange={v => setField('mode',v)} options={[
            {value:'confirm',label:'修改前需要确认',disabled:form.action==='child' && current.mode==='readonly'},
            {value:'readonly',label:'只读问答'},
          ]}/>
        </>}
      </Space>}
    </Modal>
  </>;
}
