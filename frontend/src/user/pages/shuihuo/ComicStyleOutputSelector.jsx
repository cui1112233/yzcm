import { Select } from 'antd';
import './comicStyleOutputs.css';

export default function ComicStyleOutputSelector({ styles=[],value,onChange,disabled=false }) {
 if(styles.length<2)return null;
 return <label className="batch-factory-comic-output-selector"><span>画风</span><Select size="small" value={value} disabled={disabled} aria-label="查看漫画画风" onChange={onChange} options={styles.map(style=>({value:style.presetId,label:style.presetName || style.presetId}))} /></label>;
}
