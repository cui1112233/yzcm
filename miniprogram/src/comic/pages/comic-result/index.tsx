import Taro, { useRouter } from "@tarojs/taro";
import { Button, Image, ScrollView, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import { ThemedPage } from "../../../theme";
import ComicStickySpacer from "../../components/ComicStickySpacer";
import { loadComicBooks, type ComicBookOption } from "../../services/comicFlow";
import {
  buildPreviewRequest, createComicCollection, downloadComicCollectionZip, loadComicImagePaths,
  loadComicPages, saveComicImageToAlbum, type ComicPageItem,
} from "../../services/comicResult";
import "../../styles/comic.scss";

export default function ComicResultPage() {
  const router = useRouter();
  const batchId = String(router.params?.batchId || "");
  const initialBookId = String(router.params?.bookId || "");
  const [books, setBooks] = useState<ComicBookOption[]>([]);
  const [bookId, setBookId] = useState(initialBookId);
  const [pages, setPages] = useState<ComicPageItem[]>([]);
  const [pageIndex, setPageIndex] = useState(0);
  const [paths, setPaths] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const version = useRef(0);

  const loadImages = async (page: ComicPageItem) => {
    const current = version.current;
    const loaded = await loadComicImagePaths(page);
    if (current === version.current) setPaths(loaded);
  };

  const loadPages = async (id: string) => {
    const current = ++version.current;
    setLoading(true);
    setError("");
    setPaths({});
    setPages([]);
    try {
      const next = await loadComicPages(batchId, id);
      if (current !== version.current) return;
      setPages(next);
      const defaultIndex = Math.max(0, next.findIndex((page) => page.images.length));
      setPageIndex(defaultIndex);
      if (next[defaultIndex]) void loadImages(next[defaultIndex]);
    } catch (cause: any) {
      if (current === version.current) setError(cause?.message || "暂时打不开这些图，请稍后再试");
    } finally {
      if (current === version.current) setLoading(false);
    }
  };

  const load = () => {
    if (!batchId) {
      setLoading(false);
      setError("还没有可看的漫画，先完成一次生成");
      return;
    }
    const current = ++version.current;
    setLoading(true);
    setError("");
    void loadComicBooks(batchId).then((list) => {
      if (current !== version.current) return;
      setBooks(list);
      const id = list.find((book) => book.id === initialBookId)?.id || list[0]?.id || "";
      setBookId(id);
      if (id) void loadPages(id);
      else {
        setLoading(false);
        setError("还没有可看的漫画，先完成一次生成");
      }
    }).catch((cause: any) => {
      if (current !== version.current) return;
      if (initialBookId) {
        setBookId(initialBookId);
        void loadPages(initialBookId);
        return;
      }
      setLoading(false);
      setError(cause?.message || "暂时打不开这本作品，请稍后再试");
    });
  };

  useEffect(() => { load(); return () => { version.current += 1; }; }, []);

  const selectBook = (index: number) => {
    const book = books[index];
    if (!book || book.id === bookId) return;
    setBookId(book.id);
    void loadPages(book.id);
  };

  const selectPage = (index: number) => {
    setPageIndex(index);
    setPaths({});
    const page = pages[index];
    if (page) void loadImages(page);
  };

  const goPrev = () => { if (pageIndex > 0) selectPage(pageIndex - 1); };
  const goNext = () => { if (pageIndex < pages.length - 1) selectPage(pageIndex + 1); };

  const preview = async (imageId: string) => {
    const page = pages[pageIndex];
    if (!page) return;
    setError("");
    try {
      const loaded = { ...paths, ...(await loadComicImagePaths(page)) };
      setPaths(loaded);
      const ids = page.images.map((image) => image.id).filter((id) => loaded[id]);
      if (!ids.length) throw new Error("这张图还没准备好，稍等一下再点");
      const request = buildPreviewRequest(ids.map((id) => loaded[id]), Math.max(0, ids.indexOf(imageId)));
      await Taro.previewImage({ urls: request.urls, current: request.current });
    } catch (cause: any) {
      setError(cause?.message || "暂时打不开大图，请稍后再试");
    }
  };

  const savePrimary = async () => {
    const page = pages[pageIndex];
    const image = page?.images.find((item) => item.id === page.primaryImageId);
    if (!image) { setError("这一页还没有可以保存的图"); return; }
    setBusy("save");
    setError("");
    try {
      const loaded = paths[image.id] ? paths : await loadComicImagePaths(page);
      setPaths(loaded);
      const filePath = loaded[image.id];
      if (!filePath) throw new Error("这张图还没准备好，稍等一下再试");
      await saveComicImageToAlbum(filePath);
      await Taro.showToast({ title: "已保存到相册", icon: "success" });
    } catch (cause: any) {
      const message = String(cause?.message || "保存失败");
      if (/权限/.test(message)) {
        const choice = await Taro.showModal({ title: "需要相册权限", content: message, confirmText: "去设置", cancelText: "取消" });
        if (choice.confirm) void Taro.openSetting({});
      } else {
        setError(message);
      }
    } finally {
      setBusy("");
    }
  };

  const downloadZip = async () => {
    const videoIds = pages.filter((page) => page.primaryImageId).map((page) => page.videoId);
    if (!videoIds.length) { setError("还没有可以打包的图，等生成完成再来"); return; }
    if (!bookId) { setError("还没有可看的漫画，先完成一次生成"); return; }
    setBusy("zip");
    setError("");
    try {
      const collection = await createComicCollection(batchId, bookId, videoIds);
      const file = await downloadComicCollectionZip(batchId, bookId, collection.id);
      const choice = await Taro.showModal({
        title: "已打包下载",
        content: `共 ${collection.items.length} 页，文件名 comic-collection.zip`,
        confirmText: "知道了",
        cancelText: "复制链接",
      });
      if (choice.cancel) await Taro.setClipboardData({ data: file.url });
    } catch (cause: any) {
      setError(cause?.message || "打包下载失败，请稍后再试");
    } finally {
      setBusy("");
    }
  };

  const current = pages[pageIndex];
  const primaryImage = current?.images.find((image) => image.id === current.primaryImageId);
  const stagePath = primaryImage ? paths[primaryImage.id] : "";
  const downloadable = pages.filter((page) => page.primaryImageId).length;

  return (
    <ThemedPage className="mp-page comic-page">
      <BrandHeader eyebrow="漫画" title="看漫画" showBack />
      <Text className="mp-title">你的漫画</Text>
      <Text className="mp-subtitle">{pages.length ? `共 ${pages.length} 页。点大图看全部候选，左右切页。` : "生成完成后，一页页出现在这里。"}</Text>

      {loading ? <View className="mp-card"><Text className="mp-row-title">正在打开…</Text><Text className="mp-row-description">马上就好。</Text></View> : null}

      {!loading && error && !pages.length ? <View className="mp-card">
        <Text className="mp-row-title">{batchId ? "暂时打不开" : "还没有漫画"}</Text>
        <Text className="mp-error">{error}</Text>
        {"还没有可看的漫画，先完成一次生成" === error
          ? <Button className="mp-primary" onClick={() => { void Taro.redirectTo({ url: "/comic/pages/comic-config/index" }); }}>去做漫画</Button>
          : <Button className="mp-secondary" onClick={load}>再试一次</Button>}
      </View> : null}

      {!loading && pages.length ? <>
        {books.length > 1 ? <ScrollView scrollX className="comic-style-row">
          {books.map((book, index) => (
            <View key={book.id} className={`comic-choice ${book.id === bookId ? "is-active" : ""}`} style={{ display: "inline-flex", width: "320rpx", marginRight: "16rpx" }} onClick={() => selectBook(index)}>
              <Text className="comic-choice-title" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{book.title}</Text>
              <Text className="comic-choice-mark">{book.id === bookId ? "当前" : "切换"}</Text>
            </View>
          ))}
        </ScrollView> : null}

        {current ? <>
          <View className="comic-stage">
            {stagePath
              ? <Image src={stagePath} lazyLoad mode="aspectFill" className="comic-stage-image" onClick={() => { if (primaryImage) void preview(primaryImage.id); }} />
              : <View className="comic-stage-empty" onClick={() => { if (current.images[0]) void preview(current.images[0].id); }}>
                <Text>这一页还在生成，稍后回来看看</Text>
              </View>}
            {primaryImage ? <Text className="comic-badge">当前采用</Text> : null}
          </View>

          {current.images.length > 1 ? <ScrollView scrollX className="comic-cand-row">
            {current.images.map((image) => (
              <View key={image.id} className={`comic-cand ${image.isPrimary ? "is-primary" : ""} comic-candidate`} onClick={() => { void preview(image.id); }}>
                {paths[image.id] ? <Image src={paths[image.id]} lazyLoad mode="aspectFill" className="comic-cand-image" /> : <View className="comic-cand-image" />}
                <Text className="comic-cand-label">{image.isPrimary ? "采用" : "候选"}</Text>
              </View>
            ))}
          </ScrollView> : null}

          <ScrollView scrollX className="comic-strip">
            {pages.map((page, index) => (
              <View key={page.pageId || page.videoId || String(index)} className={`comic-tile ${index === pageIndex ? "is-active" : ""} ${page.images.length ? "" : "is-empty"}`} onClick={() => selectPage(index)}>
                <Text>{page.ordinal}</Text>
              </View>
            ))}
          </ScrollView>

          {error ? <Text className="mp-error">{error}</Text> : null}

          <View className="mp-card">
            <Text className="mp-row-title">保存和下载</Text>
            <Text className="mp-row-description">保存这张图，或把整本打包成一个压缩包。</Text>
            <Button className="mp-secondary" loading={busy === "save"} disabled={busy === "save" || !current.primaryImageId} onClick={() => { void savePrimary(); }}>保存这张图到相册</Button>
            <Button className="mp-secondary" loading={busy === "zip"} disabled={busy === "zip" || !downloadable} onClick={() => { void downloadZip(); }}>打包下载全部（{downloadable} 页）</Button>
          </View>
        </> : null}
      </> : null}

      {pages.length ? <>
        <ComicStickySpacer />
        <View className="comic-sticky">
          <View className="comic-nav">
            <Button className="mp-secondary" disabled={pageIndex <= 0} onClick={goPrev}>上一页</Button>
            <View className="comic-nav-label">
              <Text className="mp-row-title">第 {current?.ordinal || pageIndex + 1} / {pages.length} 页</Text>
            </View>
            <Button className="mp-secondary" disabled={pageIndex >= pages.length - 1} onClick={goNext}>下一页</Button>
          </View>
        </View>
      </> : null}
    </ThemedPage>
  );
}
