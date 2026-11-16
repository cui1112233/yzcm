type ProjectSource = { id?: unknown; sourceText?: unknown; revision?: unknown; sourceRevision?: unknown; updatedAt?: unknown };
export function sourceRevisionKey(project: ProjectSource): string {
  return JSON.stringify([String(project.id || ''), String(project.sourceText || ''), project.sourceRevision ?? project.revision ?? project.updatedAt ?? null]);
}
export function createSegmentationReview() {
  let version = 0, sourceKey = '', submitting = false;
  const current = (ticket: number, key: string) => ticket === version && key === sourceKey;
  return {
    begin(key: string) { sourceKey = key; return ++version; },
    invalidate() { version += 1; sourceKey = ''; },
    current,
    async confirm(ticket: number, key: string, action: () => Promise<unknown>) {
      if (!current(ticket, key) || submitting) throw new Error('原文已变化或候选已确认，请重新生成候选分镜');
      submitting = true;
      try { await action(); if (current(ticket, key)) { version += 1; sourceKey = ''; } }
      finally { submitting = false; }
    },
  };
}

type ConfirmationInput = {
  review: ReturnType<typeof createSegmentationReview>; ticket: number; sourceKey: string;
  read: () => Promise<{ project: ProjectSource }>; isCurrent: () => boolean;
  submit: () => Promise<unknown>; clear: () => void;
};
export async function confirmReviewedSegmentation(input: ConfirmationInput) {
  const { review, ticket, sourceKey, read, submit, clear, isCurrent } = input;
  await review.confirm(ticket, sourceKey, async () => {
    const fresh = await read();
    if (!isCurrent() || !review.current(ticket, sourceKey)) return;
    if (!review.current(ticket, sourceRevisionKey(fresh.project))) {
      review.invalidate(); clear();
      throw new Error('服务端原文版本已变化，请刷新项目后重新生成候选');
    }
    await submit();
    if (isCurrent() && review.current(ticket, sourceKey)) clear();
  });
}
