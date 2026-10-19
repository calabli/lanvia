'use strict';

/* ================= 基础工具 ================= */

const $ = (s, el = document) => el.querySelector(s);
const store = {
  get(k) { try { return localStorage.getItem('lanvia.' + k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem('lanvia.' + k, v); } catch {} },
  del(k) { try { localStorage.removeItem('lanvia.' + k); } catch {} },
};
const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const enc = new TextEncoder();

/* SHA-256 / HMAC：局域网走 http，浏览器的 crypto.subtle 不可用，所以自带一份实现。 */
const K256 = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);
function sha256(msg) {
  const len = msg.length, n = ((len + 9 + 63) >> 6) << 6;
  const m = new Uint8Array(n); m.set(msg); m[len] = 0x80;
  const dv = new DataView(m.buffer);
  dv.setUint32(n - 4, len * 8 >>> 0); dv.setUint32(n - 8, Math.floor(len / 0x20000000));
  const H = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]);
  const W = new Uint32Array(64);
  const rotr = (x, k) => (x >>> k) | (x << (32 - k));
  for (let o = 0; o < n; o += 64) {
    for (let i = 0; i < 16; i++) W[i] = dv.getUint32(o + i * 4);
    for (let i = 16; i < 64; i++) {
      const a = W[i - 15], b = W[i - 2];
      W[i] = W[i - 16] + (rotr(a, 7) ^ rotr(a, 18) ^ (a >>> 3)) + W[i - 7] + (rotr(b, 17) ^ rotr(b, 19) ^ (b >>> 10));
    }
    let [a, b, c, d, e, f, g, h] = H;
    for (let i = 0; i < 64; i++) {
      const t1 = (h + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K256[i] + W[i]) | 0;
      const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) | 0;
      h = g; g = f; f = e; e = (d + t1) | 0; d = c; c = b; b = a; a = (t1 + t2) | 0;
    }
    H[0] += a; H[1] += b; H[2] += c; H[3] += d; H[4] += e; H[5] += f; H[6] += g; H[7] += h;
  }
  const out = new Uint8Array(32), odv = new DataView(out.buffer);
  H.forEach((v, i) => odv.setUint32(i * 4, v));
  return out;
}
function hmac(key, msg) {
  const k = new Uint8Array(64); k.set(key.length > 64 ? sha256(key) : key);
  const ip = k.map(b => b ^ 0x36), op = k.map(b => b ^ 0x5c);
  const inner = new Uint8Array(64 + msg.length); inner.set(ip); inner.set(msg, 64);
  const outer = new Uint8Array(96); outer.set(op); outer.set(sha256(inner), 64);
  return sha256(outer);
}
const hex = b => Array.from(b, x => x.toString(16).padStart(2, '0')).join('');

/* ================= 状态与鉴权 ================= */

const ALPHABET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
const normCode = s => (s || '').toUpperCase().split('').filter(c => ALPHABET.includes(c)).join('');
const fmtCode = c => c.length > 5 ? c.slice(0, 5) + '-' + c.slice(5) : c;

const isLoopback = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
// 桌面客户端里的窗口：panel 是托盘弹出的快捷面板，main 是主窗口
const DESKTOP = new URLSearchParams(location.search).get('desktop');
const PANEL = DESKTOP === 'panel';
const S = {
  key: null, offset: 0, items: [], devices: [], you: null, node: null, local: false,
  info: null, filter: '', es: null, connected: false, retry: 0, unseen: 0,
  view: [], sel: 0, // 当前显示的列表和键盘选中项（快捷面板）
};
const desktop = action => api('POST', '/api/desktop/' + action).catch(() => {});

