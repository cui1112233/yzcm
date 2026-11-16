import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import postcss from 'postcss';
import selectorParser from 'postcss-selector-parser';

const dist = fileURLToPath(new URL('../dist/', import.meta.url));
const supportedPseudos = new Set([':before', ':after', '::before', '::after']);

function wxssFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return wxssFiles(path);
    return entry.isFile() && entry.name.endsWith('.wxss') ? [path] : [];
  });
}

function supportsSelector(selector) {
  let supported = true;
  selectorParser(root => root.walkPseudos(pseudo => {
    if (!supportedPseudos.has(pseudo.value)) supported = false;
  })).processSync(selector);
  return supported;
}

let removed = 0;
for (const path of wxssFiles(dist)) {
  const root = postcss.parse(readFileSync(path, 'utf8'), { from: path });
  root.walkRules(rule => {
    const selectors = selectorParser().astSync(rule.selector).nodes
      .map(selector => selector.toString())
      .filter(supportsSelector);
    if (selectors.length === 0) {
      rule.remove();
      removed += 1;
      return;
    }
    if (selectors.length !== rule.selectors.length) {
      rule.selector = selectors.join(',');
      removed += 1;
    }
  });
  writeFileSync(path, root.toString());
}
console.log(`WXSS compatibility: removed ${removed} unsupported pseudo-class rule(s).`);
