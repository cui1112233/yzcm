export type BatchFactoryPageMode = "works" | "create" | "project" | "book";

export function startAutomationPolling<T>(read: () => Promise<T>, publish: (value: T) => void, onError: (cause: unknown) => void, interval = 5000) {
  let active = true;
  let timer: ReturnType<typeof setTimeout>;
  const poll = async () => {
    try { const next = await read(); if (active) publish(next); }
    catch (cause) { if (active) onError(cause); }
    if (active) timer = setTimeout(poll, interval);
  };
  timer = setTimeout(poll, interval);
  return () => { active = false; clearTimeout(timer); };
}

export type BatchFactoryPageState = {
  mode: BatchFactoryPageMode;
  selectedBatchId: string;
  selectedBookId: string;
};

export const initialBatchFactoryPageState: BatchFactoryPageState = {
  mode: "works",
  selectedBatchId: "",
  selectedBookId: "",
};

export function openProject(state: BatchFactoryPageState, batchId: string): BatchFactoryPageState {
  return { mode: "project", selectedBatchId: String(batchId || ""), selectedBookId: "" };
}

export function openBook(state: BatchFactoryPageState, bookId: string): BatchFactoryPageState {
  return { ...state, mode: "book", selectedBookId: String(bookId || "") };
}

export function openCreate(state: BatchFactoryPageState): BatchFactoryPageState {
  return { ...state, mode: "create" };
}

export function backToProject(state: BatchFactoryPageState): BatchFactoryPageState {
  return { ...state, mode: "project", selectedBookId: "" };
}

export function backToWorks(state: BatchFactoryPageState): BatchFactoryPageState {
  return { mode: "works", selectedBatchId: "", selectedBookId: "" };
}

export function replaceSelectedBatch(state: BatchFactoryPageState, batchId: string): BatchFactoryPageState {
  const nextBatchId = String(batchId || "");
  if (nextBatchId === state.selectedBatchId) return state;
  return { ...state, selectedBatchId: nextBatchId, selectedBookId: "", mode: "project" };
}