function setCode(code) {
  S.key = code ? sha256(enc.encode('lanvia-key:' + code)) : null;
}
function sign(method, path) {
  const ts = String(Date.now() + S.offset);
  return { ts, sig: hex(hmac(S.key, enc.encode(ts + '\n' + method + '\n' + path))) };
}
function signedURL(path, params = {}, method = 'GET') {
  const q = new URLSearchParams(params);
  if (S.key) { const { ts, sig } = sign(method, path); q.set('_t', ts); q.set('_s', sig); }
  const qs = q.toString();
  return path + (qs ? '?' + qs : '');
}
async function api(method, path, body) {
  const headers = {};
  if (S.key) { const { ts, sig } = sign(method, path); headers['X-Lanvia-TS'] = ts; headers['X-Lanvia-Sig'] = sig; }
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const r = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  if (r.status === 401 && !isLoopback) { unpair('配对已失效，请重新输入配对码'); throw new Error('unauthorized'); }
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || 'HTTP ' + r.status);
  return data;
}
async function syncTime() {
  try {
    const t0 = Date.now();
    const { now } = await (await fetch('/api/time', { cache: 'no-store' })).json();
    S.offset = now - Math.round((t0 + Date.now()) / 2);
  } catch {}
}

/* ================= 设备识别 ================= */

function detectKind() {
  const ua = navigator.userAgent;
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'ipad';
  if (/iPhone|iPod/.test(ua)) return 'ios';
  if (/Android/.test(ua)) return 'android';
  if (/Windows/.test(ua)) return 'windows';
  if (/Mac/.test(ua)) return 'mac';
  if (/Linux|CrOS/.test(ua)) return 'linux';
  return 'web';
}
const KIND = detectKind();
const KIND_NAME = { ios: 'iPhone', ipad: 'iPad', android: '安卓手机', windows: 'Windows 电脑', mac: 'Mac', linux: 'Linux 电脑', web: '浏览器' };
const myName = () => (S.local && S.node ? S.node.name : store.get('name') || KIND_NAME[KIND]);
const isTouch = matchMedia('(pointer: coarse)').matches;

/* ================= 图标 ================= */

const I = (d, extra = '') => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" ${extra}>${d}</svg>`;
const ICON = {
  copy: I('<rect x="9" y="9" width="11" height="11" rx="2.5"/><path d="M5 15V6.5A2.5 2.5 0 0 1 7.5 4H15"/>'),
  check: I('<path d="m5 12.5 4.5 4.5L19 7.5"/>'),
  trash: I('<path d="M4.5 7h15M10 11v6M14 11v6M6.5 7l.8 11.2A2 2 0 0 0 9.3 20h5.4a2 2 0 0 0 2-1.8L17.5 7M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>'),
  download: I('<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19.5h14"/>'),
  file: I('<path d="M14 3.5H7.5A2 2 0 0 0 5.5 5.5v13a2 2 0 0 0 2 2h9a2 2 0 0 0 2-2V8z"/><path d="M14 3.5V8h4.5"/>'),
  link: I('<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7L11.5 6.8"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.5-1.5"/>'),
  key: I('<circle cx="8" cy="15" r="3.5"/><path d="m10.5 12.5 8-8M16 7l2.5 2.5M14 9l2 2"/>'),
  gear: I('<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>'),
  close: I('<path d="M6 6l12 12M18 6 6 18"/>'),
  chevron: I('<path d="m6 15 6-6 6 6"/>'),
  phone: I('<rect x="7" y="3" width="10" height="18" rx="2.5"/><path d="M11 18h2"/>'),
  tablet: I('<rect x="4.5" y="3" width="15" height="18" rx="2.5"/><path d="M11 18h2"/>'),
  laptop: I('<rect x="4.5" y="5" width="15" height="10" rx="1.5"/><path d="M2.5 19h19"/>'),
  globe: I('<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.5 2.7 2.5 14.3 0 17M12 3.5c-2.5 2.7-2.5 14.3 0 17"/>'),
};
const kindIcon = k => ({ ios: ICON.phone, android: ICON.phone, ipad: ICON.tablet, mac: ICON.laptop, windows: ICON.laptop, linux: ICON.laptop }[k] || ICON.globe);

/* ================= 剪贴板 ================= */

