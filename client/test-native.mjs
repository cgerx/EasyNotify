import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { startRelay } from '../server/test/process.js';

let relay = await startRelay({ key: 'native-test-key' });
const { port, base } = relay;
const child = spawn('client/build/StoreTests', [base], { stdio: ['ignore', 'pipe', 'inherit'] });
const exit = new Promise(resolve => child.on('exit', (code, signal) => resolve({ code, signal })));
const deadline = setTimeout(() => child.kill('SIGKILL'), 45_000);
try {
  for await (const line of createInterface({ input: child.stdout })) {
    if (line.startsWith('SEND_')) {
      const response = await fetch(`${base}/notify`, {
        method: 'POST', headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ title: line, description: '# Native\n\n**Markdown**' })
      });
      if (!response.ok) throw new Error(`Native delivery failed: ${response.status}`);
    } else if (line === 'STOP_RELAY') {
      await relay.close();
    } else if (line === 'RESTART_RELAY') {
      relay = await startRelay({ key: 'native-test-key', port });
    } else console.log(line);
  }
  const { code, signal } = await exit;
  if (code !== 0) throw new Error(`Native tests failed: ${code ?? signal}`);
} finally {
  clearTimeout(deadline);
  child.kill();
  await relay.close();
}
