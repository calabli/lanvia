'use strict';

// Main window and browsers: a single centered stream with a device beam and a sync capsule at the top,
// an Omni-Bar that both searches and sends, cards that copy on a single click, and a drawer (the
// #previewDlg dialog) that slides in on demand for details, editing, comments and history.
// The tray quick panel and the standalone preview window reuse the same elements (see panel.css).

const inboxListMode = () => !PANEL && !PREVIEW;
const blockingDialog = () => document.querySelector('dialog[open]');

function streamTitle(it) {
  if (it.title) return it.title;
  if (it.kind === 'file') return it.name;
  return (it.text || '').split('\n').find(l => l.trim()) || (it.kind === 'todo' ? '清单' : '文本');
}

/* ================= Cards ================= */

// Detected language of a text item, cached per item version (detection runs on every render otherwise)
const CARD_LANG = new Map();
function cardLang(it) {
  if (it.kind !== 'text') return null;
  const k = it.id + ':' + (it.rev || 0);
  if (!CARD_LANG.has(k)) {
    if (CARD_LANG.size > 600) CARD_LANG.clear();
    CARD_LANG.set(k, it.text.split('\n').length >= 3 ? HL.detect(it.text.slice(0, 4000)) : null);
  }
  return CARD_LANG.get(k);
}

function cardText(it) {
  const lang = cardLang(it);
  const code = findCode(it.text);
  const url = (it.text.match(URL_RE) || [])[0];
  const dh = deepHit(it), terms = S.terms || [];
  // Show the context around the hit only when it was found in the full text (beyond the excerpt)
  const deep = dh && !terms.every(t => haystack(it).includes(t))
    ? `<div class="deep-hit" data-act="detail"><span>在全文第 ${dh.line} 行找到：</span><span class="deep-snip" data-raw>${esc(dh.snippet)}</span></div>` : '';
  const smart = code || url ? `<div class="smart">${code ? `<button class="btn sm" data-act="code" data-v="${esc(code)}">${ICON.key}复制验证码 ${esc(code)}</button>` : ''}${url ? `<a class="btn sm" href="${esc(url)}" target="_blank" rel="noopener noreferrer">${ICON.link}打开链接</a>` : ''}</div>` : '';
  return `${lang ? `<div class="s-head"><span class="s-lang mono">${esc(HL.name(lang))}</span></div>` : ''}<div class="text s-text${lang ? ' mono' : ''}" data-raw>${linkify(it.text.slice(0, 3000))}</div>${deep}${smart}`;
}

function cardFile(it) {
  const mime = it.mime || '';
  // The list only loads thumbnails; formats that cannot be thumbnailed show the original only below 2 MB
  const src = /^image\/(png|jpe?g|gif)$/i.test(mime) ? '/api/thumb/' + it.id
    : /^image\/(webp|avif|bmp)$/i.test(mime) && it.size < 2 << 20 ? '/api/blob/' + it.id : '';
  const media = src
    ? `<img class="thumb" alt="${esc(it.name)}" data-src="${src}" data-act="view" onerror="this.remove()">`
    // Videos on phones must be downloaded and decrypted in full, so the list only holds a button
    : /^video\//.test(mime) ? (S.key && !plainOK(it) ? `<button class="thumb video-ph" data-act="view" aria-label="播放">${ICON.play}</button>`
      : `<video class="thumb" controls preload="metadata" data-src="/api/blob/${it.id}"></video>`) : '';
  return `${media}<div class="file"><div class="file-icon">${ICON.file}</div><div><div class="file-name" data-raw>${esc(it.name)}</div><div class="muted small mono">${fmtSize(it.size || 0)}</div></div></div>`;
}