async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) { await navigator.clipboard.writeText(text); return true; }
  } catch {}
  // 非安全上下文（局域网 http）只能走 execCommand，必须在用户点击的同步调用栈里
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;font-size:16px';
  document.body.appendChild(ta);
  if (KIND === 'ios' || KIND === 'ipad') {
    ta.contentEditable = 'true';
    const r = document.createRange(); r.selectNodeContents(ta);
    const sel = getSelection(); sel.removeAllRanges(); sel.addRange(r);
    ta.setSelectionRange(0, text.length);
  } else {
    ta.select();
  }
  let ok = false;
  try { ok = document.execCommand('copy'); } catch {}
  ta.remove();
  getSelection()?.removeAllRanges();
  return ok;
}

/* ================= 渲染 ================= */

function relTime(ts) {
  const d = Date.now() + S.offset - ts;
  if (d < 45e3) return '刚刚';
  if (d < 3600e3) return Math.round(d / 60e3) + ' 分钟前';
  const t = new Date(ts), now = new Date();
  const hm = t.toTimeString().slice(0, 5);
  if (t.toDateString() === now.toDateString()) return hm;
  const y = new Date(now); y.setDate(now.getDate() - 1);
  if (t.toDateString() === y.toDateString()) return '昨天 ' + hm;
  return `${t.getMonth() + 1}月${t.getDate()}日 ${hm}`;
}
function fmtSize(n) {
  if (n < 1024) return n + ' B';
  const u = ['KB', 'MB', 'GB']; let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 1 : 0) + ' ' + u[i];
}
const URL_RE = /\bhttps?:\/\/[^\s<>"'，。！？）】]+/gi;
function linkify(text) {
  return esc(text).replace(URL_RE, u => `<a href="${u}" target="_blank" rel="noopener noreferrer">${u}</a>`);
}
// 智能识别：短文本里的验证码
function findCode(text) {
  if (text.length > 160) return null;
  if (!/验证码|校验码|动态码|code|otp|码/i.test(text)) return null;
  const m = text.match(/(?:^|\D)(\d{4,8})(?!\d)/);
  return m ? m[1] : null;
}

function itemHTML(it, latest) {
  const fromDev = S.devices.find(d => d.name === it.from);
  const meta = `<div class="meta">${kindIcon(fromDev?.kind)}<span class="from">${esc(it.from)}</span><span>· ${relTime(it.ts)}</span>${latest ? '<span class="badge">最新</span>' : ''}</div>`;
  if (it.kind === 'file') {
    const isImg = /^image\/(png|jpe?g|gif|webp|heic|avif|bmp)$/i.test(it.mime || '');
    const isVid = /^video\//.test(it.mime || '');
    const media = isImg
      ? `<img class="thumb" loading="lazy" alt="${esc(it.name)}" src="${esc(signedURL('/api/blob/' + it.id))}" data-act="view">`
      : isVid ? `<video class="thumb" controls preload="metadata" src="${esc(signedURL('/api/blob/' + it.id))}"></video>` : '';
    return `${meta}${media}
      <div class="file"><div class="file-icon">${ICON.file}</div><div><div class="file-name">${esc(it.name)}</div><div class="muted small">${fmtSize(it.size || 0)}</div></div></div>
      <div class="actions">
        <button class="btn sm" data-act="download">${ICON.download}下载</button>
        <span class="spacer"></span>
        <button class="icon-btn" data-act="delete" aria-label="删除">${ICON.trash}</button>
      </div>`;
  }
  const long = it.text.length > 400 || it.text.split('\n').length > 8;
  const url = (it.text.match(URL_RE) || [])[0];
  const code = findCode(it.text);
  const smart = [
    code ? `<button class="btn sm" data-act="code" data-v="${esc(code)}">${ICON.key}复制验证码 ${esc(code)}</button>` : '',
    url && url.length < it.text.length + 1 ? `<a class="btn sm" href="${esc(url)}" target="_blank" rel="noopener noreferrer">${ICON.link}打开链接</a>` : '',
  ].join('');
  return `${meta}<div class="text" data-act="copy">${linkify(it.text)}</div>
    ${long ? '<button class="more" data-act="expand">展开全部</button>' : ''}
    <div class="actions">
      <button class="btn sm" data-act="copy">${ICON.copy}<span>复制</span></button>
      <span class="smart">${smart}</span>
      <span class="spacer"></span>
      <span class="muted small">${it.text.length > 60 ? it.text.length + ' 字' : ''}</span>
      <button class="icon-btn" data-act="delete" aria-label="删除">${ICON.trash}</button>
    </div>`;
}

function renderList() {
  const f = S.filter.trim().toLowerCase();
  const items = f ? S.items.filter(it => (it.text || it.name || '').toLowerCase().includes(f) || it.from.toLowerCase().includes(f)) : S.items;
  const list = $('#list');
  const open = new Set([...list.querySelectorAll('.item.open')].map(li => li.dataset.id));
  S.view = items;
  S.sel = Math.max(0, Math.min(S.sel, items.length - 1));
  list.innerHTML = items.map((it, i) =>
    `<li class="item card${i === 0 && !f ? ' latest' : ''}${open.has(it.id) ? ' open' : ''}${PANEL && i === S.sel ? ' sel' : ''}" data-id="${it.id}">${itemHTML(it, i === 0 && !f)}</li>`).join('');
  $('#empty').hidden = S.items.length > 0;
}

function renderDevices() {
  const others = S.devices.filter(d => !(d.host && S.local) && d.id !== S.you);
  const n = S.devices.length;
  $('#devCount').textContent = S.connected ? (others.length ? `${others.length + 1} 台设备` : '仅本机') : '重新连接中';
  $('#connDot').classList.toggle('on', S.connected);
  $('#deviceList').innerHTML = S.devices.map(d => {
    const me = d.id === S.you || (S.local && d.host);
    const sub = d.node ? (d.host && !S.local ? '你正通过它连接 · 运行 Lanvia' : '运行 Lanvia') : `浏览器 · 经由 ${esc(d.via || '')}`;
    return `<li><div class="dev-ic">${kindIcon(d.kind)}</div><div><div class="dev-name">${esc(d.name)} ${me ? '<span class="badge">本机</span>' : ''}</div><div class="dev-sub">${sub}</div></div></li>`;
  }).join('') || '<li class="muted">暂无</li>';
  if (S.local) {
    const card = $('#pairCard');
    if (!card.dataset.init) { // 首次：还没有别的设备时展开配对卡片
      card.dataset.init = 1;
      const pref = store.get('pairCollapsed');
      card.classList.toggle('collapsed', pref ? pref === '1' : n > 1);
    }
  }
}

let toastTimer;
function toast(msg, action) {
  const t = $('#toast');
  t.innerHTML = `<span class="t-msg">${esc(msg)}</span>` + (action ? `<button>${esc(action.label)}</button>` : '');
  if (action) $('button', t).onclick = () => { action.run(); t.classList.remove('show'); };
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), action ? 5000 : 2200);
}

