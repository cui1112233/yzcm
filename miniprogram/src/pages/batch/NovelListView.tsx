import { Text, View } from "@tarojs/components";
import AppIcon from "../../components/AppIcon";
import type { BatchFactoryBookView } from "../../services/batchFactoryModels";

export type NovelListViewProps = { books: BatchFactoryBookView[]; onSelect: (bookId: string) => void };

export default function NovelListView({ books, onSelect }: NovelListViewProps) {
  return <View className="mp-batch-novel-list"><View className="mp-batch-novel-head"><Text className="mp-section-title">小说列表</Text><Text className="mp-row-description">{books.length} 本</Text></View>{books.map((book, index) => <View className="mp-batch-novel-row" key={book.bookId} role="button" aria-label={`进入${book.title || book.sourceBookId || book.bookId}`} onClick={() => onSelect(book.bookId)}><Text className="mp-batch-novel-index">{String(index + 1).padStart(2, "0")}</Text><View className="mp-row-copy"><Text className="mp-row-title">{book.title || book.sourceBookId || book.bookId}</Text><Text className="mp-row-description">{book.platformName ? `${book.platformName} · ` : ""}{book.sourceBookId || book.bookId}{book.style ? ` · ${book.style}` : ""}</Text><View className="mp-batch-stage-summary"><Text className={`mp-batch-chip mp-batch-chip-${book.stages.source.tone}`}>原文</Text><Text className={`mp-batch-chip mp-batch-chip-${book.stages.storyboard.tone}`}>分镜</Text><Text className={`mp-batch-chip mp-batch-chip-${book.stages.video.tone}`}>视频</Text></View></View><AppIcon name="chevron-right" size={16} /></View>)}</View>;
}
