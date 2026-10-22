'use strict';

// 在后台线程里下载并解密文件、加密要上传的文件。
// 手机上解密一个上百兆的文件要好几秒，放在页面主线程里会让整个页面卡住（看起来像卡死）。
//
// 页面发来的消息：
//   { type: 'key', raw }                              设置内容加密密钥（32 字节）
//   { type: 'open', id, url, headers, mime }          下载并解密，完成后回传 Blob
//   { type: 'seal', id, file }                        加密一个文件，完成后回传 Blob
//   { type: 'cancel', id }                            取消
// 回传：{ id, type: 'progress', done, total } / { id, type: 'done', blob } / { id, type: 'error', message, status }

importScripts('crypto.js');

let key = null;
const jobs = new Map(); // id -> AbortController

function progress(id, done, total) {
  const now = Date.now(), j = jobs.get(id);
  if (!j || now - j.t < 150) return; // 最多每 150 毫秒报一次
  j.t = now;
  postMessage({ id, type: 'progress', done, total });
}

async function open({ id, url, headers, mime }) {
  const ac = jobs.get(id).ac;
  const r = await fetch(url, { headers, signal: ac.signal });
  if (!r.ok) throw Object.assign(new Error('HTTP ' + r.status), { status: r.status });
  const op = r.headers.get('X-Lanvia-Enc') === '1' ? new LC.Opener(key) : null;
  const reader = r.body.getReader(), parts = [];
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    for (const p of op ? op.push(value) : [value]) { parts.push(p); got += p.length; }
    progress(id, got, 0);
  }
  op?.end();
  return new Blob(parts, { type: mime || '' });
}

async function seal({ id, file }) {
  const ac = jobs.get(id).ac;
  const s = new LC.Sealer(key), parts = [], CHUNK = 64 * LC.FRAME, size = file.size;
  if (!size) return new Blob([s.frame(new Uint8Array(0), true)]);
  for (let off = 0; off < size; off += CHUNK) {
    if (ac.signal.aborted) throw new DOMException('aborted', 'AbortError');
    const buf = new Uint8Array(await file.slice(off, off + CHUNK).arrayBuffer());
    for (let i = 0; i < buf.length; i += LC.FRAME) {
      const end = Math.min(buf.length, i + LC.FRAME);
      parts.push(s.frame(buf.subarray(i, end), off + end === size));
    }
    progress(id, Math.min(size, off + CHUNK), size);
  }
  return new Blob(parts);
}

onmessage = async ({ data: m }) => {
  if (m.type === 'key') { key = m.raw ? LC.newKey(new Uint8Array(m.raw)) : null; return; }
  if (m.type === 'cancel') { jobs.get(m.id)?.ac.abort(); return; }
  jobs.set(m.id, { ac: new AbortController(), t: 0 });
  try {
    const blob = await (m.type === 'open' ? open(m) : seal(m));
    postMessage({ id: m.id, type: 'done', blob });
  } catch (e) {
    postMessage({ id: m.id, type: 'error', message: e.name === 'AbortError' ? 'aborted' : e.message, status: e.status || 0 });
  } finally {
    jobs.delete(m.id);
  }
};