/* ================= 实时连接 ================= */

function connect() {
  if (S.es) S.es.close();
  const es = new EventSource(signedURL('/api/events', { name: myName(), kind: KIND }));
  S.es = es;
  es.addEventListener('hello', e => {
    const d = JSON.parse(e.data);
    S.you = d.you; S.node = d.node; S.local = d.local; S.items = d.items || [];
    S.connected = true; S.retry = 0;
    document.body.classList.toggle('is-local', S.local);
    $('#settingsBtn').hidden = !S.local;
    $('#remoteOnly').hidden = S.local;
    $('#pairCard').hidden = !S.local || PANEL;
    if (S.local) loadInfo();
    renderList(); renderDevices();
  });
  es.addEventListener('item', e => {
    const it = JSON.parse(e.data);
    if (S.items.some(x => x.id === it.id)) return;
    const i = S.items.findIndex(x => x.ts < it.ts);
    i < 0 ? S.items.push(it) : S.items.splice(i, 0, it);
    renderList();
    const li = $(`.item[data-id="${it.id}"]`); li?.classList.add('flash');
    if (it.from !== myName() && Date.now() + S.offset - it.ts < 30e3) onIncoming(it);
  });
  es.addEventListener('delete', e => { const { id } = JSON.parse(e.data); S.items = S.items.filter(x => x.id !== id); renderList(); });
  es.addEventListener('clear', e => { const { ts } = JSON.parse(e.data); S.items = S.items.filter(x => x.ts > ts); renderList(); });
  es.addEventListener('devices', e => { S.devices = JSON.parse(e.data); renderDevices(); renderList(); });
  es.onerror = () => {
    es.close(); S.connected = false; renderDevices();
    const wait = Math.min(1000 * 2 ** S.retry++, 15000);
    setTimeout(async () => {
      if (S.es !== es) return;
      try { await api('GET', '/api/info'); } catch (err) { if (err.message === 'unauthorized') return; }
      if (S.es === es) connect();
    }, wait);
  };
}

