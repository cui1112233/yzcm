import { Button, Input, Text, View } from "@tarojs/components";
import { useState } from "react";
import AppIcon from "../../components/AppIcon";
import { statusTone } from "../../services/batchFactoryModels";
import type { BatchFactoryReadState } from "../../services/batchFactoryModels";
import type { BatchFactoryBatch } from "../../services/batchFactoryV11";
import { batchBookCount } from "../../services/batchFactoryV11";
import BatchCover from "./BatchCover";

export type PersonalWorksViewProps = {
  readState: BatchFactoryReadState;
  error?: string;
  batches: BatchFactoryBatch[];
  onCreate: () => void;
  onOpen: (batchId: string) => void;
  onRetry: () => void;
};

function coverFor(batch: BatchFactoryBatch) {
  const source = batch as BatchFactoryBatch & { coverUrl?: string; cover?: { url?: string } };
  return String(source.coverUrl || source.cover?.url || "").trim();
}
function progressFor(batch: BatchFactoryBatch) {
  const books = Array.isArray(batch.books) ? batch.books : [];
  const total = Number(batch.bookCount ?? books.length);
  const states = books.map(book => statusTone(book.status));
  if (!total || total !== books.length || states.some(state => state.tone === "unknown")) return null;
  const completed = states.filter(state => state.tone === "success").length;
  return { completed, total, percent: Math.round((completed / total) * 100) };
}
export default function PersonalWorksView({ readState, batches, error, onCreate, onOpen, onRetry }: PersonalWorksViewProps) {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [searchOpen, setSearchOpen] = useState(false);
  const visible = batches.filter((batch) => `${batch.title || batch.name || ""} ${batch.id}`.toLowerCase().includes(query.trim().toLowerCase()) && (filter === "all" || statusTone(batch.status).tone === filter));
  return <View className="mp-batch-works">
    <View className="mp-batch-works-heading">
      <View><Text className="mp-batch-works-title">我的作品<Text className="mp-batch-heading-count">{batches.length}</Text></Text><Text className="mp-row-description">故事的每一步制作，都在这里。</Text></View>
      <Button className="mp-batch-icon-button" aria-label="搜索作品" aria-expanded={searchOpen} onClick={() => setSearchOpen(value => !value)}><AppIcon name="search" size={20} /></Button>
    </View>
    <Button className="mp-batch-new-entry" onClick={onCreate}><View><Text className="mp-batch-new-title">新建批量</Text><Text className="mp-batch-new-description">导入小说 · 统一配置 · 自动制作</Text></View><AppIcon name="plus" size={22} /></Button>
    {searchOpen ? <Input className="mp-batch-search" value={query} placeholder="搜索作品名称 / 编号" onInput={(event) => setQuery(event.detail.value)} /> : query ? <Button className="mp-batch-text-button" onClick={() => { setSearchOpen(true); }}>搜索：{query} · 修改</Button> : null}
    <View className="mp-batch-section-label"><Text>个人作品</Text><Text className="mp-row-description">{batches.length} 个批量作品</Text></View>
    <View className="mp-batch-filter-row">{[["all", "全部"], ["info", "制作中"], ["success", "已完成"], ["danger", "异常"]].map(([key, label]) => <Button key={key} aria-pressed={filter === key} className={filter === key ? "mp-batch-filter is-selected" : "mp-batch-filter"} onClick={() => setFilter(key)}>{label}</Button>)}</View>
    {readState === "loading" ? <View className="mp-batch-state"><Text className="mp-row-title">正在读取个人作品</Text><Text className="mp-row-description">正在从当前连接的服务端读取作品。</Text></View> : null}
    {readState === "error" ? <View className="mp-batch-state mp-batch-state-error"><Text className="mp-row-title">个人作品暂时无法读取</Text><Text className="mp-row-description">{error || "服务端没有返回有效作品列表。"}</Text><Button className="mp-secondary" onClick={onRetry}>重新读取</Button></View> : null}
    {readState === "unavailable" ? <View className="mp-batch-state"><Text className="mp-row-title">作品列表不可用</Text><Text className="mp-row-description">当前账号没有读取批量作品的能力。</Text></View> : null}
    {readState === "empty" ? <View className="mp-batch-state mp-batch-state-empty"><AppIcon name="factory" size={28} /><Text className="mp-row-title">还没有批量作品</Text><Text className="mp-row-description">新建批量后会在这里显示你的作品。</Text><Button className="mp-primary" onClick={onCreate}>新建批量</Button></View> : null}
    {readState === "ready" ? <View className="mp-batch-work-list">
      {visible.map((batch) => {
        const cover = coverFor(batch);
        const tone = statusTone(batch.status);
        const progress = progressFor(batch);
        return <View className="mp-batch-work-row" key={batch.id} onClick={() => onOpen(batch.id)}>
          <View className="mp-batch-work-cover"><BatchCover url={cover} batchId={batch.id} /></View>
          <View className="mp-row-copy"><Text className="mp-row-title">{batch.title || batch.name || "未命名批量"}</Text><Text className="mp-row-description">{batchBookCount(batch)} 本小说</Text><Text className={`mp-status-text mp-status-text-${tone.tone} mp-batch-work-status`}>· {tone.label}{progress ? ` · ${progress.completed}/${progress.total}` : ""}</Text>{progress ? <View className="mp-batch-progress" aria-label={`完成 ${progress.completed} / ${progress.total}`}><View className="mp-batch-progress-value" style={{ width: `${progress.percent}%` }} /></View> : null}</View>
          <AppIcon name="chevron-right" size={16} />
        </View>;
      })}
    </View> : null}
    {readState === "ready" && !visible.length ? <View className="mp-batch-state"><Text className="mp-row-description">没有符合条件的作品，请调整搜索或筛选。</Text></View> : null}
  </View>;
}
