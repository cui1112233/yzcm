import { useEffect, useRef } from "react";
import { useDidHide, useDidShow } from "@tarojs/taro";
import { startAutomationPolling } from "./batchFactoryPageState";

export function useAutomationPolling<T>(enabled: boolean, batchId: string, read: () => Promise<T>, update: (value: T) => void, fail: (cause: unknown) => void) {
  const callbacks = useRef({ read, update, fail });
  callbacks.current = { read, update, fail };
  const visible = useRef(true);
  const stop = useRef<(() => void) | undefined>();
  const pause = () => { stop.current?.(); stop.current = undefined; };
  const start = () => {
    pause();
    if (!visible.current || !enabled || !batchId) return;
    stop.current = startAutomationPolling(() => callbacks.current.read(), value => callbacks.current.update(value), cause => callbacks.current.fail(cause));
  };
  useDidHide(() => { visible.current = false; pause(); });
  useDidShow(() => { visible.current = true; start(); });
  useEffect(() => { start(); return pause; }, [enabled, batchId]);
}
