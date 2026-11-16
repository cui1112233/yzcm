import { useEffect, useRef } from "react";
import { useDidHide, useDidShow } from "@tarojs/taro";
import { startComicPolling } from "../../services/comicFlow";

export function useComicPolling<T>(
  enabled: boolean,
  batchId: string,
  read: () => Promise<T>,
  publish: (value: T) => void,
  fail: (cause: unknown) => void,
) {
  const callbacks = useRef({ read, publish, fail });
  callbacks.current = { read, publish, fail };
  const visible = useRef(true);
  const stop = useRef<(() => void) | undefined>(undefined);
  const pause = () => { stop.current?.(); stop.current = undefined; };
  const start = () => {
    pause();
    if (!visible.current || !enabled || !batchId) return;
    stop.current = startComicPolling(
      () => callbacks.current.read(),
      (value) => callbacks.current.publish(value),
      (cause) => callbacks.current.fail(cause),
    );
  };
  useDidHide(() => { visible.current = false; pause(); });
  useDidShow(() => { visible.current = true; start(); });
  useEffect(() => { start(); return pause; }, [enabled, batchId]);
}