function onIncoming(it) {
  if (document.hidden) { S.unseen++; document.title = `(${S.unseen}) Lanvia`; }
  if (it.kind === 'text') {
    const preview = it.text.replace(/\s+/g, ' ').slice(0, 36);
    // 电脑端已自动写入系统剪贴板，就不必再点
    if (S.local && S.info?.settings?.autoApply && S.info?.clipboard) toast(`来自 ${it.from}：已写入剪贴板`);
    else toast(`${it.from}：${preview}`, { label: '复制', run: () => doCopy(it.text, null, it.id) });
  } else {
    toast(`${it.from} 发来文件 ${it.name}`, { label: '下载', run: () => download(it) });
  }
}

async function doCopy(text, btn, id) {
  let ok = false;
  // 本机：由 Lanvia 直接写入系统剪贴板，不受浏览器/WebView 剪贴板权限限制
  if (S.local && id) ok = await api('POST', '/api/copy/' + id).then(() => true, () => false);
  if (!ok) ok = await copyText(text);
  if (ok && PANEL) { desktop('hide'); return; }
  if (btn) {
    const old = btn.innerHTML;
    btn.classList.add('ok'); btn.innerHTML = ICON.check + '<span>已复制</span>';
    setTimeout(() => { btn.classList.remove('ok'); btn.innerHTML = old; }, 1400);
  }
  if (!btn || !ok) toast(ok ? '已复制' : '复制失败，请长按文字手动复制');
}

async function saveLocal(it, then) {
  try {
    const { path } = await api('POST', `/api/save/${it.id}?then=${then}`);
    if (then === 'none') toast('已保存到 下载/Lanvia', { label: '显示', run: () => api('POST', '/api/reveal', { path }) });
  } catch (e) { toast('保存失败：' + e.message); }
}

function download(it) {
  if (S.local) return saveLocal(it, 'none');
  const a = document.createElement('a');
  a.href = signedURL('/api/blob/' + it.id, { dl: '1' });
  a.download = it.name;
  document.body.appendChild(a); a.click(); a.remove();
}

/* ================= 发送 ================= */

async function sendText() {
  const ta = $('#input');
  const text = ta.value;
  if (!text.trim()) return;
  $('#sendBtn').disabled = true;
  try {
    await api('POST', '/api/clip', { text, from: myName() });
    ta.value = ''; autosize();
    if (isTouch) ta.blur();
  } catch (e) {
    toast('发送失败：' + e.message);
  }
  updateSend();
}

function uploadFile(file) {
  const row = document.createElement('div');
  row.className = 'up';
  row.innerHTML = `<div class="up-row"><span class="up-name">${esc(file.name)}</span><span class="muted">0%</span></div><div class="bar"><i></i></div>`;
  $('#uploads').appendChild(row);
  const pct = $('.muted', row), bar = $('i', row);
  const xhr = new XMLHttpRequest();
  xhr.open('POST', signedURL('/api/upload', { name: file.name || 'file', from: myName() }, 'POST'));
  xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
  xhr.upload.onprogress = e => {
    if (!e.lengthComputable) return;
    const p = Math.round(e.loaded / e.total * 100);
    pct.textContent = `${p}% · ${fmtSize(e.total)}`; bar.style.width = p + '%';
  };
  const done = msg => { if (msg) toast(msg); setTimeout(() => row.remove(), msg ? 0 : 600); };
  xhr.onload = () => (xhr.status === 200 ? done() : done('上传失败：HTTP ' + xhr.status));
  xhr.onerror = () => done('上传失败，请检查网络');
  xhr.send(file);
}

