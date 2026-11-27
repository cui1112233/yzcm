import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCanvasHref, summarizeCanvasPreview } from './canvasSyncModel.js';

test('canvas href keeps work and return location', () => {
  assert.equal(buildCanvasHref('task-1','work-1'), '/agent/canvas?taskId=task-1&workId=work-1&returnTo=%2Fscript-editor%3FworkId%3Dwork-1%26tab%3Dcanvas');
});
test('preview summary keeps conflicts visible', () => assert.deepEqual(summarizeCanvasPreview([{state:'create'},{state:'conflict'}]), {create:1,update:0,conflict:1}));