const CARD_TODO_ROWS = 6;
function cardTodo(it) {
  const es = todoLive(it), done = es.filter(e => e.done);
  const rows = [...es.filter(e => !e.done), ...done];
  const row = e => `<li class="todo-e${e.done ? ' done' : ''}" data-eid="${e.id}"><label><input type="checkbox" data-act="todo-check"${e.done ? ' checked' : ''}><span class="todo-t" data-raw>${linkify(e.text)}</span></label></li>`;
  return `<div class="todo-head"><span class="todo-title" data-raw>${esc(it.text || '')}</span>${it.text ? '' : '<span class="todo-title muted">清单</span>'}<span class="todo-prog muted small mono">${done.length}/${es.length}</span></div>
    <ul class="todo s-todo-list">${rows.slice(0, CARD_TODO_ROWS).map(row).join('')}</ul>
    ${rows.length > CARD_TODO_ROWS ? `<button class="more" data-act="detail">还有 ${rows.length - CARD_TODO_ROWS} 项</button>` : ''}
    ${isTouch ? '' : '<form class="todo-add"><input placeholder="添加一项…" maxlength="1000" autocomplete="off" enterkeyhint="done" aria-label="添加一项"></form>'}`;
}

function streamItemHTML(it) {
  const picking = S.picking, picked = picking && S.picked.has(it.id);
  const dev = S.devices.find(d => d.name === it.from);
  const body = it.kind === 'file' ? cardFile(it) : it.kind === 'todo' ? cardTodo(it) : cardText(it);
  const fav = it.fav ? `<div class="fav-title">${ICON.starOn}${it.title ? `<span data-raw>${esc(it.title)}</span>` : '<span>已收藏</span>'}</div>` : '';
  const meta = `<div class="s-meta">${kindIcon(dev?.kind)}<span class="s-from" data-raw>${esc(it.from)}</span><span class="s-sep" aria-hidden="true">·</span><span class="s-time">${relTime(it.ts)}</span>${deliveryDot(it.id)}${it.rev ? `<span class="s-tag mono" title="已编辑">v${it.rev + 1}</span>` : ''}${it.comments ? `<span class="s-tag" title="${it.comments} 条评论">${ICON.comment}<span class="mono">${it.comments}</span></span>` : ''}${it.fav ? '' : expiryBadge(it)}</div>`;
  const actions = picking ? '' : `<div class="s-actions"><button class="s-act${it.fav ? ' on' : ''}" data-act="fav" aria-label="收藏" title="收藏">${it.fav ? ICON.starOn : ICON.star}</button><button class="s-act s-detail" data-act="detail" title="详情（也可以双击卡片）">详情</button></div>`;
  return `<article class="s-card s-${it.kind}${picked ? ' picked' : ''}" data-act="${picking ? 'pick' : 'main'}" tabindex="0" aria-label="${esc(streamTitle(it).slice(0, 120))}">
    ${picking ? `<span class="pick-box${picked ? ' on' : ''}" aria-hidden="true">${picked ? ICON.check : ''}</span>` : ''}
    ${fav}${body}${tagsHTML(it)}${meta}${actions}</article>`;
}

// Single click on a card: copy text and checklists, open files in the drawer
function cardMainAction(it, li, e) {
  // Selecting part of the text, or clicking a control inside the card, never copies the whole card
  if (getSelection()?.toString()) return;
  if (e && e.target.closest('a, button, input, label, form, video, select, textarea')) return;
  if (it.kind === 'file') return openPreview(it);
  copyItem(it);
  li.classList.remove('copied-flash');
  void li.offsetWidth; // restart the animation
  li.classList.add('copied-flash');
}

/* ================= Stream state shown around the list ================= */

const KIND_ICONS = () => ({ all: ICON.copy, text: ICON.pencil, file: ICON.file, todo: ICON.check, fav: ICON.star });

function syncStream() {
  if (!inboxListMode()) return;
  document.querySelectorAll('#kindFilters [data-kind]').forEach(b => {
    const active = b.dataset.kind === S.kind;
    b.classList.toggle('active', active);
    b.setAttribute('aria-pressed', String(active));
  });
  document.querySelectorAll('[data-kind-count]').forEach(el => {
    const kind = el.dataset.kindCount;
    el.textContent = S.items.filter(it => kind === 'all' || (kind === 'fav' ? it.fav : it.kind === kind)).length;
  });
  // Active device / tag filters as removable chips (the device beam and the capsule menu set them)
  const chips = [];
  if (S.from) chips.push(`<button class="filter-chip" data-clear="from" title="取消设备筛选"><span data-raw>${esc(S.from)}</span>${ICON.close}</button>`);
  if (S.tag) chips.push(`<button class="filter-chip" data-clear="tag" title="取消标签筛选"><span data-raw>#${esc(S.tag)}</span>${ICON.close}</button>`);
  const box = $('#activeFilters'), html = chips.join('');
  if (box.dataset.html !== html) { box.innerHTML = html; box.dataset.html = html; }
  $('#smTags').hidden = !allTags().length;
  syncPickBar();
  renderBeam();
}

