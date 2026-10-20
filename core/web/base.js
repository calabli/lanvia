'use strict';

// 基础：工具函数、签名与鉴权、设备识别、图标、剪贴板。其余脚本都依赖这里。

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
