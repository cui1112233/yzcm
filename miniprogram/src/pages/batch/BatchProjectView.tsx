import { Button, Text, View } from "@tarojs/components";
import type { BatchFactoryBatch } from "../../services/batchFactoryV11";
import type { BatchFactoryBookView } from "../../services/batchFactoryModels";
import NovelListView from "./NovelListView";
import AppIcon from "../../components/AppIcon";
import { statusTone } from "../../services/batchFactoryModels";
import BatchCover from "./BatchCover";

export type BatchProjectViewProps = { batch: BatchFactoryBatch; books: BatchFactoryBookView[]; selectedBookId?: string; onBack: () => void; onSelectBook: (bookId: string) => void; onRefresh: () => void; onConfigure: () => void; onProduction: () => void };

function coverFor(batch: BatchFactoryBatch) {
  const source = batch as BatchFactoryBatch & { cover?: { url?: string } };
  return String(batch.coverUrl || source.cover?.url || "").trim();
}

export default function BatchProjectView({ batch, books, selectedBookId, onBack, onSelectBook, onRefresh, onConfigure, onProduction }: BatchProjectViewProps) {
  return <View className="mp-batch-project">
    <Button className="mp-batch-text-button mp-batch-breadcrumb" onClick={onBack}>‹ 我的作品</Button>
    <View className="mp-batch-project-head"><BatchCover className="mp-batch-project-cover" url={coverFor(batch)} batchId={batch.id} /><View className="mp-row-copy"><Text className="mp-batch-project-title">{batch.title || batch.name || "未命名批量"}</Text><Text className="mp-row-description">{books.length} 本小说 · 选择一本进入工作区</Text><Text className={`mp-status-text mp-status-text-${statusTone(batch.status).tone} mp-batch-project-status`}>· {statusTone(batch.status).label}</Text></View><Button className="mp-icon-button" aria-label="刷新作品" onClick={onRefresh}><AppIcon name="refresh-ccw" size={20} /></Button></View>
    <View className="mp-batch-project-toolbar"><Button className="mp-secondary mp-batch-action" onClick={onConfigure}><AppIcon name="sliders" size={17} />统一配置</Button><Button className="mp-primary mp-batch-action" onClick={onProduction}><AppIcon name="timer" size={17} />批量执行与定时</Button></View>
    <NovelListView books={books} onSelect={onSelectBook} />
  </View>;
}