/* ---- Device beam: online devices joined by a flowing dashed line; click one to filter by it ---- */

const BEAM_MAX = 4;
function renderBeam() {
  const box = $('#beamDevices');
  if (!box || !inboxListMode()) return;
  const me = S.devices.find(d => d.id === S.you) || (S.local ? S.devices.find(d => d.host) : null);
  const list = [me, ...S.devices.filter(d => d !== me && d.node), ...S.devices.filter(d => d !== me && !d.node)].filter(Boolean);
  const shown = list.slice(0, BEAM_MAX), extra = list.length - shown.length;
  const line = '<span class="beam-line" aria-hidden="true"><svg viewBox="0 0 40 6" preserveAspectRatio="none"><path d="M0 3H40"/></svg></span>';
  const html = shown.map((d, i) => {
    const on = S.from === d.name;
    const sub = d === me ? '本机' : d.node ? (d.enc ? '已加密' : '未加密（旧版本）') : '浏览器';
    return (i ? line : '') + `<button type="button" class="beam-node${on ? ' on' : ''}${d === me ? ' self' : ''}" data-from="${esc(d.name)}" aria-pressed="${on}" title="${esc(d.name)} · ${sub}（点击只看它发来的内容）"><span class="beam-dot" aria-hidden="true"></span>${kindIcon(d.kind)}<span class="beam-name" data-raw>${esc(d.name)}</span></button>`;
  }).join('') + (extra > 0 ? line + `<button type="button" class="beam-node more" data-beam-more title="查看所有在线设备"><span class="mono">+${extra}</span></button>` : '');
  if (box.dataset.html !== html) { box.innerHTML = html; box.dataset.html = html; }
  box.classList.toggle('paused', !S.connected || (S.local && !!S.info && isPaused()));
  box.classList.toggle('alone', list.length < 2);
}

/* ---- Sync capsule: one status pill that opens the sync, appearance and settings panel ---- */

function renderSyncCapsule() {
  const cap = $('#syncCapsule');
  if (!cap) return;
  const paused = S.local && !!S.info && isPaused();
  const plain = S.devices.some(d => d.node && !d.host && !d.enc);
  const state = !S.connected ? 'off' : paused ? 'paused' : 'on';
  $('#syncLabel').textContent = state === 'off' ? 'OFFLINE' : paused ? 'PAUSED' : plain ? 'SYNC' : 'SYNC · E2E';
  cap.classList.toggle('off', state === 'off');
  cap.classList.toggle('paused', state === 'paused');
  const others = S.devices.filter(d => d.id !== S.you && !(S.local && d.host)).length;
  cap.title = state === 'off' ? '正在重新连接' : paused ? '同步已暂停' : `已连接 · ${others + 1} 台设备${plain ? '' : ' · 端到端加密'}`;
  if (!$('#syncMenu').hidden) renderSyncMenu();
}

function renderSyncMenu() {
  const paused = S.local && !!S.info && isPaused();
  const plain = S.devices.some(d => d.node && !d.host && !d.enc);
  const others = S.devices.filter(d => d.id !== S.you && !(S.local && d.host)).length;
  $('#smStatus').innerHTML = `<span class="dot${S.connected ? ' on' : ''}" aria-hidden="true"></span><b>${!S.connected ? '正在重新连接' : paused ? '同步已暂停' : '已连接'}</b><span class="muted">${others + 1} 台设备${plain ? ' · 有旧版本设备未加密' : ' · 端到端加密'}</span>`;
  // Sync switches and pause only exist on this computer; a phone only gets appearance and its own settings
  $('#smLocal').hidden = !S.local || !S.info;
  if (S.local && S.info) {
    document.querySelectorAll('#smLocal [data-mode]').forEach(c => { c.checked = c.dataset.mode === 'watch' ? isActive() : isAutoPaste(); });
    renderPause();
  }
  const theme = document.documentElement.dataset.theme || 'system';
  document.querySelectorAll('#smTheme [data-theme-set]').forEach(b => {
    const on = b.dataset.themeSet === theme;
    b.classList.toggle('on', on); b.setAttribute('aria-checked', String(on));
  });
  const fs = applyFont(store.get('fontSize')), r = $('#smFont');
  r.min = FS_MIN; r.max = FS_MAX; r.step = FS_STEP;
  if (+r.value !== fs) r.value = fs;
  r.style.setProperty('--p', ((fs - FS_MIN) / (FS_MAX - FS_MIN) * 100) + '%');
  $('#smFontVal').textContent = Math.round(fs * 100) + '%';
  $('#smTags').hidden = !allTags().length;
  $('[data-sm="settings"]').textContent = S.local ? '更多设置…' : '这台设备…';
}

