import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const exe = process.argv[2] ? path.resolve(process.argv[2]) : path.join(__dirname, 'id3mcp.exe');

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

function mkMp3(p) {
  fs.writeFileSync(p, Buffer.from('audio-bytes-padding!!', 'utf8'));
}
function mkJpeg(p) {
  fs.writeFileSync(p, Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xff, 0xd9]));
}
function mkPng(p) {
  const sig = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  const ihdrData = Buffer.from([0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0]);
  const ihdrType = Buffer.from('IHDR');
  const ihdrLen = Buffer.alloc(4); ihdrLen.writeUInt32BE(13);
  const ihdrCrc = Buffer.alloc(4); ihdrCrc.writeUInt32BE(0x0d6a6328);
  const ihdr = Buffer.concat([ihdrLen, ihdrType, ihdrData, ihdrCrc]);
  const iendLen = Buffer.alloc(4);
  const iendType = Buffer.from('IEND');
  const iendCrc = Buffer.alloc(4); iendCrc.writeUInt32BE(0xae426082);
  const iend = Buffer.concat([iendLen, iendType, iendCrc]);
  fs.writeFileSync(p, Buffer.concat([sig, ihdr, iend]));
}

class MCP {
  constructor(exePath, root) {
    this.exePath = exePath;
    this.root = root;
    this.nextId = 1;
    this.pending = new Map();
    this.buf = '';
    this.closed = new Promise((resolve) => { this._resolveClosed = resolve; });
    this.child = spawn(exePath, [], {
      cwd: root,
      env: { ...process.env, ID3MCP_ROOT: root },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    this.stderr = '';
    this.child.stderr.on('data', (d) => { this.stderr += d.toString('utf8'); });
    this.child.stdout.on('data', (d) => {
      this.buf += d.toString('utf8');
      let idx;
      while ((idx = this.buf.indexOf('\n')) >= 0) {
        const line = this.buf.slice(0, idx).trim();
        this.buf = this.buf.slice(idx + 1);
        if (!line) continue;
        let msg;
        try { msg = JSON.parse(line); } catch { continue; }
        if (msg.id !== undefined && this.pending.has(msg.id)) {
          const { resolve, reject, timer } = this.pending.get(msg.id);
          clearTimeout(timer);
          this.pending.delete(msg.id);
          if (msg.error) reject(new Error(`RPC ${msg.id} error: ${JSON.stringify(msg.error)}`));
          else resolve(msg);
        }
      }
    });
    this.child.on('close', (code) => this._resolveClosed(code));
  }
  send(msg) { this.child.stdin.write(JSON.stringify(msg) + '\n'); }
  request(method, params = {}) {
    const id = this.nextId++;
    const msg = { jsonrpc: '2.0', id, method, params };
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`timeout waiting for ${method} id=${id}; stderr=${this.stderr}`));
      }, 20000);
      this.pending.set(id, { resolve, reject, timer });
      this.send(msg);
    });
  }
  notify(method, params = {}) { this.send({ jsonrpc: '2.0', method, params }); }
  async callTool(name, args) {
    const msg = await this.request('tools/call', { name, arguments: args });
    const text = msg.result?.content?.[0]?.text;
    let data = text;
    try { data = JSON.parse(text); } catch {}
    return { msg, data, isError: !!msg.result?.isError };
  }
  async close() {
    try { this.child.stdin.end(); } catch {}
    const t = setTimeout(() => { try { this.child.kill(); } catch {} }, 2000);
    const code = await this.closed;
    clearTimeout(t);
    return code;
  }
}

