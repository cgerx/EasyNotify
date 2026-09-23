import { test } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { resolve } from 'node:path';
import { WebSocket } from 'ws';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { startRelay } from '../../server/test/process.js';

test('real MCP stdio initialize, list, send, validation and offline queueing (no sender key)', async t => {
  const relay = await startRelay({ key: 'mcp-test-key' });
  t.after(() => relay.close());
  const url = relay.base;
  async function client() {
    const c = new Client({ name: 'easynotify-test', version: '1' });
    await c.connect(new StdioClientTransport({ command: resolve('mcp/build/easynotify-mcp'), args: [],
      env: { EASYNOTIFY_URL: url }, stderr: 'pipe' }));
    t.after(() => c.close());
    return c;
  }
  const c = await client();
  const tools = await c.listTools();
  assert.deepEqual(tools.tools.map(x => x.name), ['notify']);
  assert.deepEqual(Object.keys(tools.tools[0].inputSchema.properties).sort(), ['description', 'title']);
  const args = { title: 'MCP 已完成', description: '# 结果\n\n**全部通过**' };
  const offline=await c.callTool({ name: 'notify', arguments: args });
  assert.equal(JSON.parse(offline.content[0].text).accepted,true);
  const ws = new WebSocket(url.replace('http', 'ws') + '/ws', { headers: { authorization: 'Bearer mcp-test-key' } });
  const queued = once(ws,'message');
  await once(ws, 'open');
  const pending=JSON.parse((await queued)[0]);
  ws.send(JSON.stringify({type:'ack',id:pending.id}));
  const received = once(ws, 'message');
  const result = await c.callTool({ name: 'notify', arguments: args });
  assert.ok(!result.isError);
  assert.equal(JSON.parse(result.content[0].text).accepted, true);
  assert.equal(JSON.parse((await received)[0]).description, args.description);
  await assert.rejects(c.callTool({ name: 'notify', arguments: { ...args, title: '文'.repeat(31) } }), error => error.code === -32602);
  const blank = await c.callTool({ name: 'notify', arguments: { ...args, title: ' ' } });
  assert.equal(blank.isError, true);
  await relay.close();
  const unreachable = await c.callTool({ name: 'notify', arguments: args });
  assert.equal(unreachable.isError, true);
  assert.match(unreachable.content[0].text, /Delivery is unknown/);
});