function placeSyncMenu() {
  const m = $('#syncMenu'), r = $('#syncCapsule').getBoundingClientRect(), w = m.offsetWidth;
  m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
  m.style.top = r.bottom + 8 + 'px';
}

function toggleSyncMenu(open) {
  const m = $('#syncMenu'), cap = $('#syncCapsule');
  open = open ?? m.hidden;
  if (open) { renderSyncMenu(); m.hidden = false; placeSyncMenu(); $('button, input', m)?.focus({ preventScroll: true }); }
  else m.hidden = true;
  cap.setAttribute('aria-expanded', String(open));
}

function bindSyncMenu() {
  const m = $('#syncMenu'), cap = $('#syncCapsule');
  cap.onclick = () => toggleSyncMenu();
  m.addEventListener('change', e => {
    const c = e.target.closest('[data-mode]');
    if (c) setMode(c.dataset.mode, c.checked);
  });
  m.addEventListener('input', e => {
    if (e.target.id !== 'smFont') return;
    setFont(e.target.value);
    renderSyncMenu(); placeSyncMenu();
  });
  m.addEventListener('click', e => {
    const p = e.target.closest('[data-pause]');
    if (p) { pause(+p.dataset.pause).then(renderSyncMenu); return; }
    const t = e.target.closest('[data-theme-set]');
    if (t) { applyTheme(t.dataset.themeSet); renderSyncMenu(); return; }
    const link = e.target.closest('[data-sm]');
    if (!link) return;
    toggleSyncMenu(false);
    if (link.dataset.sm === 'devices' || !S.local) { $('#myName').value = myName(); $('#devicesDlg').showModal(); }
    else { loadInfo(); showSettingsCat(store.get('settingsCat') || 'general'); $('#settingsDlg').showModal(); }
  });
  m.addEventListener('keydown', e => { if (e.key === 'Escape') { e.preventDefault(); toggleSyncMenu(false); cap.focus(); } });
  document.addEventListener('mousedown', e => { if (!m.hidden && !m.contains(e.target) && !cap.contains(e.target)) toggleSyncMenu(false); });
  addEventListener('resize', () => { if (!m.hidden) placeSyncMenu(); });
}

/* ================= Layout ================= */

// The tray panel keeps its own arrangement: search and categories at the top, the composer pinned to the bottom
function mountInboxLayout() {
  if (!PANEL) return;
  $('.inbox-search-row').after($('#kindFilters'));
  $('#listComposerHost').append($('#clipCard'), $('#composer'));
}

/* ================= Drawer (the #previewDlg dialog) ================= */

function renderInboxTools() {
  if (PANEL || !PV) return;
  const it = PV;
  $('#readerFavorite').innerHTML = it.fav ? ICON.starOn : ICON.star;
  $('#readerFavorite').classList.toggle('on', !!it.fav);
  $('#readerFavorite').setAttribute('aria-pressed', String(!!it.fav));
  // A favorite's name is the only title that isn't already the first line of the body
  $('#readerTitle').textContent = (it.title || '').slice(0, 180);
  $('#readerTitle').hidden = !it.title;
  const dlv = $('#pvDelivery'), badge = PVS.mode === 'view' ? deliveryBadge(it.id) : '';
  const show = badge && /failed|pending/.test(badge);
  dlv.hidden = !show;
  if (show && dlv.dataset.html !== badge) { dlv.innerHTML = badge; dlv.dataset.html = badge; }
  renderReaderTabs();
}

