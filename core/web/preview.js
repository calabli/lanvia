'use strict';

// 预览窗口：查看、代码高亮、编辑、历史版本与逐行对比。

/* ================= 预览、编辑与历史版本 ================= */

let PV = null; // 正在预览的内容
// mode: view 查看 | edit 编辑 | history 历史版本
const PVS = { mono: null, wrap: store.get('pv.wrap') !== '0', mode: 'view', base: '', hist: [], sel: null, diff: true, lang: null, fmt: false };

// 预览里显示的文本：JSON 可以切换为格式化后的样子
function pvText() {
  if (PVS.fmt && PVS.lang === 'json' && PVS.mode === 'view') {
    try { return JSON.stringify(JSON.parse(PV.text), null, 2); } catch {}
  }
  return PV.text;
}

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

async function showPreview(it, mode = 'view') {
  // 列表里的长正文是截断的，预览前先取全文
  if (it.textLen) {
    try { it = await loadFull(it); } catch (e) { toast('加载失败：' + e.message); return; }
  }
  PV = it;
  PVS.lang = it.kind === 'text' ? HL.detect(it.text) : null;
  PVS.mono = it.kind === 'text' ? (!!PVS.lang || looksLikeCode(it.text)) : false;
  // 单行的长 JSON 默认就展开，方便阅读
  PVS.fmt = PVS.lang === 'json' && !it.text.trim().includes('\n') && it.text.length > 80;
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
  const edited = it.rev ? `<span class="badge soft"><span>已编辑</span> · <span data-raw>${esc(it.editedBy || '')}</span> <span>${relTime(it.editedAt)}</span></span>` : '';
  $('#pvMeta').innerHTML = `${kindIcon(fromDev?.kind)}<span class="from" data-raw>${esc(it.from)}</span><span>· ${relTime(it.ts)}</span>${edited}${expiryBadge(it)}
    ${PVS.lang ? `<span class="badge soft lang">${esc(HL.name(PVS.lang))}</span>` : ''}
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
  show('#pvFmt', PVS.lang === 'json' && m === 'view');
  $('#pvFmt').classList.toggle('on', PVS.fmt);
  $('#pvFmt').textContent = PVS.fmt ? '原文' : '格式化';
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

const ROW_CHUNK = 1500;
const rowsHTML = (from, to) => PVS.rows.slice(from, to).map(l => `<div class="ln">${l || ' '}</div>`).join('');

// 预览滚动到底部附近时追加下一批行（行号由 CSS 计数器自动接续）
function moreRows() {
  const box = $('#pvBody'), code = $('.pv-code', box);
  if (!code || !PVS.rows || PVS.shown >= PVS.rows.length) return;
  if (box.scrollTop + box.clientHeight < box.scrollHeight - 1200) return;
  const to = Math.min(PVS.rows.length, PVS.shown + ROW_CHUNK);
  code.insertAdjacentHTML('beforeend', rowsHTML(PVS.shown, to));
  PVS.shown = to;
}

function textHTML(text) {
  const lines = text.split('\n');
  if (PVS.mono && lines.length <= 200000) { // 分批渲染，几万行也不卡
    // 识别出语言时按语法着色，否则只加行号和链接
    const rows = PVS.lang ? HL.lines(text, PVS.lang) : lines.map(linkify);
    // 每行一个元素，几万行一次性放进页面会很卡：先放一批，滚动到底部附近再接着放
    PVS.rows = rows;
    PVS.shown = Math.min(rows.length, ROW_CHUNK);
    return `<div class="pv-code${PVS.wrap ? ' wrap' : ''}" data-raw style="--gutter:${String(lines.length).length + 1}ch">${
      rowsHTML(0, PVS.shown)}</div>`;
  }
  return `<div class="pv-text${PVS.mono ? ' mono' : ''}${PVS.wrap ? '' : ' nowrap'}" data-raw>${linkify(text)}</div>`;
}

function renderPreviewBody() {
  const it = PV, body = $('#pvBody');
  $('#pvWrap').classList.toggle('on', PVS.wrap);
  $('#pvMono').classList.toggle('on', !!PVS.mono);
  if (it.kind === 'file') {
    const img = /^image\//.test(it.mime || '') && it.mime !== 'image/svg+xml', vid = /^video\//.test(it.mime || '');
    if (!img && !vid) {
      body.innerHTML = `<div class="pv-file"><div class="file-icon">${ICON.file}</div><div class="file-name" data-raw>${esc(it.name)}</div><div class="muted">${fmtSize(it.size || 0)} · ${esc(it.mime || '')}</div></div>`;
      return;
    }
    // 手机上要先下载、解密：显示进度，完成后再放进 <img> / <video>
    body.innerHTML = `<div class="pv-media"><div class="pv-file muted pv-loading">${S.key ? '正在加载…' : ''}</div></div>`;
    const size = it.size || 0, tip = $('.pv-loading', body);
    mediaURL('/api/blob/' + it.id, it, got => { if (size && tip) tip.textContent = `正在加载 ${Math.round(got / size * 100)}%`; }).then(url => {
      if (PV !== it) return; // 加载期间换了别的内容
      $('.pv-media', body).innerHTML = img ? `<img src="${esc(url)}" alt="${esc(it.name)}">` : `<video src="${esc(url)}" controls autoplay></video>`;
    }, e => { if (PV === it && tip) tip.textContent = '加载失败：' + e.message; });
    return;
  }
  if (PVS.mode === 'history') return renderHistory();
  body.innerHTML = textHTML(pvText());
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
  const text = PVS.mode === 'history' ? (PVS.hist.find(v => v.id === PVS.sel)?.text ?? PV.text) : pvText();
  // 历史版本、格式化后的 JSON 和原文不同，不能按条目 ID 让服务端复制
  const sameAsItem = PVS.mode !== 'history' && text === PV.text;
  await doCopy(text, andClose ? null : $('#pvCopy'), sameAsItem ? PV.id : null);
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
      <span class="hv-sub"><span data-raw>${esc(v.by)}</span> · <span>${relTime(v.ts)}</span></span>
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
    out += `<div class="df ${cls}" data-raw><span class="sign">${o[0] === '=' ? ' ' : o[0] === '-' ? '−' : '+'}</span>${esc(o[1]) || ' '}</div>`;
  });
  flush();
  return `<div class="pv-diff${PVS.mono ? ' mono' : ''}">${out}</div>`;
}

/* ---- 列表数据更新（编辑同步过来时） ---- */

function upsertItem(it) {
  if (!it.textLen) FULL.delete(it.id); // 内容变了，缓存的全文作废（API 返回的完整内容另存）
  else if (FULL.get(it.id)?.rev !== it.rev) FULL.delete(it.id);
  const i = S.items.findIndex(x => x.id === it.id);
  if (i >= 0) S.items[i] = it;
  renderList();
}

async function onItemUpdated(it) {
  upsertItem(it);
  if (PV?.id !== it.id) return;
  if (it.textLen) { try { it = await loadFull(it); } catch { return; } }
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
  $('#pvBody').addEventListener('scroll', moreRows, { passive: true });
  $('#pvWrap').onclick = () => { PVS.wrap = !PVS.wrap; store.set('pv.wrap', PVS.wrap ? '1' : '0'); renderPreviewBody(); };
  $('#pvFmt').onclick = () => { PVS.fmt = !PVS.fmt; renderPreview(); };
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