async function main() {
  if (!fs.existsSync(exe)) {
    console.error(`Binary not found: ${exe}`);
    process.exit(2);
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'id3mcp-it-'));
  const mp3 = path.join(root, 'test.mp3');
  const subdir = path.join(root, 'sub');
  const nestedMp3 = path.join(subdir, 'nested.mp3');
  const cover = path.join(root, 'cover.jpg');
  const extracted = path.join(root, 'out.jpg');
  fs.mkdirSync(subdir, { recursive: true });
  mkMp3(mp3);
  mkMp3(nestedMp3);
  mkJpeg(cover);

  const mcp = new MCP(exe, root);
  let passed = 0;
  async function test(name, fn) {
    try {
      await fn();
      console.log(`PASS ${name}`);
      passed++;
    } catch (e) {
      console.error(`FAIL ${name}: ${e.stack || e}`);
      throw e;
    }
  }

  try {
    const init = await mcp.request('initialize', {
      protocolVersion: '2024-11-05',
      capabilities: {},
      clientInfo: { name: 'integration-test', version: '1.0' },
    });
    assert(init.result?.serverInfo?.name === 'id3mcp', 'server name');
    mcp.notify('notifications/initialized', {});

    await test('id3_list', async () => {
      const { data, isError } = await mcp.callTool('id3_list', { path: '.' });
      assert(!isError, `id3_list error: ${JSON.stringify(data)}`);
      assert(Array.isArray(data.files), 'files array');
      assert(data.files.some((f) => f.endsWith('test.mp3')), 'contains test.mp3');
      assert(!data.files.some((f) => f.includes('nested.mp3')), 'non-recursive excludes nested');
      assert(data.recursive === false, 'recursive false by default');
    });

    await test('id3_list recursive', async () => {
      const { data, isError } = await mcp.callTool('id3_list', { path: '.', recursive: true });
      assert(!isError, `id3_list recursive error: ${JSON.stringify(data)}`);
      assert(data.recursive === true, 'recursive true');
      assert(data.files.some((f) => f.replace(/\\/g, '/').endsWith('sub/nested.mp3')), 'contains nested mp3');
    });

    await test('id3_read', async () => {
      const { data, isError } = await mcp.callTool('id3_read', { paths: ['test.mp3'] });
      assert(!isError, `id3_read error: ${JSON.stringify(data)}`);
      assert(data.files?.length === 1, 'one file');
      assert(data.files[0].title === '', 'empty title');
      assert(Array.isArray(data.files[0].covers), 'covers array');
    });

    await test('id3_set preview', async () => {
      const { data, isError } = await mcp.callTool('id3_set', {
        confirm: false,
        files: [{ path: 'test.mp3', title: 'Е.С.Бхакти Вигьяна Госвами Махарадж', artist: 'Test Artist' }],
      });
      assert(!isError, `id3_set preview error: ${JSON.stringify(data)}`);
      assert(data.wrote === false, 'preview wrote=false');
      assert(data.files[0].after.title === 'Е.С.Бхакти Вигьяна Госвами Махарадж', 'preview title');
      const afterRead = await mcp.callTool('id3_read', { paths: ['test.mp3'] });
      assert(afterRead.data.files[0].title === '', 'preview did not write');
    });

    await test('id3_set confirm', async () => {
      const { data, isError } = await mcp.callTool('id3_set', {
        confirm: true,
        files: [{ path: 'test.mp3', title: 'Е.С.Бхакти Вигьяна Госвами Махарадж', artist: 'Test Artist', album: 'Test Album' }],
      });
      assert(!isError, `id3_set confirm error: ${JSON.stringify(data)}`);
      assert(data.wrote === true, 'wrote=true');
      assert(data.files[0].after.title === 'Е.С.Бхакти Вигьяна Госвами Махарадж', 'title');
      assert(data.files[0].after.artist === 'Test Artist', 'artist');
      assert(data.files[0].after.album === 'Test Album', 'album');
    });

    await test('id3_set_cover preview', async () => {
      const { data, isError } = await mcp.callTool('id3_set_cover', {
        confirm: false,
        files: [{ path: 'test.mp3', image_path: 'cover.jpg' }],
      });
      assert(!isError, `id3_set_cover preview error: ${JSON.stringify(data)}`);
      assert(data.wrote === false, 'preview wrote=false');
      assert(data.files[0].after.covers.length >= 1, 'preview has cover');
      const r = await mcp.callTool('id3_read', { paths: ['test.mp3'] });
      assert(r.data.files[0].covers.length === 0, 'cover preview did not write');
    });

    await test('id3_set_cover confirm', async () => {
      const { data, isError } = await mcp.callTool('id3_set_cover', {
        confirm: true,
        files: [{ path: 'test.mp3', image_path: 'cover.jpg' }],
      });
      assert(!isError, `id3_set_cover confirm error: ${JSON.stringify(data)}`);
      assert(data.wrote === true, 'wrote=true');
      assert(data.files[0].after.covers[0].mime === 'image/jpeg', 'jpeg mime');
    });

    await test('id3_extract_cover', async () => {
      const { data, isError } = await mcp.callTool('id3_extract_cover', {
        path: 'test.mp3',
        out_path: 'out.jpg',
        overwrite: true,
      });
      assert(!isError, `id3_extract_cover error: ${JSON.stringify(data)}`);
      assert(fs.existsSync(extracted), 'extracted file exists');
      const orig = fs.readFileSync(cover);
      const got = fs.readFileSync(extracted);
      assert(Buffer.compare(orig, got) === 0, 'extracted bytes match');
      assert(data.cover?.mime === 'image/jpeg', 'cover mime');
    });

    await test('id3_read after writes', async () => {
      const { data, isError } = await mcp.callTool('id3_read', { paths: ['test.mp3'] });
      assert(!isError, `read error: ${JSON.stringify(data)}`);
      const f = data.files[0];
      assert(f.title === 'Е.С.Бхакти Вигьяна Госвами Махарадж', 'title persisted');
      assert(f.artist === 'Test Artist', 'artist persisted');
      assert(f.album === 'Test Album', 'album persisted');
      assert(f.covers.length === 1, 'cover persisted');
    });

    console.log(`\n${passed} tests passed`);
  } finally {
    await mcp.close();
    try { fs.rmSync(root, { recursive: true, force: true }); } catch {}
  }
}

main().catch((e) => {
  console.error(e.stack || e);
  process.exit(1);
});