function openInboxMore(anchor) {
  if (!PV) return;
  const it = PV;
  openMenu(anchor, '更多操作', [
    { label: '标签', run: () => openTagMenu(it, anchor) },
    { label: '保留时长', run: () => openExpiryMenu(it, anchor) },
    ...(it.fav ? [{ label: '重命名片段', run: () => renameFav(it) }] : []),
    ...(it.kind === 'todo' ? [{ label: '重命名清单', run: () => uiPrompt('清单名称', { value: it.text || '', ok: '保存' }).then(v => { if (v !== null && v.trim() !== it.text) todoChange(it, { title: v }); }) }] : []),
    ...(it.kind === 'todo' && todoLive(it).some(e => e.done) ? [{ label: '清除已完成', run: () => todoChange(it, { clear: true }) }] : []),
    ...(it.kind === 'file' && S.local ? [{ label: '在文件夹中显示', run: () => saveLocal(it, 'reveal') }] : []),
    { label: '删除', run: () => api('DELETE', '/api/items/' + it.id).catch(e => toast('删除失败：' + e.message)) },
  ]);
}

/* ---- Drawer tabs: content / history / comments ---- */

function readerTab() {
  if (PVS.mode === 'history') return 'history';
  return PVS.com ? 'comments' : 'content';
}

function renderReaderTabs() {
  const tabs = $('#pvTabs'), d = $('#previewDlg');
  const on = !PANEL && !!PV && PVS.mode !== 'edit';
  tabs.hidden = !on;
  d.classList.toggle('tabbed', on);
  if (!on) { d.classList.remove('tab-comments'); return; }
  const it = PV, cur = readerTab();
  const defs = [['content', '内容']];
  if (it.kind === 'text') defs.push(['history', it.rev ? `历史 ${it.rev + 1}` : '历史']);
  defs.push(['comments', it.comments ? `评论 ${it.comments}` : '评论']);
  const html = defs.map(([k, label]) => `<button role="tab" class="pv-tab${k === cur ? ' on' : ''}" data-tab="${k}" aria-selected="${k === cur}">${esc(label)}</button>`).join('');
  if (tabs.dataset.html !== html) { tabs.innerHTML = html; tabs.dataset.html = html; }
  d.classList.toggle('tab-comments', cur === 'comments');
}

function pickReaderTab(tab) {
  if (!PV || tab === readerTab()) return;
  if (PVS.mode === 'history') closeHistory();
  PVS.com = tab === 'comments';
  if (tab === 'history') { PVS.com = false; openHistory(); }
  renderComPanel();
  renderReaderTabs();
  if (tab === 'comments') $('#pvComInput').focus();
}

/* ================= Batch delete and clear =================
   "多选" turns a click on a card into ticking it; "全选" and "批量删除" only apply to the items
   currently filtered in, so ticked items that are filtered out are never deleted by accident. */

S.picking = false;
S.picked = new Set();
const BATCH = 5000; // same as the Go side's maxBatchDelete

const pickedInView = () => (S.view || []).filter(it => S.picked.has(it.id));

function setPicking(on) {
  S.picking = on;
  S.picked.clear();
  document.body.classList.toggle('picking', on);
  $('#selectBar').hidden = !on;
  $('#selectBtn').classList.toggle('on', on);
  $('#selectBtn').setAttribute('aria-pressed', String(on));
  renderList();
  if (on) $('#selectAll').focus({ preventScroll: true });
}

function togglePick(id) {
  S.picked.has(id) ? S.picked.delete(id) : S.picked.add(id);
  renderList();
}

// Called after every list repaint: drop entries that no longer exist and refresh the count and the buttons
function syncPickBar() {
  if (!S.picking) return;
  const live = new Set(S.items.map(it => it.id));
  for (const id of S.picked) if (!live.has(id)) S.picked.delete(id);
  const n = pickedInView().length, all = (S.view || []).length;
  $('#selectCount').textContent = `已选 ${n} 条`;
  $('#selectAll').textContent = all && n === all ? '取消全选' : '全选';
  $('#selectAll').disabled = !all;
  $('#selectDelete').disabled = !n;
}

async function deleteIDs(ids) {
  let n = 0;
  for (let i = 0; i < ids.length; i += BATCH) n += (await api('POST', '/api/items/delete', { ids: ids.slice(i, i + BATCH) })).count;
  return n;
}

