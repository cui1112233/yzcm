import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';

const here = path.dirname(fileURLToPath(import.meta.url));
const browser = [process.env.CHROME_PATH, 'C:/Program Files/Google/Chrome/Application/chrome.exe', 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(file => file && existsSync(file));
const archive = path.resolve(here, '../../../../_local-archive-20261028/luoshui-parity-20261126');

test('正文和章节方案显示目标与替换内容，过期方案不可确认', { skip: !browser }, async () => {
  const contents = `
    import React from 'react';
    import {createRoot} from 'react-dom/client';
    import {ComicAssistantPanel} from './ComicScreenplayAuthoringPanels.jsx';
    import {setToken, setVerifiedImpersonation} from '../../shared/api/client.js';
    setToken('fixture');
    setVerifiedImpersonation({actorUsername:'alice', username:'alice', impersonationId:'none'});
    const tick = () => new Promise(resolve => setTimeout(resolve, 120));
    const assert = (value, text) => { if (!value) throw Error(text); };
    const state = {revision:3, messages:[], proposals:[
      {id:'doc', status:'pending', actions:[{kind:'document', target_id:'doc-real', content:'待审核的正文稿', content_edit:{document:{id:'doc-real', version:5, revision:1}, publish:true}}]},
      {id:'chapter', status:'stale', actions:[{kind:'chapter', target_id:'chapter-real', content_edit:{author_revision:2, chapter_title:'原章节', chapter:{id:'chapter-real', title:'章名修改', source_text:''}}}]},
      {id:'legacy', status:'stale', actions:[{kind:'document', content:'旧历史稿'}]},
    ]};
    const writes = [];
    window.fetch = async (url, options = {}) => {
      if (options.method === 'POST') {
        writes.push({url:String(url), body:JSON.parse(options.body)});
        state.revision++;
        state.proposals[0].status = 'applied';
      }
      return new Response(JSON.stringify({state}), {headers:{'Content-Type':'application/json'}});
    };
    (async () => {
      createRoot(document.getElementById('root')).render(<ComicAssistantPanel projectId="p" canGenerate={false}/>);
      for (let i=0; i<50 && !document.querySelector('.comic-assistant-proposal'); i++) await tick();
      const [doc, chapter, legacy] = document.querySelectorAll('.comic-assistant-proposal');
      assert(doc && chapter && legacy, 'missing proposal');
      for (const text of ['目标正文', '第5版', 'doc-real', '待替换正文', '待审核的正文稿']) assert(doc.textContent.includes(text), 'missing '+text);
      for (const text of ['原章节', 'chapter-real', '章节版本 2', '章名修改', '章节原文', '清空']) assert(chapter.textContent.includes(text), 'missing '+text);
      assert(legacy.textContent.includes('旧历史稿') && legacy.querySelector('button').disabled, 'old action history removed or still actionable');
      assert(chapter.querySelector('button').disabled, 'stale chapter can be applied');
      assert(writes.length === 0, 'wrote before user confirmation');
      doc.querySelector('button').click();
      await tick();
      await tick();
      assert(writes.length === 0, 'opened popover before writing');
      const confirm = document.querySelector('.ant-popover .ant-btn-primary');
      assert(confirm, 'missing confirmation');
      confirm.click();
      await tick();
      await tick();
      assert(writes.length === 1 && writes[0].url.endsWith('/projects/p/assistant/apply'), 'wrong confirmation path');
      assert(writes[0].body.proposal_id === 'doc' && writes[0].body.expected_revision === 3 && writes[0].body.confirmed === true, 'wrong confirmation payload');
      assert(document.querySelector('.comic-assistant-proposal button').disabled, 'applied proposal can repeat');
      document.body.dataset.result = 'passed';
    })().catch(error => { document.body.dataset.error = error.message; });
  `;
  const bundle = await build({ stdin: { resolveDir: here, loader: 'jsx', contents }, bundle: true, jsx: 'automatic', write: false, format: 'iife', platform: 'browser', loader: { '.css': 'empty' } });
  mkdirSync(archive, { recursive: true });
  const dir = mkdtempSync(path.join(archive, 'assistant-content-edit-'));
  writeFileSync(path.join(dir, 'test.js'), bundle.outputFiles[0].text);
  const html = path.join(dir, 'test.html');
  writeFileSync(html, '<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
  const output = execFileSync(browser, ['--headless=new', '--disable-gpu', '--no-first-run', '--disable-background-networking', '--host-resolver-rules=MAP * ~NOTFOUND', '--window-size=900,900', '--virtual-time-budget=16000', '--user-data-dir='+path.join(dir, 'profile'), '--dump-dom', pathToFileURL(html).href], { encoding: 'utf8', timeout: 60000, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4000000 });
  writeFileSync(path.join(dir, 'result.html'), output);
  assert.ok(/data-result="passed"/.test(output), output.match(/data-error="([^"]*)"/)?.[1] || 'browser did not finish');
});