function autosize() {
  const ta = $('#input');
  ta.style.height = 'auto';
  ta.style.height = Math.min(ta.scrollHeight, innerHeight * 0.4) + 'px';
}
function updateSend() { $('#sendBtn').disabled = !$('#input').value.trim(); }

/* ================= 本机仪表盘 ================= */

async function loadInfo() {
  try { S.info = await api('GET', '/api/info'); } catch { return; }
  const i = S.info;
  $('#pairCode').textContent = i.code;
  const sel = $('#urlSelect');
  const prev = sel.value;
  sel.innerHTML = (i.urls || []).map(u => `<option>${esc(u)}</option>`).join('') || '<option>未检测到局域网地址</option>';
  if (prev && i.urls?.includes(prev)) sel.value = prev;
  drawQR();
  $('#nodeName').value = i.name;
  $('#optWatch').checked = i.settings.watch;
  $('#optApply').checked = i.settings.autoApply;
  $('#clipWarn').hidden = !!i.clipboard;
  $('#token').textContent = i.token;
  const base = (i.urls || [])[0] || `http://localhost:${i.port}`;
  $('#curl').textContent =
`# 发送文本
curl -H "Authorization: Bearer ${i.token}" --data-binary "你好" ${base}/api/clip
# 获取最新文本
curl -H "Authorization: Bearer ${i.token}" ${base}/api/latest
# 发送文件
curl -H "Authorization: Bearer ${i.token}" --data-binary @photo.jpg "${base}/api/upload?name=photo.jpg"`;
  $('#dataDir').textContent = '数据目录：' + i.dataDir;
  $('#nearby').innerHTML = (i.nearby || []).map(n =>
    `<div class="nearby"><span>${kindIcon(osKind(n.os))} <b>${esc(n.name)}</b> 在同一网络，但配对码不同</span><span class="muted">${esc(n.addr.split(':')[0])}</span></div>`).join('');
}
const osKind = os => ({ darwin: 'mac', windows: 'windows', linux: 'linux' }[os] || 'web');

async function drawQR() {
  const url = $('#urlSelect').value;
  if (!S.info || !url.startsWith('http')) { $('#qr').innerHTML = ''; return; }
  try {
    const { size, bits } = await api('GET', '/api/qr?text=' + encodeURIComponent(`${url}/#k=${normCode(S.info.code)}`));
    let d = '';
    for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (bits[y * size + x] === '1') d += `M${x} ${y}h1v1h-1z`;
    $('#qr').innerHTML = `<svg viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges"><path fill="#111" d="${d}"/></svg>`;
  } catch {}
}

async function saveSettings(patch) {
  try { S.info = await api('POST', '/api/settings', patch); if (patch.name) { S.node.name = S.info.name; connect(); } }
  catch (e) { toast('保存失败：' + e.message); }
}

/* ================= 配对 ================= */

function showPair(err) {
  $('#app').hidden = true;
  $('#pair').hidden = false;
  $('#pairError').textContent = err || '';
  setTimeout(() => $('#pairInput').focus(), 50);
}
function unpair(msg) {
  store.del('code'); setCode(null);
  if (S.es) { S.es.close(); S.es = null; }
  showPair(msg);
}
async function tryCode(code) {
  setCode(code);
  try {
    const r = await fetch('/api/info', { headers: (() => { const { ts, sig } = sign('GET', '/api/info'); return { 'X-Lanvia-TS': ts, 'X-Lanvia-Sig': sig }; })() });
    if (r.status === 401) return false;
    return r.ok;
  } catch { return false; }
}
function startApp() {
  $('#pair').hidden = true;
  $('#app').hidden = false;
  connect();
}

/* ================= 事件绑定 ================= */

