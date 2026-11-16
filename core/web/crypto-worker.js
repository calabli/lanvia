'use strict';

// Download and decrypt files, and encrypt files to upload, in a background thread.
// Decrypting a hundred-megabyte file on the phone takes several seconds; doing it on the page's main thread freezes the whole page (it looks like a hang).
//
// Messages sent by the page:
//   { type: 'key', raw }                              set the content encryption key (32 bytes)
//   { type: 'open', id, url, headers, mime }          download and decrypt, post the Blob back when done
//   { type: 'seal', id, file }                        encrypt one file, post the Blob back when done
//   { type: 'cancel', id }                            cancel
// Posted back: { id, type: 'progress', done, total } / { id, type: 'done', blob } / { id, type: 'error', message, status }

importScripts('crypto.js');

let key = null;
const jobs = new Map(); // id -> AbortController

function progress(id, done, total) {
  const now = Date.now(), j = jobs.get(id);
  if (!j || now - j.t < 150) return; // report at most once every 150 ms
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