async function deletePicked() {
  const ids = pickedInView().map(it => it.id);
  if (!ids.length) return;
  if (!(await uiConfirm(`删除选中的 ${ids.length} 条内容？会从所有设备上删除，无法撤销。`, { ok: `删除 ${ids.length} 条`, danger: true }))) return;
  try {
    const n = await deleteIDs(ids);
    setPicking(false);
    toast(`已在所有设备上删除 ${n} 条`);
  } catch (e) { toast('删除失败：' + e.message); }
}

// Clear: keeps favorites and lists by default (deletes the rest one by one); without it everything is cleared (all devices clear by timestamp)
async function askClearAll() {
  const keepable = it => it.fav || it.kind === 'todo';
  const text = keep => {
    const n = keep ? S.items.filter(it => !keepable(it)).length : S.items.length;
    return { n, message: `将从所有设备上删除 ${n} 条内容和对应的文件，无法撤销。`, ok: `清空 ${n} 条` };
  };
  const first = text(true);
  const r = await uiConfirm(first.message, { ok: first.ok, danger: true, check: '保留收藏和清单', checked: true, onCheck: text });
  if (!r) return false;
  const { n } = text(r.checked);
  if (!n) { toast('没有可清空的内容'); return true; }
  try {
    if (r.checked) await deleteIDs(S.items.filter(it => !keepable(it)).map(it => it.id));
    else await api('POST', '/api/clear');
    if (S.picking) setPicking(false);
    toast(`已在所有设备上清空 ${n} 条`);
  } catch (e) { toast('清空失败：' + e.message); }
  return true;
}

function bindPicking() {
  $('#selectBtn').onclick = () => setPicking(!S.picking);
  $('#selectDone').onclick = () => setPicking(false);
  $('#selectAll').onclick = () => {
    const view = S.view || [];
    const all = view.length && view.every(it => S.picked.has(it.id));
    view.forEach(it => (all ? S.picked.delete(it.id) : S.picked.add(it.id)));
    // Show the pages that are not loaded yet when selecting all, so items that are off screen are not deleted unnoticed
    if (!all && view.length > S.limit) S.limit = view.length;
    renderList();
  };
  $('#selectDelete').onclick = deletePicked;
  $('#clearBtn').onclick = askClearAll;
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape' && S.picking && !blockingDialog()) { e.preventDefault(); setPicking(false); }
  });
}

function setFromFilter(name) {
  S.from = S.from === name ? '' : name;
  const sel = $('#fromFilter');
  if (sel) sel.value = [...sel.options].some(o => o.value === S.from) ? S.from : '';
  S.sel = 0;
  renderList();
}

/* ================= Omni-Bar: type to filter, Enter to send ================= */

// Several lines that read like a checklist (- / * / - [ ] / 1.) become a checklist instead of a text
function looksLikeList(text) {
  const lines = text.split('\n').map(s => s.trim()).filter(Boolean);
  if (lines.length < 2) return false;
  const marked = lines.filter(l => /^([-*•+]\s|\d{1,3}\.\s|\[.?\]\s?|[☐☑✅]\s?)/.test(l)).length;
  return marked >= 2 && marked >= lines.length - 1;
}

async function omniSubmit() {
  const ta = $('#input');
  if (!ta.value.trim()) return;
  if (looksLikeList(ta.value)) await newTodo();
  else await sendText();
  if (!ta.value) { S.filter = ''; renderList(); }
}

function omniFilter() {
  if (!inboxListMode()) return;
  S.filter = $('#input').value;
  renderList();
  deepSearch();
}

function syncOmniPlaceholder() {
  const ta = $('#input');
  if (!ta || !inboxListMode()) return;
  const narrow = matchMedia('(max-width: 650px)').matches;
  ta.placeholder = narrow
    ? (isTouch ? '搜索，或粘贴发送…' : '搜索，或输入发送…')
    : (isTouch ? '搜索，或长按粘贴后发送…' : '输入即搜，回车即发，或 ⌘V 粘贴 / 拖入文件…');
}

function bindOmni() {
  const ta = $('#input');
  ta.addEventListener('input', omniFilter);
  ta.addEventListener('keydown', e => {
    if (e.key === 'Escape' && ta.value) { e.preventDefault(); e.stopPropagation(); ta.value = ''; autosize(); updateSend(); omniFilter(); }
  });
  $('#sendHint').textContent = '';
  syncOmniPlaceholder();
  matchMedia('(max-width: 650px)').addEventListener('change', syncOmniPlaceholder);
}