function bind() {
  $('#settingsBtn').innerHTML = ICON.gear;
  $('#pairCollapse').innerHTML = ICON.chevron;
  document.querySelectorAll('[data-close]').forEach(b => { b.innerHTML = ICON.close; b.onclick = () => b.closest('dialog').close(); });
  document.querySelectorAll('dialog').forEach(d => d.addEventListener('click', e => { if (e.target === d) d.close(); }));

  const ta = $('#input');
  ta.addEventListener('input', () => { autosize(); updateSend(); });
  ta.addEventListener('keydown', e => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey || (!isTouch && !e.shiftKey && !e.isComposing))) { e.preventDefault(); sendText(); }
  });
  $('#sendHint').textContent = isTouch ? '' : 'Enter 发送 · Shift+Enter 换行';
  $('#sendBtn').onclick = sendText;

  if (navigator.clipboard?.readText && window.isSecureContext) {
    $('#pasteBtn').hidden = false;
    $('#pasteBtn').onclick = async () => {
      try { ta.value = await navigator.clipboard.readText(); autosize(); updateSend(); ta.focus(); }
      catch { toast('无法读取剪贴板'); }
    };
  } else if (isTouch) {
    ta.placeholder = '长按此处粘贴，或输入要分享的内容…';
  }

  $('#fileInput').onchange = e => { [...e.target.files].forEach(uploadFile); e.target.value = ''; };

  // 粘贴截图/文件直接上传；在输入框之外按 ⌘V 直接发送文字
  document.addEventListener('paste', e => {
    if ($('#app').hidden) return;
    const files = [...(e.clipboardData?.files || [])];
    if (files.length) { e.preventDefault(); files.forEach(uploadFile); return; }
    const inField = e.target.closest?.('input, textarea, [contenteditable]');
    const text = e.clipboardData?.getData('text/plain');
    if (!inField && text) { e.preventDefault(); ta.value = text; sendText(); toast('已发送'); }
  });

  let dragDepth = 0;
  addEventListener('dragenter', e => { if ([...e.dataTransfer.types].includes('Files')) { dragDepth++; $('#drop').hidden = false; } });
  addEventListener('dragleave', () => { if (--dragDepth <= 0) { dragDepth = 0; $('#drop').hidden = true; } });
  addEventListener('dragover', e => e.preventDefault());
  addEventListener('drop', e => {
    e.preventDefault(); dragDepth = 0; $('#drop').hidden = true;
    [...(e.dataTransfer?.files || [])].forEach(uploadFile);
  });

  $('#list').addEventListener('click', e => {
    const el = e.target.closest('[data-act]');
    if (!el || e.target.closest('a')) return;
    const li = el.closest('.item');
    const it = S.items.find(x => x.id === li.dataset.id);
    if (!it) return;
    switch (el.dataset.act) {
      case 'copy':
        if (el.classList.contains('text') && getSelection().toString()) return; // 正在选择文字
        doCopy(it.text, el.classList.contains('btn') ? el : $('[data-act="copy"].btn', li), it.id);
        break;
      case 'code': doCopy(el.dataset.v, el); break;
      case 'expand': li.classList.toggle('open'); el.textContent = li.classList.contains('open') ? '收起' : '展开全部'; break;
      case 'download': download(it); break;
      case 'view': S.local ? saveLocal(it, 'open') : window.open(signedURL('/api/blob/' + it.id), '_blank', 'noopener'); break;
      case 'delete':
        api('DELETE', '/api/items/' + it.id).catch(err => toast('删除失败：' + err.message));
        break;
    }
  });

  $('#search').addEventListener('input', e => { S.filter = e.target.value; renderList(); });

  $('#devicesBtn').onclick = () => { $('#myName').value = myName(); $('#devicesDlg').showModal(); };
  $('#myName').addEventListener('change', e => {
    const v = e.target.value.trim();
    v ? store.set('name', v) : store.del('name');
    connect();
  });
  $('#forgetBtn').onclick = () => { if (confirm('退出后需要重新输入配对码。确定吗？')) { $('#devicesDlg').close(); unpair(); } };

  $('#settingsBtn').onclick = () => { if (PANEL) return desktop('main'); loadInfo(); $('#settingsDlg').showModal(); };
  $('#pairCollapse').onclick = () => {
    const c = $('#pairCard').classList.toggle('collapsed');
    store.set('pairCollapsed', c ? '1' : '0');
  };
  $('#urlSelect').onchange = drawQR;
  $('#nodeName').addEventListener('change', e => e.target.value.trim() && saveSettings({ name: e.target.value.trim() }));
  $('#optWatch').onchange = e => saveSettings({ watch: e.target.checked });
  $('#optApply').onchange = e => saveSettings({ autoApply: e.target.checked });
  $('#joinForm').onsubmit = async e => {
    e.preventDefault();
    const code = normCode($('#joinInput').value);
    if (code.length !== 10) return toast('配对码应为 10 位');
    try { S.info = await api('POST', '/api/join', { code }); $('#joinInput').value = ''; toast('已加入，正在寻找其他设备…'); loadInfo(); }
    catch (err) { toast(err.message); }
  };
  $('#resetCode').onclick = async () => {
    if (!confirm('所有已配对的手机和电脑都需要重新配对。确定重新生成吗？')) return;
    await api('POST', '/api/join', { reset: true }); loadInfo(); toast('已生成新的配对码');
  };
  $('#clearAll').onclick = async () => {
    if (!confirm('将清空所有已连接设备上的历史记录和文件。确定吗？')) return;
    await api('POST', '/api/clear'); $('#settingsDlg').close();
  };

  // 配对表单：输入时自动格式化
  const pi = $('#pairInput');
  pi.addEventListener('input', () => { const c = normCode(pi.value).slice(0, 10); pi.value = fmtCode(c); });
  $('#pairForm').onsubmit = async e => {
    e.preventDefault();
    const code = normCode(pi.value);
    if (code.length !== 10) { $('#pairError').textContent = '配对码应为 10 位'; return; }
    if (await tryCode(code)) { store.set('code', code); startApp(); }
    else { setCode(null); $('#pairError').textContent = '配对码不正确，或无法连接到这台电脑'; }
  };

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) return;
    S.unseen = 0; document.title = 'Lanvia';
    // iOS 切到后台会断开连接，回到前台立即重连
    if (!S.connected && !$('#app').hidden) { syncTime().then(connect); }
  });
  setInterval(() => { if (!document.hidden && S.items.length) renderList(); }, 60e3);
  if (PANEL) bindPanel();
}

