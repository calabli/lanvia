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
const QS = new URLSearchParams(location.search);
const DESKTOP = QS.get('desktop');
const PANEL = DESKTOP === 'panel';
const PREVIEW = DESKTOP === 'preview'; // 桌面端的悬浮预览窗口
const PREVIEW_ID = QS.get('id');
const S = {
  key: null, offset: 0, items: [], devices: [], you: null, node: null, local: false,
  info: null, filter: '', kind: 'all', from: '', since: 0, fuzzy: false, es: null, connected: false, retry: 0, unseen: 0,
  view: [], sel: 0, // 当前显示的列表和键盘选中项（快捷面板）
};
const desktop = (action, id) => api('POST', '/api/desktop/' + action + (id ? '?id=' + id : '')).catch(() => {});

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
  if (S.key) { const { ts, sig } = sign(method, path.split('?')[0]); headers['X-Lanvia-TS'] = ts; headers['X-Lanvia-Sig'] = sig; }
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
  expand: I('<path d="M14 4h6v6M10 20H4v-6M20 4l-7 7M4 20l7-7"/>'),
  wrap: I('<path d="M4 6h16M4 12h13a3 3 0 0 1 0 6h-4M4 18h5"/><path d="m14 16-2 2 2 2"/>'),
  code: I('<path d="m8 8-4 4 4 4M16 8l4 4-4 4M13.5 5l-3 14"/>'),
  pencil: I('<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>'),
  history: I('<path d="M3.5 12a8.5 8.5 0 1 0 2.5-6"/><path d="M3.5 4.5V9H8M12 7.5V12l3 2"/>'),
  star: I('<path d="m12 3.8 2.5 5.1 5.6.8-4 4 .9 5.6-5-2.7-5 2.7.9-5.6-4-4 5.6-.8z"/>'),
  starOn: I('<path fill="currentColor" d="m12 3.8 2.5 5.1 5.6.8-4 4 .9 5.6-5-2.7-5 2.7.9-5.6-4-4 5.6-.8z"/>'),
  timer: I('<circle cx="12" cy="13" r="7.5"/><path d="M12 9.5V13l2.5 1.5M9.5 3h5"/>'),
  pin: I('<path d="M9 4h6l-1 6 4 3v2H6v-2l4-3z"/><path d="M12 15v5"/>'),
  globe: I('<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.5 2.7 2.5 14.3 0 17M12 3.5c-2.5 2.7-2.5 14.3 0 17"/>'),
  sun: I('<circle cx="12" cy="12" r="3.5"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>'),
  moon: I('<path d="M20.5 13.2A8.5 8.5 0 0 1 10.8 3.5a8.5 8.5 0 1 0 9.7 9.7Z"/>'),
  auto: I('<rect x="3" y="4" width="18" height="13" rx="3"/><path d="M8 21h8M12 17v4M12 4v13"/>'),
};
const kindIcon = k => ({ ios: ICON.phone, android: ICON.phone, ipad: ICON.tablet, mac: ICON.laptop, windows: ICON.laptop, linux: ICON.laptop }[k] || ICON.globe);

