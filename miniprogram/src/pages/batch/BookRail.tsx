import { ScrollView, Text, View } from "@tarojs/components";
import type { BatchFactoryBookView } from "../../services/batchFactoryModels";

export type BookRailProps = { books: BatchFactoryBookView[]; selectedBookId?: string; onSelect: (bookId: string) => void };

export default function BookRail({ books, selectedBookId, onSelect }: BookRailProps) {
  return <ScrollView className="mp-batch-book-rail" scrollX>
    <View className="mp-batch-book-rail-inner">{books.map((book) => <View key={book.bookId} className={`mp-batch-book-rail-item${book.bookId === selectedBookId ? " is-active" : ""}`} onClick={() => onSelect(book.bookId)}><Text className="mp-batch-book-rail-title">{book.title || book.bookId}</Text><Text className="mp-batch-book-rail-status">{book.stages.video.label}</Text></View>)}</View>
  </ScrollView>;
}