/* ================= Settings sections ================= */

const SETTING_CATS = [['general', '通用'], ['sync', '同步'], ['storage', '存储'], ['devices', '设备与配对'], ['security', '安全'], ['automation', '自动化'], ['about', '关于与更新']];

function showSettingsCat(cat) {
  const secs = [...document.querySelectorAll('#settingsDlg [data-cat]')];
  // Sections hidden by other code (for example no updates in the command line version) do not count; a category whose sections are all hidden is not shown
  const avail = SETTING_CATS.filter(([k]) => secs.some(s => s.dataset.cat === k && !s.hidden));
  if (!avail.some(([k]) => k === cat)) cat = avail[0]?.[0] || 'general';
  store.set('settingsCat', cat);
  secs.forEach(s => s.classList.toggle('cat-off', s.dataset.cat !== cat));
  $('#settingsNav').innerHTML = avail.map(([k, label]) => `<button type="button" data-cat-btn="${k}" class="${k === cat ? 'on' : ''}" aria-current="${k === cat ? 'true' : 'false'}">${esc(label)}</button>`).join('');
}

function bindSettingsNav() {
  $('#settingsNav').addEventListener('click', e => {
    const b = e.target.closest('[data-cat-btn]');
    if (!b) return;
    showSettingsCat(b.dataset.catBtn);
    $('#settingsDlg').scrollTop = 0;
    $(`#settingsNav [data-cat-btn="${b.dataset.catBtn}"]`)?.focus({ preventScroll: true });
  });
  showSettingsCat(store.get('settingsCat') || 'general');
}

/* ================= Binding ================= */

// Drawer controls: shared by the main window's drawer and the standalone preview window
function bindDrawer() {
  $('#readerFavorite').innerHTML = ICON.star;
  $('#readerMore').innerHTML = ICON.more;
  $('#readerFavorite').onclick = e => PV && toggleFav(PV, e.currentTarget);
  $('#readerMore').onclick = e => openInboxMore(e.currentTarget);
  $('#pvTabs').addEventListener('click', e => { const b = e.target.closest('[data-tab]'); if (b) pickReaderTab(b.dataset.tab); });
}

function bindInbox() {
  if (!PANEL) bindDrawer();
  if (!inboxListMode()) { $('#selectBtn').hidden = true; mountInboxLayout(); return; }
  document.body.classList.add('inbox', 'stream-mode');
  document.documentElement.classList.add('inbox-root');
  bindPicking();
  bindSyncMenu();
  bindOmni();
  const icons = KIND_ICONS();
  document.querySelectorAll('#kindFilters [data-kind]').forEach(b => { $('.nav-symbol', b).innerHTML = icons[b.dataset.kind]; });
  $('#pairingBtn').innerHTML = '<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 6v12M6 12h12"/></svg>';
  $('#pairingBtn').onclick = () => { loadInfo(); $('#pairingDlg').showModal(); };
  $('#beamDevices').addEventListener('click', e => {
    if (e.target.closest('[data-beam-more]')) { $('#myName').value = myName(); $('#devicesDlg').showModal(); return; }
    const b = e.target.closest('[data-from]');
    if (b) setFromFilter(b.dataset.from);
  });
  $('#activeFilters').addEventListener('click', e => {
    const b = e.target.closest('[data-clear]');
    if (!b) return;
    if (b.dataset.clear === 'from') setFromFilter(S.from);
    else setTagFilter(S.tag);
  });
  // Keyboard: ↑↓ move between cards, Enter runs the main action, Space opens the drawer
  $('#list').addEventListener('keydown', e => {
    const card = e.target.closest?.('.s-card');
    if (!card || e.target !== card) return;
    const li = card.closest('.item'), it = S.items.find(x => x.id === li?.dataset.id);
    if (!it) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const sib = e.key === 'ArrowDown' ? li.nextElementSibling : li.previousElementSibling;
      $('.s-card', sib || li)?.focus();
    } else if (e.key === 'Enter') { e.preventDefault(); S.picking ? togglePick(it.id) : cardMainAction(it, li); }
    else if (e.key === ' ') { e.preventDefault(); openPreview(it); }
  });
}