function applyTheme(theme) {
  if (theme === 'light' || theme === 'dark') document.documentElement.dataset.theme = theme;
  else { theme = 'system'; delete document.documentElement.dataset.theme; }
  store.set('theme', theme);
  const names = { system: '跟随系统', light: '浅色', dark: '深色' };
  const next = { system: '浅色', light: '深色', dark: '跟随系统' };
  const button = $('#themeBtn');
  button.innerHTML = { system: ICON.auto, light: ICON.sun, dark: ICON.moon }[theme];
  button.setAttribute('aria-label', `外观：${names[theme]}，切换为${next[theme]}`);
  button.title = `外观：${names[theme]}，点击切换`;
}

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
  const meta = `<div class="meta">${kindIcon(fromDev?.kind)}<span class="from">${esc(it.from)}</span><span>· ${relTime(it.ts)}</span>${latest ? '<span class="badge">最新</span>' : ''}${it.rev ? `<span class="badge soft" title="${esc(it.editedBy || '')} 修改">已编辑</span>` : ''}${it.fav ? '' : expiryBadge(it)}</div>${it.fav ? `<div class="fav-title">${ICON.starOn}<span>${it.title ? esc(it.title) : '已收藏'}</span></div>` : ''}`;
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
        <button class="icon-btn${it.fav ? ' on' : ''}" data-act="fav" aria-label="收藏" title="收藏">${it.fav ? ICON.starOn : ICON.star}</button>
        <button class="icon-btn" data-act="expiry" aria-label="保留时长" title="保留时长">${ICON.timer}</button>
        <button class="icon-btn" data-act="delete" aria-label="删除">${ICON.trash}</button>
      </div>`;
  }
  const long = isLong(it);
  const lines = it.text.split('\n').length;
  const url = (it.text.match(URL_RE) || [])[0];
  const code = findCode(it.text);
  const smart = [
    code ? `<button class="btn sm" data-act="code" data-v="${esc(code)}">${ICON.key}复制验证码 ${esc(code)}</button>` : '',
    url && url.length < it.text.length + 1 ? `<a class="btn sm" href="${esc(url)}" target="_blank" rel="noopener noreferrer">${ICON.link}打开链接</a>` : '',
  ].join('');
  return `${meta}<div class="text" data-act="copy">${linkify(it.text)}</div>
    ${long && !PANEL ? '<button class="more" data-act="expand">展开全部</button>' : ''}
    <div class="actions">
      <button class="btn sm" data-act="copy">${ICON.copy}<span>复制</span></button>
      ${long ? `<button class="btn sm" data-act="preview">${ICON.expand}<span>预览</span></button>` : ''}
      <span class="smart">${smart}</span>
      <span class="spacer"></span>
      <span class="muted small">${long ? textStats(it.text) : it.text.length > 60 ? it.text.length + ' 字' : ''}</span>
      <button class="icon-btn" data-act="edit" aria-label="编辑" title="编辑">${ICON.pencil}</button>
      <button class="icon-btn${it.fav ? ' on' : ''}" data-act="fav" aria-label="收藏" title="收藏">${it.fav ? ICON.starOn : ICON.star}</button>
      <button class="icon-btn" data-act="expiry" aria-label="保留时长" title="保留时长">${ICON.timer}</button>
      <button class="icon-btn" data-act="delete" aria-label="删除">${ICON.trash}</button>
    </div>`;
}

const isLong = it => it.kind === 'text' && (it.text.length > 280 || it.text.split('\n').length > 4);
const textStats = t => { const n = t.split('\n').length; return (n > 1 ? n + ' 行 · ' : '') + t.length + ' 字'; };

/* ---- 搜索：多个关键词（空格分隔）同时匹配；找不到时退回模糊匹配；按类型、设备、时间筛选 ---- */

const haystack = it => ((it.title || '') + '\n' + (it.text || it.name || '') + '\n' + it.from).toLowerCase();

// 模糊匹配：关键词的字符按顺序出现即可，间隔越小越相关；间隔太大（长文本里碰巧凑齐）不算
function fuzzyScore(h, q) {
  let from = 0, last = -1, gaps = 0;
  for (const c of q) {
    const j = h.indexOf(c, from);
    if (j < 0) return -1;
    if (last >= 0) gaps += j - last - 1;
    last = j; from = j + 1;
  }
  return gaps <= q.length * 6 ? gaps : -1;
}

function searchItems() {
  const now = Date.now() + S.offset;
  const base = S.items.filter(it =>
    (S.kind === 'all' || (S.kind === 'fav' ? it.fav : it.kind === S.kind)) &&
    (!S.from || it.from === S.from) &&
    (!S.since || it.ts >= now - S.since));
  const terms = S.filter.trim().toLowerCase().split(/\s+/).filter(Boolean);
  S.terms = terms; S.fuzzy = false;
  if (!terms.length) return base;
  const exact = base.filter(it => { const h = haystack(it); return terms.every(t => h.includes(t)); });
  if (exact.length) return exact;
  const q = terms.join('');
  const scored = base.map(it => [fuzzyScore(haystack(it), q), it]).filter(x => x[0] >= 0).sort((a, b) => a[0] - b[0]);
  S.fuzzy = scored.length > 0;
  return scored.map(x => x[1]);
}

// 在已渲染的文字里标出关键词（只处理文本节点，不影响链接等结构）
function markTerms(root, terms) {
  if (!terms.length) return;
  const re = new RegExp(terms.map(t => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'gi');
  root.querySelectorAll('.text, .fav-title span, .file-name').forEach(el => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    for (const n of nodes) {
      const s = n.nodeValue;
      re.lastIndex = 0;
      if (!re.test(s)) continue;
      re.lastIndex = 0;
      const frag = document.createDocumentFragment();
      let last = 0, m;
      while ((m = re.exec(s))) {
        if (!m[0]) { re.lastIndex++; continue; }
        frag.append(s.slice(last, m.index));
        const mk = document.createElement('mark'); mk.textContent = m[0]; frag.append(mk);
        last = m.index + m[0].length;
      }
      frag.append(s.slice(last));
      n.replaceWith(frag);
    }
  });
}

function renderFromFilter() {
  const sel = $('#fromFilter');
  if (!sel) return;
  const names = [...new Set(S.items.map(it => it.from))].sort();
  const key = names.join('\u0000');
  if (sel.dataset.key === key) return;
  sel.dataset.key = key;
  sel.innerHTML = '<option value="">所有设备</option>' + names.map(n => `<option value="${esc(n)}">${esc(n)}</option>`).join('');
  if (!names.includes(S.from)) S.from = '';
  sel.value = S.from;
}

function renderList() {
  renderFromFilter();
  const items = searchItems();
  const f = S.filter.trim() || S.from || S.since;
  const list = $('#list');
  const open = new Set([...list.querySelectorAll('.item.open')].map(li => li.dataset.id));
  S.view = items;
  S.sel = Math.max(0, Math.min(S.sel, items.length - 1));
  list.innerHTML = items.map((it, i) =>
    `<li class="item card${i === 0 && !f ? ' latest' : ''}${open.has(it.id) ? ' open' : ''}${PANEL && i === S.sel ? ' sel' : ''}${isLong(it) ? ' long' : ''}" data-id="${it.id}">${itemHTML(it, i === 0 && !f)}</li>`).join('');
  if (!S.fuzzy) markTerms(list, S.terms || []);
  $('#itemCount').textContent = String(items.length);
  const cap = $('.history-caption');
  if (cap) { cap.dataset.text ??= cap.textContent; cap.textContent = S.fuzzy ? '没有完全匹配的内容，以下是相近的结果' : cap.dataset.text; cap.classList.toggle('fuzzy', S.fuzzy); }
  $('#empty').hidden = items.length > 0;
  const filtered = !!f || S.kind !== 'all';
  $('#emptyTitle').textContent = filtered ? '还没有找到这份灵感。' : '好想法，从这里开始。';
  $('#emptyDescription').textContent = filtered ? '试试其他关键词，或切换内容类型。' : '在已连接的设备上复制文字，或发送你的第一条内容。';
}

function renderDevices() {
  const others = S.devices.filter(d => !(d.host && S.local) && d.id !== S.you);
  const n = S.devices.length;
  $('#devCount').textContent = S.connected ? (others.length ? `${others.length + 1} 台设备` : '仅本机') : '重新连接中';
  $('#connDot').classList.toggle('on', S.connected);
  document.body.dataset.connected = String(S.connected);
  $('#spaceStatus').textContent = S.connected ? '你的空间，已连接' : '正在重新连接';
  $('#nodeLabel').textContent = S.node?.name || 'Lanvia';
  const preview = S.devices.filter(d => d.id !== S.you);
  $('#devicePreview').innerHTML = preview.slice(0, 4).map(d =>
    `<span class="preview-device${d.host ? ' host' : ''}" title="${esc(d.name)}">${kindIcon(d.kind)}</span>`).join('') +
    (preview.length > 4 ? `<span class="preview-device">+${preview.length - 4}</span>` : '');
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
    $('#pairCollapse').setAttribute('aria-expanded', String(!card.classList.contains('collapsed')));
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
    if (d.devices) S.devices = d.devices;
    S.connected = true; S.retry = 0;
    document.body.classList.toggle('is-local', S.local);
    $('#settingsBtn').hidden = !S.local;
    $('#remoteOnly').hidden = S.local;
    $('#pairCard').hidden = !S.local || PANEL;
    if (S.local && !PREVIEW) loadInfo();
    renderList(); renderDevices();
    if (PREVIEW) {
      const it = S.items.find(x => x.id === PREVIEW_ID);
      if (!it) showPreviewMissing();
      else if (PV?.id !== it.id) showPreview(it, QS.get('mode') || 'view'); // 重连时不打断正在进行的编辑
    }
  });
  es.addEventListener('item', e => {
    const it = JSON.parse(e.data);
    if (S.items.some(x => x.id === it.id)) return;
    const i = S.items.findIndex(x => x.ts < it.ts);
    i < 0 ? S.items.push(it) : S.items.splice(i, 0, it);
    renderList();
    const li = $(`.item[data-id="${it.id}"]`); li?.classList.add('flash');
    if (!PREVIEW && it.from !== myName() && Date.now() + S.offset - it.ts < 30e3) onIncoming(it);
  });
  es.addEventListener('update', e => onItemUpdated(JSON.parse(e.data)));
  es.addEventListener('settings', e => {
    if (!S.info) return;
    S.info.settings = JSON.parse(e.data);
    $('#optApply').checked = S.info.settings.autoApply;
    $('#optKeep').value = String(S.info.settings.historyDays);
    $('#optFetch').value = String(S.info.settings.autoFetchMB);
    renderMode();
  });
  es.addEventListener('delete', e => {
    const { id } = JSON.parse(e.data);
    S.items = S.items.filter(x => x.id !== id); renderList();
    if (PV?.id === id) PREVIEW ? showPreviewMissing() : $('#previewDlg').close();
  });
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

/* ================= 预览、编辑与历史版本 ================= */

let PV = null; // 正在预览的内容
// mode: view 查看 | edit 编辑 | history 历史版本
const PVS = { mono: null, wrap: store.get('pv.wrap') !== '0', mode: 'view', base: '', hist: [], sel: null, diff: true };

// 看起来像代码 / 配置 / 日志时默认用等宽字体和行号
function looksLikeCode(t) {
  const ls = t.split('\n');
  if (ls.length < 3) return false;
  const hits = ls.filter(l => /^(\t| {2,})\S|[{};]\s*$|^\s*(\/\/|#|<\/?[a-z]|\$ |[a-z_]+\s*[:=])/i.test(l)).length;
  return hits / ls.length > 0.35;
}

function openPreview(it, mode = 'view') {
  // 快捷面板太窄，交给桌面端打开一个可拖动、可置顶的独立窗口
  if (PANEL) return desktop(mode === 'view' ? 'preview' : mode, it.id);
  showPreview(it, mode);
}

function showPreview(it, mode = 'view') {
  PV = it;
  PVS.mono = it.kind === 'text' ? looksLikeCode(it.text) : false;
  PVS.mode = 'view';
  $('#pvWarn').hidden = true;
  renderPreview();
  const d = $('#previewDlg');
  if (!d.open) PREVIEW ? d.show() : d.showModal();
  if (mode === 'edit') startEdit();
  else if (mode === 'history') openHistory();
  else { $('#pvBody').scrollTop = 0; $('#pvBody').focus({ preventScroll: true }); }
}

// 头部、工具栏、底栏都随模式变化
function renderPreview() {
  const it = PV, isText = it.kind === 'text', m = PVS.mode;
  const fromDev = S.devices.find(x => x.name === it.from);
  const edited = it.rev ? `<span class="badge soft" title="${esc(it.editedBy || '')} 修改于 ${relTime(it.editedAt)}">已编辑 · ${esc(it.editedBy || '')} ${relTime(it.editedAt)}</span>` : '';
  $('#pvMeta').innerHTML = `${kindIcon(fromDev?.kind)}<span class="from">${esc(it.from)}</span><span>· ${relTime(it.ts)}</span>${edited}${expiryBadge(it)}
    <span class="pv-stat">${isText ? textStats(it.text) : esc(it.name) + ' · ' + fmtSize(it.size || 0)}</span>`;
  const show = (id, on) => { $(id).hidden = !on; };
  show('#pvWrap', isText && m !== 'edit');
  show('#pvMono', isText);
  show('#pvEdit', isText && m === 'view');
  show('#pvHist', isText && m !== 'edit');
  show('#pvRestore', m === 'history' && PVS.sel && PVS.sel !== PVS.hist[0]?.id);
  show('#pvDiff', m === 'history');
  show('#pvCancel', m === 'edit');
  show('#pvSave', m === 'edit');
  show('#pvCopy', m !== 'edit');
  $('#pvHist').classList.toggle('on', m === 'history');
  $('#pvHist').innerHTML = ICON.history + (it.rev ? `<span class="count">${it.rev + 1}</span>` : '');
  $('#pvDiff').textContent = PVS.diff ? '看全文' : '看改动';
  $('#pvCopy').innerHTML = !isText ? `${ICON.download}<span>${S.local ? '保存' : '下载'}</span>`
    : m === 'history' ? `${ICON.copy}<span>复制此版本</span>` : `${ICON.copy}<span>复制全部</span>`;
  const mod = KIND === 'mac' ? '⌘' : 'Ctrl+';
  $('#pvFoot').innerHTML = !isText ? '<span><kbd>esc</kbd> 关闭</span>'
    : m === 'edit' ? `<span><kbd>${mod}S</kbd> 保存为新版本</span><span><kbd>esc</kbd> 取消</span><span>保存后会同步到所有设备，旧内容保留在历史版本里</span>`
    : m === 'history' ? '<span><kbd>↑</kbd><kbd>↓</kbd> 切换版本</span><span><kbd>esc</kbd> 返回</span>'
    : `<span><kbd>↩</kbd> 复制全部并关闭</span><span><kbd>${mod}E</kbd> 编辑</span><span>选中文字后 <kbd>${mod}C</kbd> 只复制选中部分</span><span><kbd>esc</kbd> 关闭</span>`;
  $('#previewDlg').classList.toggle('with-hist', m === 'history');
  if (m !== 'edit') renderPreviewBody();
}

function textHTML(text) {
  const lines = text.split('\n');
  if (PVS.mono && lines.length <= 20000) {
    return `<div class="pv-code${PVS.wrap ? ' wrap' : ''}" style="--gutter:${String(lines.length).length + 1}ch">${
      lines.map(l => `<div class="ln">${linkify(l) || ' '}</div>`).join('')}</div>`;
  }
  return `<div class="pv-text${PVS.mono ? ' mono' : ''}${PVS.wrap ? '' : ' nowrap'}">${linkify(text)}</div>`;
}

function renderPreviewBody() {
  const it = PV, body = $('#pvBody');
  $('#pvWrap').classList.toggle('on', PVS.wrap);
  $('#pvMono').classList.toggle('on', !!PVS.mono);
  if (it.kind === 'file') {
    const src = esc(signedURL('/api/blob/' + it.id));
    if (/^image\//.test(it.mime || '') && it.mime !== 'image/svg+xml') body.innerHTML = `<div class="pv-media"><img src="${src}" alt="${esc(it.name)}"></div>`;
    else if (/^video\//.test(it.mime || '')) body.innerHTML = `<div class="pv-media"><video src="${src}" controls autoplay></video></div>`;
    else body.innerHTML = `<div class="pv-file"><div class="file-icon">${ICON.file}</div><div class="file-name">${esc(it.name)}</div><div class="muted">${fmtSize(it.size || 0)} · ${esc(it.mime || '')}</div></div>`;
    return;
  }
  if (PVS.mode === 'history') return renderHistory();
  body.innerHTML = textHTML(it.text);
}

function showPreviewMissing() {
  PV = null; PVS.mode = 'view';
  $('#pvMeta').textContent = '';
  $('#pvBody').innerHTML = '<div class="pv-file muted">这条内容已被删除</div>';
  ['#pvCopy', '#pvEdit', '#pvHist', '#pvSave', '#pvCancel', '#pvRestore', '#pvDiff', '#pvWrap', '#pvMono'].forEach(s => { $(s).hidden = true; });
  $('#pvFoot').innerHTML = '';
  $('#previewDlg').classList.remove('with-hist');
  $('#pvHistList').innerHTML = '';
  const d = $('#previewDlg');
  if (!d.open) d.show();
}

async function closePreview() {
  if (PVS.mode === 'edit' && !(await cancelEdit())) return;
  if (PREVIEW) return desktop('hide-preview');
  $('#previewDlg').close();
}

async function previewCopyAll(andClose) {
  if (!PV) return;
  if (PV.kind !== 'text') { download(PV); return; }
  const text = PVS.mode === 'history' ? (PVS.hist.find(v => v.id === PVS.sel)?.text ?? PV.text) : PV.text;
  await doCopy(text, andClose ? null : $('#pvCopy'), PVS.mode === 'history' ? null : PV.id);
  if (andClose) closePreview();
}

/* ---- 编辑 ---- */

function startEdit() {
  if (!PV || PV.kind !== 'text') return;
  PVS.mode = 'edit';
  PVS.base = PV.text;
  $('#pvWarn').hidden = true;
  renderPreview();
  const body = $('#pvBody');
  body.innerHTML = `<textarea id="pvEditor" class="pv-edit${PVS.mono ? ' mono' : ''}" spellcheck="false" aria-label="编辑内容"></textarea>`;
  const ta = $('#pvEditor');
  ta.value = PV.text;
  ta.focus();
  ta.setSelectionRange(0, 0);
  ta.scrollTop = 0;
}

// 返回 true 表示已退出编辑
async function cancelEdit() {
  const ta = $('#pvEditor');
  if (ta && ta.value !== PVS.base && !(await uiConfirm('放弃这次修改吗？', { ok: '放弃修改', danger: true }))) return false;
  PVS.mode = 'view';
  $('#pvWarn').hidden = true;
  renderPreview();
  $('#pvBody').focus({ preventScroll: true });
  return true;
}

async function saveEdit() {
  const ta = $('#pvEditor');
  if (!ta || !PV) return;
  const text = ta.value;
  if (text === PV.text && text === PVS.base) { cancelEdit(); return; }
  if (!text.trim()) { toast('内容不能为空'); return; }
  $('#pvSave').disabled = true;
  try {
    const it = await api('POST', `/api/items/${PV.id}/edit`, { text, from: myName() });
    upsertItem(it);
    PV = it;
    PVS.mode = 'view';
    $('#pvWarn').hidden = true;
    renderPreview();
    flashButton($('#pvCopy'), `已保存 · 第 ${it.rev + 1} 版`);
  } catch (e) {
    toast('保存失败：' + e.message);
  } finally {
    $('#pvSave').disabled = false;
  }
}

/* ---- 历史版本 ---- */

async function openHistory() {
  if (!PV || PV.kind !== 'text') return;
  PVS.mode = 'history';
  try {
    const d = await api('GET', `/api/items/${PV.id}/versions`);
    PVS.hist = d.versions || [];
    if (!PVS.hist.some(v => v.id === PVS.sel)) PVS.sel = PVS.hist[0]?.id || null;
  } catch (e) { toast('读取历史失败：' + e.message); PVS.mode = 'view'; }
  renderPreview();
  $('#pvHistList').querySelector('.on')?.focus();
}

function closeHistory() { PVS.mode = 'view'; PVS.sel = null; renderPreview(); $('#pvBody').focus({ preventScroll: true }); }

function renderHistory() {
  const vs = PVS.hist, n = vs.length;
  $('#pvHistList').innerHTML = vs.map((v, i) => {
    const prev = vs[i + 1];
    const st = prev ? diffStats(prev.text, v.text) : null;
    const label = i === n - 1 ? '原始内容' : `第 ${n - i} 版`;
    return `<button class="hv${v.id === PVS.sel ? ' on' : ''}" data-vid="${v.id}">
      <span class="hv-top"><b>${label}</b>${i === 0 ? '<span class="badge">当前</span>' : ''}</span>
      <span class="hv-sub">${esc(v.by)} · ${relTime(v.ts)}</span>
      ${st ? `<span class="hv-delta"><i class="add">+${st.add}</i> <i class="del">−${st.del}</i> 行</span>` : `<span class="hv-delta">${v.text.length} 字</span>`}
    </button>`;
  }).join('');
  const i = vs.findIndex(v => v.id === PVS.sel);
  const v = vs[i], prev = vs[i + 1];
  const body = $('#pvBody');
  if (!v) { body.innerHTML = ''; return; }
  if (PVS.diff && prev) {
    const ops = diffLines(prev.text, v.text);
    body.innerHTML = ops ? diffHTML(ops) : `<div class="pv-note">内容太长，无法逐行对比，显示全文。</div>${textHTML(v.text)}`;
  } else {
    body.innerHTML = (prev || !PVS.diff ? '' : '<div class="pv-note">这是原始内容。</div>') + textHTML(v.text);
  }
}

function selectVersion(vid) {
  PVS.sel = vid;
  renderPreview();
  $('#pvHistList').querySelector('.on')?.focus();
}

async function restoreVersion() {
  const v = PVS.hist.find(x => x.id === PVS.sel);
  if (!v || !PV) return;
  try {
    const it = await api('POST', `/api/items/${PV.id}/edit`, { text: v.text, from: myName() });
    upsertItem(it);
    PV = it;
    PVS.sel = null;
    await openHistory();
    flashButton($('#pvCopy'), '已恢复为新版本');
  } catch (e) { toast('恢复失败：' + e.message); }
}

/* ---- 逐行对比（最长公共子序列，先去掉相同的首尾） ---- */

function diffLines(oldText, newText) {
  const a = oldText.split('\n'), b = newText.split('\n');
  let s = 0;
  while (s < a.length && s < b.length && a[s] === b[s]) s++;
  let ea = a.length, eb = b.length;
  while (ea > s && eb > s && a[ea - 1] === b[eb - 1]) { ea--; eb--; }
  const A = a.slice(s, ea), B = b.slice(s, eb), n = A.length, m = B.length;
  if (n * m > 4e6) return null;
  const w = m + 1, L = new Uint32Array((n + 1) * w);
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--)
    L[i * w + j] = A[i] === B[j] ? L[(i + 1) * w + j + 1] + 1 : Math.max(L[(i + 1) * w + j], L[i * w + j + 1]);
  const ops = a.slice(0, s).map(l => ['=', l]);
  let i = 0, j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && A[i] === B[j]) { ops.push(['=', A[i]]); i++; j++; }
    else if (i < n && (j === m || L[(i + 1) * w + j] >= L[i * w + j + 1])) ops.push(['-', A[i++]]); // 先删后增
    else ops.push(['+', B[j++]]);
  }
  return ops.concat(a.slice(ea).map(l => ['=', l]));
}

function diffStats(oldText, newText) {
  const ops = diffLines(oldText, newText);
  if (!ops) return null;
  return { add: ops.filter(o => o[0] === '+').length, del: ops.filter(o => o[0] === '-').length };
}

// 相同的大段内容折叠起来，只保留改动前后各 3 行
function diffHTML(ops) {
  if (!ops.some(o => o[0] !== '=')) return '<div class="pv-note">与上一版内容相同。</div>';
  const keep = new Uint8Array(ops.length);
  ops.forEach((o, i) => { if (o[0] !== '=') for (let k = Math.max(0, i - 3); k <= Math.min(ops.length - 1, i + 3); k++) keep[k] = 1; });
  // 只折叠 4 行以上的连续未改动内容，更短的直接显示
  for (let i = 0; i < ops.length;) {
    if (keep[i]) { i++; continue; }
    let j = i; while (j < ops.length && !keep[j]) j++;
    if (j - i < 4) keep.fill(1, i, j);
    i = j;
  }
  let out = '', skipped = 0;
  const flush = () => { if (skipped) { out += `<div class="df skip">⋯ ${skipped} 行未改动 ⋯</div>`; skipped = 0; } };
  ops.forEach((o, i) => {
    if (!keep[i]) { skipped++; return; }
    flush();
    const cls = o[0] === '+' ? 'add' : o[0] === '-' ? 'del' : '';
    out += `<div class="df ${cls}"><span class="sign">${o[0] === '=' ? ' ' : o[0] === '-' ? '−' : '+'}</span>${esc(o[1]) || ' '}</div>`;
  });
  flush();
  return `<div class="pv-diff${PVS.mono ? ' mono' : ''}">${out}</div>`;
}

/* ---- 列表数据更新（编辑同步过来时） ---- */

function upsertItem(it) {
  const i = S.items.findIndex(x => x.id === it.id);
  if (i >= 0) S.items[i] = it;
  renderList();
}

function onItemUpdated(it) {
  upsertItem(it);
  if (PV?.id !== it.id) return;
  if (PVS.mode === 'edit') {
    // 正在编辑时别人改了：提示，不打断
    if (it.text !== PVS.base) {
      $('#pvWarn').innerHTML = `${esc(it.editedBy || '其他设备')} 刚刚修改了这条内容。保存后你的版本会成为最新版本，对方的修改会保留在历史版本中。`;
      $('#pvWarn').hidden = false;
    }
    PV = it;
    return;
  }
  PV = it;
  if (PVS.mode === 'history') openHistory(); else renderPreview();
}

function bindPreview() {
  const d = $('#previewDlg');
  $('#pvCopy').onclick = () => previewCopyAll(false);
  $('#pvWrap').innerHTML = ICON.wrap;
  $('#pvMono').innerHTML = ICON.code;
  $('#pvPin').innerHTML = ICON.pin;
  $('#pvEdit').innerHTML = ICON.pencil;
  $('#pvWrap').onclick = () => { PVS.wrap = !PVS.wrap; store.set('pv.wrap', PVS.wrap ? '1' : '0'); renderPreviewBody(); };
  $('#pvMono').onclick = () => {
    PVS.mono = !PVS.mono;
    if (PVS.mode === 'edit') $('#pvEditor')?.classList.toggle('mono', PVS.mono), $('#pvMono').classList.toggle('on', PVS.mono);
    else renderPreviewBody();
  };
  $('#pvEdit').onclick = startEdit;
  $('#pvSave').onclick = saveEdit;
  $('#pvCancel').onclick = cancelEdit;
  $('#pvHist').onclick = () => (PVS.mode === 'history' ? closeHistory() : openHistory());
  $('#pvDiff').onclick = () => { PVS.diff = !PVS.diff; renderPreview(); };
  $('#pvRestore').onclick = restoreVersion;
  $('#pvHistList').addEventListener('click', e => { const b = e.target.closest('[data-vid]'); if (b) selectVersion(b.dataset.vid); });
  // 预览窗口默认悬浮在最上层，可以切换
  let pinned = true;
  $('#pvPin').hidden = !PREVIEW;
  $('#pvPin').classList.toggle('on', pinned);
  $('#pvPin').onclick = () => { pinned = !pinned; $('#pvPin').classList.toggle('on', pinned); desktop(pinned ? 'pin' : 'unpin'); };
  $('[data-close]', d).onclick = closePreview;
  d.addEventListener('cancel', e => { e.preventDefault(); closePreview(); });
  d.addEventListener('click', e => { if (e.target === d && !PREVIEW && PVS.mode !== 'edit') d.close(); });
  d.addEventListener('keydown', e => {
    if (e.isComposing || !PV) return;
    const mod = e.metaKey || e.ctrlKey, k = e.key.toLowerCase();
    if (PVS.mode === 'edit') {
      if ((mod && k === 's') || (mod && e.key === 'Enter')) { e.preventDefault(); saveEdit(); }
      else if (e.key === 'Escape') { e.preventDefault(); cancelEdit(); }
      return;
    }
    if (PVS.mode === 'history') {
      const i = PVS.hist.findIndex(v => v.id === PVS.sel);
      if (e.key === 'ArrowDown' && i < PVS.hist.length - 1) { e.preventDefault(); selectVersion(PVS.hist[i + 1].id); }
      else if (e.key === 'ArrowUp' && i > 0) { e.preventDefault(); selectVersion(PVS.hist[i - 1].id); }
      else if (e.key === 'Escape') { e.preventDefault(); closeHistory(); }
      else if (mod && k === 'c' && !getSelection()?.toString()) { e.preventDefault(); previewCopyAll(false); }
      return;
    }
    const sel = getSelection()?.toString() || '';
    if (e.key === 'Escape') { e.preventDefault(); closePreview(); }
    else if (mod && k === 'e' && PV.kind === 'text') { e.preventDefault(); startEdit(); }
    else if (e.key === 'Enter' && !mod && !e.target.closest('button')) { e.preventDefault(); previewCopyAll(true); }
    else if (e.key === ' ' && !sel && !e.target.closest('button, video')) { e.preventDefault(); closePreview(); }
    else if (mod && k === 'c' && PV.kind === 'text') {
      e.preventDefault();
      if (sel) copyText(sel).then(ok => flashButton($('#pvCopy'), ok ? '已复制选中' : '复制失败'));
      else previewCopyAll(false);
    }
  });
}

function flashButton(btn, label) {
  const old = btn.innerHTML;
  btn.classList.add('ok'); btn.innerHTML = ICON.check + `<span>${esc(label)}</span>`;
  setTimeout(() => { btn.classList.remove('ok'); btn.innerHTML = old; }, 1400);
}

/* ================= 发送模式（主动 / 被动） ================= */

const isActive = () => S.info?.settings?.watch !== false;

function renderMode() {
  if (!S.local || !S.info) return;
  const active = isActive();
  document.querySelectorAll('input[name="sendMode"]').forEach(r => { r.checked = (r.value === 'active') === active; });
  $('#modeHint').textContent = active
    ? '在这台电脑上复制文字，会自动发送到所有设备。'
    : '复制不会自动发送。需要时点“发送剪贴板”，或在托盘菜单里选“发送当前剪贴板”（桌面版）。';
  const b = $('#modeBtn');
  b.hidden = PREVIEW;
  b.classList.toggle('passive', !active);
  b.innerHTML = `<span class="mode-dot"></span>${active ? '主动' : '被动'}`;
  b.title = active ? '主动模式：复制即发送。点击切换为被动模式' : '被动模式：手动发送。点击切换为主动模式';
  $('#clipCard').hidden = active || PREVIEW;
  $('#clipHint').textContent = '也可以在托盘菜单里选“发送当前剪贴板”';
  if (!active) refreshClip();
}

async function setMode(active) {
  await saveSettings({ watch: active });
  renderMode();
  toast(active ? '已切换为主动模式：复制即发送' : '已切换为被动模式：需要手动发送');
}

let clipBusy = false;
async function refreshClip() {
  if (!S.local || !S.info || isActive() || PREVIEW || clipBusy) return;
  clipBusy = true;
  try {
    const c = await api('GET', '/api/clipboard');
    const box = $('#clipText');
    if (c.reason) {
      box.textContent = c.reason;
      box.classList.add('empty');
    } else {
      box.textContent = c.text + (c.length > [...c.text].length ? '…' : '');
      box.classList.remove('empty');
    }
    $('#clipInfo').textContent = c.reason ? '' : c.sent ? '已发送' : `${c.length} 字`;
    $('#clipSend').disabled = !!c.reason || c.sent;
    $('#clipSend').textContent = c.sent ? '已发送' : '发送剪贴板';
  } catch {} finally { clipBusy = false; }
}

async function sendClipboard() {
  try {
    await api('POST', '/api/send-clipboard');
    toast('已发送到所有设备');
  } catch (e) { toast(e.message); }
  refreshClip();
}

/* ================= 页内对话框（不依赖浏览器 / WebView 的 confirm、prompt） ================= */

function uiAsk(message, { input = false, value = '', placeholder = '', ok = '确定', cancel = '取消', danger = false } = {}) {
  return new Promise(resolve => {
    const d = $('#askDlg');
    $('#askMsg').textContent = message;
    const inp = $('#askInput');
    inp.hidden = !input; inp.value = value; inp.placeholder = placeholder;
    $('#askOk').textContent = ok; $('#askCancel').textContent = cancel;
    $('#askOk').classList.toggle('danger', danger);
    const done = v => { d.close(); resolve(v); };
    $('#askOk').onclick = () => done(input ? inp.value : true);
    $('#askCancel').onclick = () => done(input ? null : false);
    d.oncancel = e => { e.preventDefault(); done(input ? null : false); };
    inp.onkeydown = e => { if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); done(inp.value); } };
    d.showModal();
    (input ? inp : $('#askOk')).focus();
    if (input) inp.select();
  });
}
const uiConfirm = (msg, opt) => uiAsk(msg, opt);
const uiPrompt = (msg, opt) => uiAsk(msg, { ...opt, input: true });

/* ================= 收藏 / 常用片段 ================= */

async function setFav(it, fav, title = '') {
  try {
    const n = await api('POST', `/api/items/${it.id}/fav`, { fav, title });
    upsertItem(n);
    return n;
  } catch (e) { toast('设置失败：' + e.message); }
}

async function renameFav(it) {
  const title = await uiPrompt('给这条收藏起个名字，方便搜索（例如“家里 WiFi”）', { value: it.title || '', placeholder: '名称（可留空）', ok: '保存' });
  if (title !== null) { await setFav(it, true, title); }
}

function toggleFav(it, anchor) {
  if (!it.fav) {
    setFav(it, true).then(n => n && toast('已收藏：不会过期，在所有设备上都能找到', { label: '命名', run: () => renameFav(n) }));
    return;
  }
  openMenu(anchor, `收藏${it.title ? ' · ' + it.title : ''}`, [
    { label: it.title ? '重命名…' : '命名…', run: () => renameFav(it) },
    { label: '取消收藏', run: () => setFav(it, false).then(n => n && toast('已取消收藏')) },
  ]);
}

// 通用的小菜单（与保留时长菜单共用样式）
function openMenu(anchor, title, options) {
  const m = $('#expiryMenu');
  menuItem = null;
  m.innerHTML = `<div class="pop-title">${esc(title)}</div>` + options.map((o, i) => `<button role="menuitem" data-m="${i}">${esc(o.label)}</button>`).join('');
  m.onclickMenu = i => options[i].run();
  m.hidden = false;
  const r = anchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
  m.style.top = (r.bottom + h + 8 > innerHeight ? Math.max(8, r.top - h - 6) : r.bottom + 6) + 'px';
  $('button', m)?.focus();
}

/* ================= 单条保留时长 ================= */

const EXPIRY_OPTIONS = [
  { label: '1 小时后删除', ttl: 3600 },
  { label: '1 天后删除', ttl: 86400 },
  { label: '7 天后删除', ttl: 7 * 86400 },
  { label: '30 天后删除', ttl: 30 * 86400 },
  { label: '永久保留', keep: true },
  { label: '跟随全局设置', ttl: 0 },
];

function fmtRemain(ms) {
  if (ms < 3600e3) return Math.max(1, Math.round(ms / 60e3)) + ' 分钟后删除';
  if (ms < 86400e3) return Math.round(ms / 3600e3) + ' 小时后删除';
  return Math.round(ms / 86400e3) + ' 天后删除';
}

function expiryBadge(it) {
  if (it.keep) return `<span class="badge keep" title="不受全局保留时间影响">${ICON.pin}永久保留</span>`;
  if (it.expiresAt) return `<span class="badge expire" title="${new Date(it.expiresAt).toLocaleString()} 删除">${ICON.timer}${fmtRemain(it.expiresAt - Date.now() - S.offset)}</span>`;
  return '';
}

let menuItem = null;
function openExpiryMenu(it, anchor) {
  menuItem = it;
  const m = $('#expiryMenu');
  const cur = it.keep ? 'keep' : it.expiresAt ? 'ttl' : 'default';
  m.innerHTML = `<div class="pop-title">保留时长 <span class="muted">· 同步到所有设备</span></div>` + EXPIRY_OPTIONS.map((o, i) => {
    const on = (o.keep && cur === 'keep') || (o.ttl === 0 && !o.keep && cur === 'default');
    return `<button role="menuitem" data-i="${i}" class="${on ? 'on' : ''}">${o.label}${on ? ICON.check : ''}</button>`;
  }).join('') + (cur === 'ttl' ? `<div class="pop-foot">当前：${fmtRemain(it.expiresAt - Date.now() - S.offset)}</div>` : '');
  m.hidden = false;
  const r = anchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
  m.style.top = (r.bottom + h + 8 > innerHeight ? Math.max(8, r.top - h - 6) : r.bottom + 6) + 'px';
  $('button', m)?.focus();
}

function bindExpiryMenu() {
  const m = $('#expiryMenu');
  m.addEventListener('click', async e => {
    const g = e.target.closest('[data-m]');
    if (g) { m.hidden = true; m.onclickMenu?.(+g.dataset.m); return; }
    const b = e.target.closest('[data-i]');
    if (!b || !menuItem) return;
    const o = EXPIRY_OPTIONS[+b.dataset.i];
    m.hidden = true;
    try {
      const it = await api('POST', `/api/items/${menuItem.id}/expiry`, { ttl: o.ttl || 0, keep: !!o.keep });
      upsertItem(it);
      toast(o.keep ? '已设为永久保留' : o.ttl ? `已设置：${o.label}` : '已恢复跟随全局设置');
    } catch (err) { toast('设置失败：' + err.message); }
  });
  document.addEventListener('mousedown', e => { if (!m.hidden && !m.contains(e.target) && !e.target.closest('[data-act="expiry"], [data-act="fav"]')) m.hidden = true; });
  document.addEventListener('keydown', e => { if (e.key === 'Escape' && !m.hidden) { e.stopPropagation(); m.hidden = true; } }, true);
}

function renderStorage(u, quotaMB) {
  if (!u) return;
  const total = u.own + u.replicas, quota = quotaMB * 1048576;
  const base = quota || Math.max(total, 1);
  $('#stOwn').style.width = Math.min(100, u.own / base * 100) + '%';
  $('#stRep').style.width = Math.min(100, u.replicas / base * 100) + '%';
  $('#stText').innerHTML = `已用 <b>${fmtSize(total)}</b>${quota ? ' / ' + fmtSize(quota) : ''} · 原件 ${fmtSize(u.own)} · 副本 ${fmtSize(u.replicas)}（${u.count} 个）`;
  $('#clearReplicas').disabled = !u.count;
}

function renderManual(list) {
  $('#manualList').innerHTML = list.map(m => `<li>
    <span class="dot${m.connected ? ' on' : ''}"></span>
    <span class="mono">${esc(m.addr)}</span>
    <span class="muted small">${m.connected ? '已连接 · ' + esc(m.name) : '未连接，会自动重试'}</span>
    <button class="btn ghost sm" data-remove="${esc(m.addr)}">移除</button>
  </li>`).join('');
}

/* ================= 本机仪表盘 ================= */

async function loadInfo() {
  try { S.info = await api('GET', '/api/info'); } catch { return; }
  const i = S.info;
  $('#pairCode').textContent = i.code;
  const sel = $('#urlSelect');
  const prev = sel.value;
  sel.innerHTML = (i.urls || []).map(u => `<option>${esc(u)}</option>`).join('') || '<option>未检测到局域网地址</option>';
  if (prev && i.urls?.includes(prev)) sel.value = prev;
  // 只有一个地址时没必要让人选
  const single = (i.urls || []).length <= 1;
  sel.hidden = single;
  $('#urlText').hidden = !single;
  $('#urlText').textContent = (i.urls || [])[0] || '未检测到局域网地址';
  drawQR();
  $('#nodeName').value = i.name;
  renderMode();
  $('#optApply').checked = i.settings.autoApply;
  const keep = String(i.settings.historyDays ?? 30), ks = $('#optKeep');
  if (![...ks.options].some(o => o.value === keep)) ks.add(new Option(`${keep} 天`, keep)); // 手动改过配置文件的情况
  ks.value = keep;
  const fetchMB = String(i.settings.autoFetchMB ?? 100), fs = $('#optFetch');
  if (![...fs.options].some(o => o.value === fetchMB)) fs.add(new Option(`${fetchMB} MB 以下`, fetchMB));
  fs.value = fetchMB;
  const st = String(i.settings.storageMB ?? 2048), ss = $('#optStorage');
  if (![...ss.options].some(o => o.value === st)) ss.add(new Option(`${st} MB`, st));
  ss.value = st;
  renderStorage(i.storage, i.settings.storageMB);
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
  renderManual(i.manual || []);
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
  $('#app').hidden = PREVIEW;
  document.body.classList.toggle('preview-mode', PREVIEW);
  connect();
}

/* ================= 事件绑定 ================= */

function bind() {
  applyTheme(store.get('theme') || 'system');
  $('#themeBtn').onclick = () => {
    const current = document.documentElement.dataset.theme || 'system';
    applyTheme({ system: 'light', light: 'dark', dark: 'system' }[current]);
  };
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
  $('#filePicker').onclick = () => $('#fileInput').click();

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
      case 'view': case 'preview': openPreview(it); break;
      case 'edit': openPreview(it, 'edit'); break;
      case 'expiry': openExpiryMenu(it, el); break;
      case 'fav': toggleFav(it, el); break;
      case 'delete':
        api('DELETE', '/api/items/' + it.id).catch(err => toast('删除失败：' + err.message));
        break;
    }
  });

  $('#search').addEventListener('input', e => { S.filter = e.target.value; renderList(); });
  $('#fromFilter').onchange = e => { S.from = e.target.value; renderList(); };
  $('#timeFilter').onchange = e => { S.since = +e.target.value; renderList(); };
  $('#list').addEventListener('dblclick', e => {
    const li = e.target.closest('.item');
    const it = li && S.items.find(x => x.id === li.dataset.id);
    if (it && !e.target.closest('a, button, video')) { getSelection()?.removeAllRanges(); openPreview(it); }
  });
  bindPreview();
  $('#kindFilters').addEventListener('click', e => {
    const button = e.target.closest('[data-kind]');
    if (!button) return;
    S.kind = button.dataset.kind;
    $('#kindFilters').querySelectorAll('[data-kind]').forEach(b => {
      const active = b.dataset.kind === S.kind;
      b.classList.toggle('active', active);
      b.setAttribute('aria-pressed', String(active));
    });
    S.sel = 0;
    renderList();
  });
  $('.search-shortcut').textContent = KIND === 'mac' || KIND === 'ipad' || KIND === 'ios' ? '⌘ K' : 'Ctrl K';
  document.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k' && !$('#app').hidden && !document.querySelector('dialog[open]')) {
      e.preventDefault(); $('#search').focus();
    }
  });

  $('#devicesBtn').onclick = () => { $('#myName').value = myName(); $('#devicesDlg').showModal(); };
  $('#devicesPeek').onclick = () => $('#devicesBtn').click();
  $('#myName').addEventListener('change', e => {
    const v = e.target.value.trim();
    v ? store.set('name', v) : store.del('name');
    connect();
  });
  $('#forgetBtn').onclick = async () => { if (await uiConfirm('退出后需要重新输入配对码。确定吗？', { ok: '退出配对', danger: true })) { $('#devicesDlg').close(); unpair(); } };

  $('#settingsBtn').onclick = () => { if (PANEL) return desktop('main'); loadInfo(); $('#settingsDlg').showModal(); };
  $('#pairCollapse').onclick = () => {
    const c = $('#pairCard').classList.toggle('collapsed');
    store.set('pairCollapsed', c ? '1' : '0');
    $('#pairCollapse').setAttribute('aria-expanded', String(!c));
  };
  $('#urlSelect').onchange = drawQR;
  $('#nodeName').addEventListener('change', e => e.target.value.trim() && saveSettings({ name: e.target.value.trim() }));
  document.querySelectorAll('input[name="sendMode"]').forEach(r => { r.onchange = () => setMode(r.value === 'active'); });
  $('#modeBtn').onclick = () => setMode(!S.info?.settings?.watch);
  $('#clipSend').onclick = sendClipboard;
  bindExpiryMenu();
  addEventListener('focus', refreshClip);
  setInterval(() => { if (!document.hidden) refreshClip(); }, 2000);
  $('#optApply').onchange = e => saveSettings({ autoApply: e.target.checked });
  $('#optFetch').onchange = e => saveSettings({ autoFetchMB: +e.target.value });
  $('#optStorage').onchange = async e => {
    const mb = +e.target.value, u = S.info?.storage;
    if (mb && u && u.own + u.replicas > mb * 1048576 && !(await uiConfirm(`当前文件占用 ${fmtSize(u.own + u.replicas)}，超过新上限，将清理较旧的副本。确定吗？`, { ok: '确定' }))) {
      e.target.value = String(S.info.settings.storageMB); return;
    }
    await saveSettings({ storageMB: mb });
    renderStorage(S.info.storage, S.info.settings.storageMB);
  };
  $('#clearReplicas').onclick = async () => {
    const u = S.info?.storage;
    if (!u?.count) return toast('没有副本需要清理');
    if (!(await uiConfirm(`删除 ${u.count} 个副本，释放 ${fmtSize(u.replicas)}？条目会保留，需要时再从其他设备获取。`, { ok: '清理', danger: true }))) return;
    const r = await api('POST', '/api/storage/clear-replicas');
    S.info = r.info; renderStorage(r.info.storage, r.info.settings.storageMB);
    toast(`已清理 ${r.count} 个副本，释放 ${fmtSize(r.freed)}`);
  };
  $('#optKeep').onchange = async e => {
    const days = +e.target.value;
    const n = days && S.items.filter(it => it.ts < Date.now() + S.offset - days * 864e5).length;
    if (n && !(await uiConfirm(`将立即删除这台电脑上 ${n} 条超过 ${days} 天的内容，确定吗？`, { ok: '删除', danger: true }))) { e.target.value = String(S.info.settings.historyDays); return; }
    saveSettings({ historyDays: days });
  };
  $('#manualForm').onsubmit = async e => {
    e.preventDefault();
    const input = $('#manualInput'), msg = $('#manualMsg'), btn = $('#manualForm button');
    if (!input.value.trim()) return;
    btn.disabled = true; msg.className = 'small muted'; msg.textContent = '正在连接…';
    try {
      const r = await api('POST', '/api/peers', { addr: input.value });
      S.info = r.info; renderManual(r.info.manual || []);
      if (r.error) { msg.className = 'small warn'; msg.textContent = r.error; }
      else { msg.className = 'small ok-text'; msg.textContent = r.peer.connected ? `已连接：${r.peer.name}` : '已添加，正在连接…'; input.value = ''; }
    } catch (err) { msg.className = 'small warn'; msg.textContent = err.message; }
    btn.disabled = false;
  };
  $('#manualList').addEventListener('click', async e => {
    const b = e.target.closest('[data-remove]');
    if (!b) return;
    S.info = await api('POST', '/api/peers', { addr: b.dataset.remove, remove: true });
    renderManual(S.info.manual || []);
    $('#manualMsg').textContent = '';
  });
  $('#joinForm').onsubmit = async e => {
    e.preventDefault();
    const code = normCode($('#joinInput').value);
    if (code.length !== 10) return toast('配对码应为 10 位');
    try { S.info = await api('POST', '/api/join', { code }); $('#joinInput').value = ''; toast('已加入，正在寻找其他设备…'); loadInfo(); }
    catch (err) { toast(err.message); }
  };
  $('#resetCode').onclick = async () => {
    if (!(await uiConfirm('所有已配对的手机和电脑都需要重新配对。确定重新生成吗？', { ok: '重新生成', danger: true }))) return;
    await api('POST', '/api/join', { reset: true }); loadInfo(); toast('已生成新的配对码');
  };
  $('#clearAll').onclick = async () => {
    if (!(await uiConfirm('将清空所有已连接设备上的历史记录和文件。确定吗？', { ok: '清空', danger: true }))) return;
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
    else if (((e.key === ' ' && e.target === search && !search.value) || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'y')) && S.view[S.sel]) {
      e.preventDefault(); openPreview(S.view[S.sel]);
    }
    else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'e' && S.view[S.sel]?.kind === 'text') {
      e.preventDefault(); openPreview(S.view[S.sel], 'edit');
    }
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
