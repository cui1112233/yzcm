import { Alert, Button, Checkbox, Input, Modal, Select, Spin } from 'antd';
import { X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { createHydrationGuard } from './hydrationGuard';
import { mergeYZCMSelectionModels, yzcmModelSelectionId } from './apiPresetSelection';

const YZCM_KIND_OPTIONS = [
  { value: 'text', label: '文本' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
  { value: 'audio', label: '配音' },
  { value: 'asr', label: '语音识别' }
];

function RestrictedYZCMPresetModal({ open, selectedModels = [], saving = false, onSearch, onCancel, onConfirm }) {
  const [kind, setKind] = useState('text');
  const [query, setQuery] = useState('');
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState({ models: [], hasMore: false, offset: 0, limit: 50, status: '' });
  const [known, setKnown] = useState([]);
  const [pending, setPending] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const openedRef = useRef(false);
  const requestRef = useRef(0);
  const snapshotRef = useRef('');

  useEffect(() => {
    if (!open) {
      openedRef.current = false;
      requestRef.current += 1;
      return;
    }
    if (openedRef.current) return;
    openedRef.current = true;
    const initial = mergeYZCMSelectionModels(selectedModels, []);
    setKnown(initial);
    setPending(initial.map(yzcmModelSelectionId));
    setKind('text');
    setQuery('');
    setOffset(0);
    snapshotRef.current = '';
  }, [open, selectedModels]);

  useEffect(() => {
    if (!open || !onSearch) return undefined;
    const requestId = ++requestRef.current;
    const timer = setTimeout(() => {
      setLoading(true);
      setError('');
      Promise.resolve(onSearch({ kind, q: query, limit: 50, offset, snapshotId: snapshotRef.current }))
        .then(next => {
          if (requestRef.current !== requestId) return;
          const safe = next || { models: [] };
          setPage(safe);
          setKnown(current => mergeYZCMSelectionModels(current, safe.models));
          snapshotRef.current = String(safe.snapshotId || '');
          if (Number.isInteger(Number(safe.offset))) setOffset(Number(safe.offset));
        })
        .catch(reason => {
          if (requestRef.current !== requestId) return;
          setError(reason?.message || 'YZCM 模型目录读取失败');
          setPage({ models: [], hasMore: false, offset: 0, limit: 50, status: 'error' });
        })
        .finally(() => { if (requestRef.current === requestId) setLoading(false); });
    }, 250);
    return () => clearTimeout(timer);
  }, [open, onSearch, kind, query, offset]);

  const pendingSet = new Set(pending);
  const visibleRows = mergeYZCMSelectionModels([], page.models);
  const selectedRows = pending.map(id => known.find(model => yzcmModelSelectionId(model) === id)).filter(Boolean);

  function toggle(model, checked) {
    const id = yzcmModelSelectionId(model);
    if (!id) return;
    setKnown(current => mergeYZCMSelectionModels(current, [model]));
    setPending(current => (checked
      ? (current.includes(id) ? current : [...current, id])
      : current.filter(item => item !== id)));
  }

  function changeKind(value) {
    setKind(value);
    setOffset(0);
    snapshotRef.current = '';
  }

  return (
    <Modal
      open={open}
      title="自定义预设 · YZCM 模型"
      width={800}
      onCancel={onCancel}
      maskClosable={!saving}
      closable={!saving}
      destroyOnClose
      footer={(
        <div className="api-preset-picker-footer">
          <Button className="api-preset-picker-restore" onClick={() => setPending([])} disabled={saving}>清空展示选择</Button>
          <span className="api-preset-picker-footer-right">
            <Button onClick={onCancel} disabled={saving}>取消</Button>
            <Button type="primary" loading={saving} onClick={() => onConfirm(selectedRows)}>确定</Button>
          </span>
        </div>
      )}
    >
      <p className="api-preset-picker-note">只选择个人中心要展示的 YZCM 模型；不填写 Key、不显示地址，也不会改变模型在业务中的可用状态。</p>
      <div className="api-integration-toolbar">
        <Select value={kind} options={YZCM_KIND_OPTIONS} onChange={changeKind} style={{ width: 140 }} aria-label="模型类型" />
        <Input.Search
          value={query}
          allowClear
          placeholder="搜索模型名称或模型标识"
          onChange={event => { setQuery(event.target.value); setOffset(0); snapshotRef.current = ''; }}
        />
      </div>
      {error ? <Alert type="error" showIcon message={error} /> : null}
      {page.stale ? <Alert type="warning" showIcon message="当前展示的是最近一次可用目录，后台同步恢复后会自动更新。" /> : null}
      <Spin spinning={loading}>
        <div className="api-preset-picker">
          <div className="api-preset-picker-col">
            <div className="api-preset-picker-head"><span>YZCM 模型目录</span><small>{visibleRows.length} 条</small></div>
            {visibleRows.length ? <ul className="api-preset-picker-list">{visibleRows.map(model => {
              const id = yzcmModelSelectionId(model);
              return <li key={id}>
                <Checkbox checked={pendingSet.has(id)} disabled={saving} onChange={event => toggle(model, event.target.checked)}>
                  <span className="api-preset-picker-name">{model.displayName || model.modelId}</span>
                  <span className="api-preset-picker-meta">{model.modelId} · {kind}</span>
                </Checkbox>
              </li>;
            })}</ul> : <p className="api-preset-picker-empty">{loading ? '正在读取目录…' : '没有匹配的 YZCM 模型'}</p>}
            <div className="api-preset-picker-footer">
              <Button size="small" disabled={loading || offset <= 0} onClick={() => setOffset(Math.max(0, offset - Number(page.limit || 50)))}>上一页</Button>
              <Button size="small" disabled={loading || !page.hasMore} onClick={() => setOffset(Number(page.nextOffset ?? (offset + Number(page.limit || 50))))}>下一页</Button>
            </div>
          </div>
          <div className="api-preset-picker-col">
            <div className="api-preset-picker-head"><span>已选展示模型</span><small>{selectedRows.length} 条</small></div>
            {selectedRows.length ? <ul className="api-preset-picker-list is-selected">{selectedRows.map(model => (
              <li key={yzcmModelSelectionId(model)}>
                <span><span className="api-preset-picker-name">{model.displayName || model.modelId}</span><span className="api-preset-picker-meta">{model.modelId} · {model.kind}</span></span>
                <button type="button" className="api-preset-picker-remove" aria-label={`移除预置模型${model.displayName || model.modelId}`} disabled={saving} onClick={() => toggle(model, false)}><X size={14} aria-hidden="true" /></button>
              </li>
            ))}</ul> : <p className="api-preset-picker-empty">还没有选择展示模型。</p>}
          </div>
        </div>
      </Spin>
    </Modal>
  );
}

// 「自定义预设」弹窗：只负责勾选该分组下要展示的预置模型，不填 Key、不改地址。
// 勾选结果由父组件写入服务端 channelSelection。
export default function ApiPresetSelectionModal(props) {
  if (props.mode === 'restricted') return <RestrictedYZCMPresetModal {...props} />;
  return <StaticPresetSelectionModal {...props} />;
}

function StaticPresetSelectionModal({
  open,
  groupTitle = '',
  presets = [],
  selectedIds = [],
  saving = false,
  onCancel,
  onConfirm
}) {
  const [pending, setPending] = useState(selectedIds);
  // 父组件每次渲染都会传入新数组，直接用数组做依赖会把用户刚勾的结果重置掉；
  // 改用内容指纹，并且用户已经动过勾选后不再回灌。
  const guardRef = useRef(null);
  if (!guardRef.current) guardRef.current = createHydrationGuard();
  const guard = guardRef.current;
  const selectedKey = selectedIds.join('\u0000');

  useEffect(() => {
    if (!open) {
      guard.reset();
      return;
    }
    if (!guard.canHydrate()) return;
    setPending(selectedKey ? selectedKey.split('\u0000') : []);
  }, [open, selectedKey, guard]);

  const pendingSet = new Set(pending);
  const selectedPresets = presets.filter(preset => pendingSet.has(preset.id));

  function toggle(id, checked) {
    guard.touch();
    setPending(current => {
      if (checked) return current.includes(id) ? current : [...current, id];
      return current.filter(item => item !== id);
    });
  }

  function restoreDefault() {
    guard.touch();
    setPending(presets.map(preset => preset.id));
  }

  return (
    <Modal
      open={open}
      title={`自定义预设${groupTitle ? ` · ${groupTitle}` : ''}`}
      width={760}
      onCancel={onCancel}
      maskClosable={!saving}
      closable={!saving}
      destroyOnClose
      footer={(
        <div className="api-preset-picker-footer">
          <Button className="api-preset-picker-restore" onClick={restoreDefault} disabled={saving}>初始化/恢复默认</Button>
          <span className="api-preset-picker-footer-right">
            <Button onClick={onCancel} disabled={saving}>取消</Button>
            <Button type="primary" loading={saving} onClick={() => onConfirm(pending)}>确定</Button>
          </span>
        </div>
      )}
    >
      <p className="api-preset-picker-note">
        只选择要展示的预置模型；不改变模型是否可用，填了 Key 的模型仍会进入模型下拉。
      </p>
      <div className="api-preset-picker">
        <div className="api-preset-picker-col">
          <div className="api-preset-picker-head">
            <span>全部预置模型</span>
            <small>{presets.length} 条</small>
          </div>
          <ul className="api-preset-picker-list">
            {presets.map(preset => (
              <li key={preset.id}>
                <Checkbox
                  checked={pendingSet.has(preset.id)}
                  disabled={saving}
                  onChange={event => toggle(preset.id, event.target.checked)}
                >
                  <span className="api-preset-picker-name">{preset.label}</span>
                  <span className="api-preset-picker-meta">
                    {preset.baseUrl || '未填写地址'} · 模型 {preset.model || '未填写'}
                  </span>
                </Checkbox>
              </li>
            ))}
          </ul>
        </div>
        <div className="api-preset-picker-col">
          <div className="api-preset-picker-head">
            <span>已选预置模型</span>
            <small>{selectedPresets.length} 条</small>
          </div>
          {selectedPresets.length ? (
            <ul className="api-preset-picker-list is-selected">
              {selectedPresets.map(preset => (
                <li key={preset.id}>
                  <span className="api-preset-picker-name">{preset.label}</span>
                  <button
                    type="button"
                    className="api-preset-picker-remove"
                    aria-label={`移除预置模型${preset.label}`}
                    disabled={saving}
                    onClick={() => toggle(preset.id, false)}
                  >
                    <X size={14} aria-hidden="true" />
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="api-preset-picker-empty">还没有勾选预置模型；确定后该分组不显示预置卡片。</p>
          )}
        </div>
      </div>
    </Modal>
  );
}
