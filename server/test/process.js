import { spawn } from 'node:child_process';
import { resolve } from 'node:path';

export async function startRelay({ key, port = 0 } = {}) {
  const child = spawn(resolve('server/build/easynotify-server'), ['start', '--port', String(port)], {
    env: { ...process.env, EASYNOTIFY_KEY: key, HOST: '127.0.0.1' }, stdio: ['ignore', 'ignore', 'pipe']
  });
  let exited = false;
  const exit = new Promise(resolve => child.on('exit', () => { exited = true; resolve(); }));
  const base = await new Promise((resolve, reject) => {
    let output = '';
    const timeout = setTimeout(() => { child.kill(); reject(new Error('Relay startup timed out')); }, 10_000);
    child.on('error', error => { clearTimeout(timeout); reject(error); });
    child.on('exit', code => { clearTimeout(timeout); reject(new Error(`Relay exited ${code}: ${output}`)); });
    child.stderr.on('data', bytes => {
      output += bytes;
      const match = output.match(/EasyNotify listening on (127\.0\.0\.1:\d+)/);
      if (match) { clearTimeout(timeout); resolve(`http://${match[1]}`); }
    });
  });
  return { base, port: Number(new URL(base).port), pid: child.pid,
    async close() { if (!exited) child.kill('SIGTERM'); await exit; }
  };
}
