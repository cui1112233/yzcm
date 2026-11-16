import Taro, { useRouter } from "@tarojs/taro";
import { Button, Text, View } from "@tarojs/components";
import { useEffect, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import { ThemedPage } from "../../../theme";
import ComicStickySpacer from "../../components/ComicStickySpacer";
import type { BatchFactoryAutomationStatus } from "../../../services/batchFactoryV11";
import { comicProgress, loadComicBooks, readComicAutomation, type ComicBookOption } from "../../services/comicFlow";
import { useComicPolling } from "./useComicPolling";
import "../../styles/comic.scss";

const BOOK_STATUS: Record<string, string> = {
  idle: "等待开始", pending: "排队中", queued: "排队中", running: "生成中", processing: "生成中",
  succeeded: "已出图", completed: "已出图", failed: "失败", cancelled: "已取消", canceled: "已取消", partial: "部分完成", skipped: "已跳过",
};

function statusLabel(value: string | undefined) {
  const key = String(value || "").toLowerCase();
  return BOOK_STATUS[key] || "处理中";
}

function elapsedText(startedAt: number) {
  const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
  if (seconds < 60) return "不到 1 分钟";
  const minutes = Math.floor(seconds / 60);
  return `${minutes} 分钟`;
}

export default function ComicTaskPage() {
  const router = useRouter();
  const batchId = String(router.params?.batchId || "");
  const bookIds = String(router.params?.bookIds || "").split(",").filter(Boolean);
  const [status, setStatus] = useState<BatchFactoryAutomationStatus | null>(null);
  const [titles, setTitles] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [finished, setFinished] = useState(false);
  const [startedAt] = useState(() => Date.now());

  const publish = (value: BatchFactoryAutomationStatus | null) => {
    setStatus(value);
    if (comicProgress(value).finished) setFinished(true);
  };
  const fail = (cause: unknown) => {
    setError(cause instanceof Error ? cause.message : "暂时读不到进度，正在自动重试");
  };

  useComicPolling(!finished, batchId, () => readComicAutomation(batchId), publish, fail);

  useEffect(() => {
    if (!batchId) return;
    let alive = true;
    void loadComicBooks(batchId).then((list: ComicBookOption[]) => {
      if (alive) setTitles(Object.fromEntries(list.map((book) => [book.id, book.title])));
    }).catch(() => { /* 书名读不到时用「第 N 本」，不影响进度展示。 */ });
    return () => { alive = false; };
  }, [batchId]);

  const refresh = () => {
    setError("");
    void readComicAutomation(batchId).then(publish).catch(fail);
  };

  const progress = comicProgress(status);
  const books = status?.books || [];
  const completedBook = books.find((book) => ["succeeded", "completed"].includes(String(book.status || "")))?.bookId;
  const resultBookId = completedBook || bookIds[0] || "";
  const percent = progress.total ? Math.min(100, Math.round((progress.succeeded / progress.total) * 100)) : 0;
  const runningBook = books.find((book) => ["running", "queued", "processing", "pending"].includes(String(book.status || "").toLowerCase()));
  const nameOf = (bookId: string | undefined, index: number) => titles[String(bookId || "")] || `第 ${index + 1} 本`;
  const failedBooks = books.filter((book) => String(book.status || "").toLowerCase() === "failed");
  const canOpenResult = Boolean(resultBookId) && progress.succeeded > 0;
  const waiting = !status && !error;

  return (
    <ThemedPage className="mp-page comic-page">
      <BrandHeader eyebrow="漫画" title="生成中" showBack />
      <Text className="mp-title">{finished ? "漫画做好了" : "正在生成漫画"}</Text>
      <Text className="mp-subtitle">{finished ? "可以一页页看，也可以打包下载。" : "可以退出这一页，生成会继续；回来再看进度。"}</Text>

      {waiting ? <View className="mp-card">
        <Text className="mp-row-title">正在准备…</Text>
        <Text className="mp-row-description">正在读取生成进度，马上就好。</Text>
      </View> : null}

      {status ? <View className="mp-card">
        {progress.total ? <>
          <View className="comic-metric">
            <Text className="comic-metric-number">{progress.succeeded}</Text>
            <Text className="comic-metric-unit">/ {progress.total} 本已完成</Text>
          </View>
          <View className="comic-bar"><View className="comic-bar-fill" style={{ width: `${percent}%` }} /></View>
        </> : <Text className="mp-row-title">{finished ? "已经结束" : "正在排队，马上开始"}</Text>}
        <Text className="mp-row-description">{finished
          ? progress.failed ? `已完成 ${progress.succeeded} 本，${progress.failed} 本没有成功。` : `已完成 ${progress.succeeded} 本。`
          : runningBook ? `正在生成${nameOf(runningBook.bookId, books.indexOf(runningBook))}…` : "正在等待下一本开始…"}</Text>
        <Text className="mp-row-description">已用时 {elapsedText(startedAt)}，进度会自动更新，不用一直守着。</Text>
        {progress.failed ? <Text className="mp-row-description">有 {progress.failed} 本没成功，下面会写原因。</Text> : null}
      </View> : null}

      {books.length ? <View className="mp-card">
        <Text className="mp-row-title">每本书的进度</Text>
        {books.map((book, index) => {
          const label = statusLabel(book.status);
          const failed = String(book.status || "").toLowerCase() === "failed";
          return (
            <View className="comic-book-row" key={book.bookId || String(index)}>
              <View className="mp-row-copy">
                <Text className="mp-row-title">{nameOf(book.bookId, index)}</Text>
                {book.message ? <Text className="mp-row-description">{book.message}</Text> : null}
                {failed ? <Text className="mp-error">{book.error || "这本没有生成成功"}。可以回到上一步重新生成，或联系管理员。</Text> : null}
              </View>
              <Text className="comic-pill">{label}</Text>
            </View>
          );
        })}
      </View> : finished ? null : <View className="mp-card"><Text className="mp-row-description">书目会在这里逐本出现。</Text></View>}

      {error ? <View className="mp-card">
        <Text className="mp-row-title">暂时读不到进度</Text>
        <Text className="mp-error">{error}</Text>
        <Button className="mp-secondary" onClick={refresh}>立即刷新</Button>
      </View> : null}

      {failedBooks.length && !finished ? <Text className="comic-step-hint">有书没成功时，可以先看已出的页，之后再重新生成剩下的。</Text> : null}

      <ComicStickySpacer />
      <View className="comic-sticky">
        {canOpenResult ? <Button className="mp-primary" onClick={() => { void Taro.navigateTo({ url: `/comic/pages/comic-result/index?batchId=${encodeURIComponent(batchId)}&bookId=${encodeURIComponent(resultBookId)}` }); }}>
          {finished ? "看漫画" : "先看已出的页"}
        </Button> : finished ? <Button className="mp-secondary" onClick={() => { void Taro.navigateBack(); }}>回去重新生成</Button>
          : <Button className="mp-primary" disabled>生成中，请稍候</Button>}
      </View>
    </ThemedPage>
  );
}
