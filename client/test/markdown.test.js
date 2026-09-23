import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';
const html = readFileSync('client/Resources/reader.html', 'utf8')
  .replace('/*MARKED*/', () => readFileSync('node_modules/marked/lib/marked.umd.js', 'utf8'))
  .replace('/*PURIFY*/', () => readFileSync('node_modules/dompurify/dist/purify.min.js', 'utf8'));
const dom = new JSDOM(html, { runScripts: 'dangerously' });
const render = text => dom.window.renderMarkdown(text);

test('Markdown renders headings, lists, code, links and tables', () => {
  const result = render('# 完成\n\n**成功**\n\n- 单元测试\n\n```sh\necho ok\n```\n\n[详情](https://example.com)\n\n| A | B |\n|---|---|\n| 1 | 2 |');
  for (const tag of ['h1', 'strong', 'ul', 'pre', 'a', 'table']) assert.match(result, new RegExp(`<${tag}[ >]`));
});

test('untrusted Markdown cannot inject scripts, handlers, remote images or unsafe links', () => {
  const result = render('<script>alert(1)</script><img src="https://evil.test/tracker" onerror="alert(1)"><iframe src="https://evil.test"></iframe>\n\n[x](javascript:alert(1))\n\n<svg onload="alert(1)"></svg><div style="background:url(https://evil.test)">safe</div>');
  assert.doesNotMatch(result, /<script|onerror|onload|javascript:|<iframe|<img|<svg|style=/i);
  assert.match(result, /safe/);
});
