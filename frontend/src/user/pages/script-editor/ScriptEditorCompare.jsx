import { Button, Empty, Select, Tag } from 'antd';
import { useMemo, useState } from 'react';
import { diffScriptLines } from './scriptCompare.js';

export default function ScriptEditorCompare({ scripts, activeId, onMerge }) {
  const [leftId, setLeftId] = useState(activeId || scripts[0]?.id);
  const [rightId, setRightId] = useState(scripts.find(row => row.id !== (activeId || scripts[0]?.id))?.id || scripts[0]?.id);
  const left = scripts.find(row => row.id === leftId), right = scripts.find(row => row.id === rightId);
  const diff = useMemo(() => diffScriptLines(left?.content || '', right?.content || ''), [left?.content, right?.content]);
  if (scripts.length < 2) return <Empty description="至少需要两个剧本版本才能比较" />;
  const options = scripts.map(row => ({ value:row.id, label:`第${row.episode}集 V${row.version} · ${row.title}` }));
  return <div className="script-editor-compare"><header><Select value={leftId} options={options} onChange={setLeftId} /><span>对比</span><Select value={rightId} options={options} onChange={setRightId} /><Button type="primary" disabled={!right} onClick={() => onMerge(right)}>以右侧内容新建版本</Button></header><div className="script-editor-diff">{diff.map((line, index) => <div key={`${index}-${line.type}`} className={`is-${line.type}`}><Tag>{line.type === 'equal' ? ' ' : line.type === 'delete' ? '−' : '+'}</Tag><pre>{line.text || ' '}</pre></div>)}</div></div>;
}
