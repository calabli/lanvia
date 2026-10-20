'use strict';

// 列表与收发：渲染、搜索、实时连接、发送文字 / 文件 / 文件夹。

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
  const meta = `<div class="meta">${kindIcon(fromDev?.kind)}<span class="from" data-raw>${esc(it.from)}</span><span>· ${relTime(it.ts)}</span>${latest ? '<span class="badge">最新</span>' : ''}${it.rev ? `<span class="badge soft" title="${esc(it.editedBy || '')} 修改">已编辑</span>` : ''}${it.fav ? '' : expiryBadge(it)}</div>${it.fav ? `<div class="fav-title">${ICON.starOn}${it.title ? `<span data-raw>${esc(it.title)}</span>` : '<span>已收藏</span>'}</div>` : ''}`;
  if (it.kind === 'file') {
    const isImg = /^image\/(png|jpe?g|gif|webp|heic|avif|bmp)$/i.test(it.mime || '');
    const isVid = /^video\//.test(it.mime || '');
    const media = isImg
      ? `<img class="thumb" loading="lazy" alt="${esc(it.name)}" src="${esc(signedURL('/api/blob/' + it.id))}" data-act="view">`
      : isVid ? `<video class="thumb" controls preload="metadata" src="${esc(signedURL('/api/blob/' + it.id))}"></video>` : '';
    return `${meta}${media}
      <div class="file"><div class="file-icon">${ICON.file}</div><div><div class="file-name" data-raw>${esc(it.name)}</div><div class="muted small">${fmtSize(it.size || 0)}</div></div></div>
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
  // 只在全文（摘要之外）里搜到时，显示命中位置的前后文
  const dh = deepHit(it);
  const terms = S.terms || [];
  const deep = dh && !terms.every(t => haystack(it).includes(t))
    ? `<div class="deep-hit"><span>在全文第 ${dh.line} 行找到：</span><span class="deep-snip" data-raw>${esc(dh.snippet)}</span></div>` : '';
  return `${meta}<div class="text" data-act="copy" data-raw>${linkify(it.text)}</div>${deep}
    ${long && !PANEL ? '<button class="more" data-act="expand">展开全部</button>' : ''}
    <div class="actions">
      <button class="btn sm" data-act="copy">${ICON.copy}<span>复制</span></button>
      ${long ? `<button class="btn sm" data-act="preview">${ICON.expand}<span>预览</span></button>` : ''}
      <span class="smart">${smart}</span>
      <span class="spacer"></span>
      <span class="muted small">${long ? itemStats(it) : it.text.length > 60 ? it.text.length + ' 字' : ''}</span>
      <button class="icon-btn" data-act="edit" aria-label="编辑" title="编辑">${ICON.pencil}</button>
      <button class="icon-btn${it.fav ? ' on' : ''}" data-act="fav" aria-label="收藏" title="收藏">${it.fav ? ICON.starOn : ICON.star}</button>
      <button class="icon-btn" data-act="expiry" aria-label="保留时长" title="保留时长">${ICON.timer}</button>
      <button class="icon-btn" data-act="delete" aria-label="删除">${ICON.trash}</button>
    </div>`;
}

const isLong = it => it.kind === 'text' && (it.text.length > 280 || it.text.split('\n').length > 4);
const textStats = t => { const n = t.split('\n').length; return (n > 1 ? n + ' 行 · ' : '') + t.length + ' 字'; };
// 列表里的长正文是截断的：字数用全文的，行数只能说“以上”
const itemStats = it => it.textLen ? `${it.text.split('\n').length}+ 行 · ${it.textLen} 字` : textStats(it.text);

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

// 长正文的列表里只有前 16 KB，超出部分请节点在全文里搜：{ q, hits: Map(id -> { line, snippet }) }
S.deep = { q: '', hits: new Map() };
let deepTimer;

function deepSearch() {
  const q = S.filter.trim();
  clearTimeout(deepTimer);
  if (!q || !S.items.some(it => it.textLen)) { S.deep = { q: '', hits: new Map() }; return; }
  if (S.deep.q === q) return;
  deepTimer = setTimeout(async () => {
    try {
      const hits = await api('GET', '/api/search?q=' + encodeURIComponent(q));
      if (S.filter.trim() !== q) return; // 已经改了关键词
      S.deep = { q, hits: new Map(hits.map(h => [h.id, h])) };
      renderList();
    } catch {}
  }, 250);
}

const deepHit = it => (S.deep.q && S.deep.q === S.filter.trim() ? S.deep.hits.get(it.id) : undefined);

function searchItems() {
  const now = Date.now() + S.offset;
  const base = S.items.filter(it =>
    (S.kind === 'all' || (S.kind === 'fav' ? it.fav : it.kind === S.kind)) &&
    (!S.from || it.from === S.from) &&
    (!S.since || it.ts >= now - S.since));
  const terms = S.filter.trim().toLowerCase().split(/\s+/).filter(Boolean);
  S.terms = terms; S.fuzzy = false;
  if (!terms.length) return base;
  const exact = base.filter(it => { const h = haystack(it); return terms.every(t => h.includes(t)) || deepHit(it); });
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
  root.querySelectorAll('.text, .fav-title span, .file-name, .deep-snip').forEach(el => {
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
  sel.innerHTML = '<option value="">所有设备</option>' + names.map(n => `<option value="${esc(n)}" data-raw>${esc(n)}</option>`).join('');
  if (!names.includes(S.from)) S.from = '';
  sel.value = S.from;
}

/* ---- 列表渲染：增量更新 + 分页 ----
   每张卡片按“渲染结果”做缓存，只有内容真正变化的卡片才重建，其余原样保留（不重新解析、不重播动画）。
   先显示 PAGE 条，滚动到底部再加载更多。 */

const PAGE = 50;
const ROWS = new Map(); // id -> { key, el }
S.limit = PAGE;

function renderList() {
  // 页面不可见（桌面版隐藏的窗口、切到后台的标签页）时先不渲染，回到前台再补
  if (document.hidden) { S.renderPending = true; return; }
  S.renderPending = false;
  renderFromFilter();
  const items = searchItems();
  const f = S.filter.trim() || S.from || S.since;
  const fkey = [S.filter, S.kind, S.from, S.since].join('\u0001');
  if (fkey !== S.lastFilter) { S.lastFilter = fkey; S.limit = PAGE; } // 换了筛选条件就回到第一页
  S.view = items;
  S.sel = Math.max(0, Math.min(S.sel, items.length - 1));
  if (PANEL && S.sel >= S.limit) S.limit = S.sel + PAGE;
  const list = $('#list');
  const terms = S.fuzzy ? '' : (S.terms || []).join('\u0002');
  const wanted = [];
  items.slice(0, S.limit).forEach((it, i) => {
    const prev = ROWS.get(it.id);
    const latest = i === 0 && !f;
    const cls = `item card${latest ? ' latest' : ''}${prev?.el.classList.contains('open') ? ' open' : ''}${PANEL && i === S.sel ? ' sel' : ''}${isLong(it) ? ' long' : ''}`;
    const html = itemHTML(it, latest);
    const key = cls + '\u0001' + terms + '\u0001' + html;
    let el = prev?.el;
    if (!prev || prev.key !== key) {
      el = document.createElement('li');
      el.className = cls;
      el.dataset.id = it.id;
      el.innerHTML = html;
      if (terms) markTerms(el, S.terms);
      if (prev) el.style.animation = 'none'; // 只是内容更新，不重播出现动画
      ROWS.set(it.id, { key, el });
    }
    wanted.push(el);
  });
  // 删掉不再显示的，再按顺序就位（已在正确位置的不动）
  const keep = new Set(wanted);
  for (const [id, r] of ROWS) if (!keep.has(r.el)) { r.el.remove(); ROWS.delete(id); }
  for (const c of [...list.children]) if (!keep.has(c)) c.remove();
  let cur = list.firstElementChild;
  for (const el of wanted) {
    if (el === cur) { cur = cur.nextElementSibling; continue; }
    list.insertBefore(el, cur);
  }
  const rest = items.length - Math.min(items.length, S.limit);
  $('#listMore').hidden = rest <= 0;
  $('#listMoreBtn').textContent = `显示更多（还有 ${rest} 条）`;
  $('#itemCount').textContent = String(items.length);
  const cap = $('.history-caption');
  if (cap) { cap.textContent = S.fuzzy ? '没有完全匹配的内容，以下是相近的结果' : '每一份灵感，都有迹可循。'; cap.classList.toggle('fuzzy', S.fuzzy); }
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
    const enc = d.node && !d.host ? (d.enc ? ' · 已加密' : ' · 未加密（旧版本）') : '';
    const sub = d.node ? (d.host && !S.local ? '你正通过它连接 · 运行 Lanvia' : '运行 Lanvia' + enc) : `浏览器 · 经由 ${esc(d.via || '')}`;
    return `<li><div class="dev-ic">${kindIcon(d.kind)}</div><div><div class="dev-name"><span data-raw>${esc(d.name)}</span> ${me ? '<span class="badge">本机</span>' : ''}</div><div class="dev-sub">${sub}</div></div></li>`;
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
    $('#optStrict').checked = !!S.info.settings.strictEnc;
    $('#optNotify').checked = S.info.settings.notify !== false;
    renderBlock();
    if ((S.info.settings.lang || '') !== (store.get('lang') || '')) applyLang(S.info.settings.lang || '');
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
    else toast(`${it.from}：${preview}`, { label: '复制', run: () => copyItem(it) });
  } else {
    toast(`${it.from} 发来文件 ${it.name}`, { label: '下载', run: () => download(it) });
  }
}

/* ---- 长内容：列表里只有前 16 KB，全文按需加载 ---- */

const FULL = new Map(); // id -> 完整内容

async function loadFull(it) {
  if (!it.textLen) return it;
  const hit = FULL.get(it.id);
  if (hit && hit.rev === it.rev) return hit;
  const full = await api('GET', `/api/items/${it.id}`);
  FULL.set(it.id, full);
  return full;
}

// 复制一条文本。电脑上由 Lanvia 按 ID 写入系统剪贴板（总是全文）；
// 手机上写剪贴板必须在点击的同一时刻完成，长内容第一次要先加载全文，再点一次“复制”。
function copyItem(it, btn) {
  if (!it.textLen || S.local) return doCopy(it.text, btn, it.id);
  const hit = FULL.get(it.id);
  if (hit && hit.rev === it.rev) return doCopy(hit.text, btn);
  toast('正在加载全文…');
  loadFull(it).then(
    f => toast(`全文已加载（${f.text.length} 字），点此复制`, { label: '复制', run: () => doCopy(f.text) }),
    e => toast('加载失败：' + e.message));
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

/* ---- 发送文件夹：先在本机打包成 zip，再按普通文件上传 ---- */

async function sendFolderEntry(entry) {
  const out = [];
  try { await Zip.readEntry(entry, '', out); }
  catch { toast('读取文件夹失败'); return; }
  sendFolder(entry.name, out);
}

async function sendFolder(name, entries) {
  const row = document.createElement('div');
  row.className = 'up';
  const count = entries.filter(e => e.file).length;
  row.innerHTML = `<div class="up-row"><span class="up-name">${esc(name)}/ · ${count} 个文件</span><span class="muted">正在打包…</span></div><div class="bar"><i></i></div>`;
  $('#uploads').appendChild(row);
  const pct = $('.muted', row), bar = $('i', row);
  try {
    const blob = await Zip.build(entries, (done, total) => {
      const p = total ? Math.round(done / total * 100) : 100;
      pct.textContent = `正在打包 ${p}%`; bar.style.width = p + '%';
    });
    row.remove();
    uploadFile(new File([blob], name + '.zip', { type: 'application/zip' }));
  } catch (e) {
    row.remove();
    toast(e.message || '打包失败');
  }
}

function autosize() {
  const ta = $('#input');
  ta.style.height = 'auto';
  ta.style.height = Math.min(ta.scrollHeight, innerHeight * 0.4) + 'px';
}
function updateSend() { $('#sendBtn').disabled = !$('#input').value.trim(); }