/* 快捷面板：打开即搜索，↑↓ 选择，回车复制并收起 */
function bindPanel() {
  document.body.classList.add('panel');
  $('#kbdHint').hidden = false;
  const search = $('#search');
  search.placeholder = '搜索，↑↓ 选择';
  const move = d => {
    if (!S.view.length) return;
    S.sel = (S.sel + d + S.view.length) % S.view.length;
    document.querySelectorAll('#list .item').forEach((li, i) => li.classList.toggle('sel', i === S.sel));
    $('#list .item.sel')?.scrollIntoView({ block: 'nearest' });
  };
  document.addEventListener('keydown', e => {
    if (document.querySelector('dialog[open]')) return;
    const inComposer = e.target === $('#input');
    if (e.key === 'ArrowDown' && !inComposer) { e.preventDefault(); move(1); }
    else if (e.key === 'ArrowUp' && !inComposer) { e.preventDefault(); move(-1); }
    else if (e.key === 'Enter' && !inComposer && !e.isComposing) {
      const it = S.view[S.sel];
      if (!it) return;
      e.preventDefault();
      it.kind === 'text' ? doCopy(it.text, null, it.id) : saveLocal(it, 'reveal');
    } else if (e.key === 'Escape') { desktop('hide'); }
  });
  // 每次弹出都回到初始状态
  addEventListener('focus', () => {
    if (document.activeElement === $('#input')) return;
    search.value = ''; S.filter = ''; S.sel = 0; renderList();
    search.focus();
    if (!S.connected) syncTime().then(connect);
  });
}

async function boot() {
  bind();
  const m = location.hash.match(/k=([A-Za-z0-9-]+)/);
  if (m) {
    // 配对码放在 # 后面，不会发送给服务器，也不会留在地址栏里
    store.set('code', normCode(m[1]));
    history.replaceState(null, '', location.pathname);
  }
  await syncTime();
  const code = store.get('code');
  if (isLoopback) { if (code) setCode(code); startApp(); return; }
  if (!code) return showPair();
  setCode(code);
  startApp();
}

boot();
