import { expect, test } from 'vitest';
import { confirmReviewedSegmentation, createSegmentationReview, sourceRevisionKey } from './segmentationReview';

test('A candidates cannot confirm after B source is edited or saved, including late A preview', async () => {
  const review = createSegmentationReview();
  const sourceA = sourceRevisionKey({ id: 7, sourceText: 'A', revision: 1 });
  const sourceB = sourceRevisionKey({ id: 7, sourceText: 'B', revision: 2 });
  const ticket = review.begin(sourceA);
  expect(review.current(ticket, sourceA)).toBe(true);
  review.invalidate(); // source editor changes before save/readback
  expect(review.current(ticket, sourceA)).toBe(false);
  let submissions = 0;
  await expect(review.confirm(ticket, sourceB, async () => { submissions += 1; })).rejects.toThrow();
  expect(submissions).toBe(0);
});

test('source text, project identity and server revision each invalidate candidates', async () => {
  const review = createSegmentationReview();
  const ticket = review.begin(sourceRevisionKey({ id: 7, sourceText: 'A', revision: 1 }));
  for (const project of [{ id: 7, sourceText: 'B', revision: 1 }, { id: 7, sourceText: 'A', revision: 2 }, { id: 8, sourceText: 'A', revision: 1 }]) {
    expect(review.current(ticket, sourceRevisionKey(project))).toBe(false);
  }
});

test('successful confirmation consumes candidate revision and cannot submit it twice', async () => {
  const review = createSegmentationReview(), key = sourceRevisionKey({ id: 7, sourceText: 'A' });
  const ticket = review.begin(key); let submissions = 0;
  await review.confirm(ticket, key, async () => { submissions += 1; });
  await expect(review.confirm(ticket, key, async () => { submissions += 1; })).rejects.toThrow();
  expect(submissions).toBe(1);
});

test('deferred A preflight cannot clear B candidates or submit A after switching projects', async () => {
  const review = createSegmentationReview();
  const projectA = { id: 'A', sourceText: 'source A', revision: 1 };
  const keyA = sourceRevisionKey(projectA), keyB = sourceRevisionKey({ id: 'B', sourceText: 'source B', revision: 1 });
  const ticketA = review.begin(keyA);
  let selected = 'A', candidates = ['A candidate'], posts = 0, clears = 0;
  let resolveA!: (value: { project: typeof projectA }) => void;
  const pendingA = new Promise<{ project: typeof projectA }>(resolve => { resolveA = resolve; });
  const operation = confirmReviewedSegmentation({
    review, ticket: ticketA, sourceKey: keyA, read: () => pendingA,
    isCurrent: () => selected === 'A',
    submit: async () => { posts += 1; },
    clear: () => { clears += 1; candidates = []; },
  });
  selected = 'B'; review.invalidate();
  const ticketB = review.begin(keyB); candidates = ['B candidate'];
  resolveA({ project: projectA });
  await operation.catch(() => undefined);
  expect(candidates).toEqual(['B candidate']);
  expect(clears).toBe(0); expect(posts).toBe(0);
  expect(review.current(ticketB, keyB)).toBe(true);
});

test('current-project preflight still rejects changed server source and clears only its own candidates', async () => {
  const review = createSegmentationReview(), key = sourceRevisionKey({ id: 'A', sourceText: 'old', revision: 1 });
  const ticket = review.begin(key); let posts = 0, clears = 0;
  await expect(confirmReviewedSegmentation({
    review, ticket, sourceKey: key, isCurrent: () => true,
    read: async () => ({ project: { id: 'A', sourceText: 'changed', revision: 2 } }),
    submit: async () => { posts += 1; }, clear: () => { clears += 1; },
  })).rejects.toThrow('服务端原文版本已变化');
  expect(posts).toBe(0); expect(clears).toBe(1); expect(review.current(ticket, key)).toBe(false);
});
