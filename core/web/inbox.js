'use strict';

// Main window: navigation, inbox and a persistent reading pane. The tray and
// standalone preview keep their existing layouts and interactions.
const inboxListMode = () => !PANEL && !PREVIEW;
const inboxWide = () => inboxListMode() && (matchMedia('(min-width: 1000px)').matches || document.body.classList.contains('list-collapsed'));
const blockingDialog = () => document.querySelector('dialog[open]:not(.inline-preview)');
const inboxKinds = { all: '所有内容', text: '文本与链接', file: '文件', todo: '清单', fav: '收藏' };
let inboxSelection = null;
let inboxOpening = null;
let inboxLoad = 0;
let inboxFailed = null;

function inboxTitle(it) {
  if (it.title) return it.title;
  if (it.kind === 'file') return it.name;
  return (it.text || '').split('\n').find(l => l.trim()) || (it.kind === 'todo' ? '清单' : '文本与链接');
}

function inboxItemHTML(it) {
  const selected = inboxSelection === it.id;
  const title = inboxTitle(it);
  const snippet = it.kind === 'file' ? `${fmtSize(it.size || 0)} · ${it.mime || '文件'}`
    : it.kind === 'todo' ? todoText(it).split('\n').slice(1).join(' · ')
    : (it.text || '').split('\n').filter(Boolean).slice(1).join(' ') || it.text || '';
  const dev = S.devices.find(d => d.name === it.from);
  const tags = liveTags(it).slice(0, 3);
  const picking = S.picking, picked = picking && S.picked.has(it.id);
  return `<button class="inbox-entry${picking ? ' picking' : ''}" data-act="${picking ? 'pick' : 'select'}" aria-pressed="${picking ? picked : selected}" aria-label="${esc(title.slice(0, 120))}">
    ${picking ? `<span class="pick-box${picked ? ' on' : ''}" aria-hidden="true">${picked ? ICON.check : ''}</span>` : ''}
    <span class="inbox-entry-meta">${kindIcon(dev?.kind)}<span class="inbox-from" data-raw>${esc(it.from)}</span><span class="inbox-time">${relTime(it.ts)}</span></span>
    <span class="inbox-entry-title" data-raw>${esc(title.slice(0, 200))}</span>
    <span class="inbox-entry-snippet" data-raw>${esc(snippet.slice(0, 240))}</span>
    <span class="inbox-entry-badges">${it.kind === 'file' ? `<span>${ICON.file}</span>` : ''}${it.fav ? `<span class="inbox-star">${ICON.starOn}</span>` : ''}${tags.map(t => `<span class="inbox-tag" data-raw>#${esc(t)}</span>`).join('')}${it.comments ? `<span>${ICON.comment}${it.comments}</span>` : ''}</span>
  </button>`;
}

function syncInboxNavigation() {
  if (!inboxListMode()) return;
  $('#inboxTitle').textContent = inboxKinds[S.kind] || inboxKinds.all;
  document.querySelectorAll('#kindFilters [data-kind]').forEach(b => {
    const active = b.dataset.kind === S.kind;
    b.classList.toggle('active', active);
    b.setAttribute('aria-pressed', String(active));
  });
  $('#inboxTagHeading').hidden = !allTags().length;
  document.querySelectorAll('[data-kind-count]').forEach(el => {
    const kind = el.dataset.kindCount;
    el.textContent = S.items.filter(it => kind === 'all' || (kind === 'fav' ? it.fav : it.kind === kind)).length;
  });
}

function syncInboxReader(items) {
  if (!inboxListMode()) return;
  syncInboxNavigation();
  syncPickBar();
  renderSideDevices();
  if (!inboxWide()) return;
  // Filtering must never discard an edit that is currently in progress.
  if (PVS.mode === 'edit' && PV && S.items.some(it => it.id === PV.id)) return;
  if (inboxOpening && items.length) return;
  const selected = items.find(it => it.id === inboxSelection) || items[0];
  if (!selected) {
    inboxLoad++;
    previewRequest++;
    inboxOpening = null;
    inboxSelection = null;
    $('#readerHost').removeAttribute('aria-busy');
    PV = null;
    $('#previewDlg').close();
    $('#readerEmpty').hidden = false;
    return;
  }
  if (inboxSelection !== selected.id) {
    inboxSelection = selected.id;
    // Repaint only selection; the queued render reuses the other inbox rows.
    queueMicrotask(renderList);
  }
  if (PV?.id !== selected.id && inboxOpening !== selected.id && inboxFailed !== selected.id) selectInboxItem(selected, 'view', false);
}

async function selectInboxItem(it, mode = 'view', focus = false) {
  const keepListFocus = !!document.activeElement?.closest('.inbox-entry');
  if (PV && PV.id !== it.id && PVS.mode === 'edit' && !(await cancelEdit())) return;
  const ticket = ++inboxLoad;
  inboxSelection = it.id;
  inboxOpening = it.id;
  inboxFailed = null;
  $('#readerHost').setAttribute('aria-busy', 'true');
  renderList();
  const shown = await showPreview(it, mode, focus);
  if (ticket !== inboxLoad) return;
  inboxOpening = null;
  $('#readerHost').removeAttribute('aria-busy');
  if (!shown) {
    inboxFailed = it.id;
    if (inboxWide()) { PV = null; $('#previewDlg').close(); $('#readerEmpty').hidden = false; }
    return;
  }
  if (inboxWide()) $('#readerEmpty').hidden = !!PV;
  if (keepListFocus && mode === 'view' && !focus) $(`.item[data-id="${it.id}"] .inbox-entry`)?.focus({ preventScroll: true });
}

function mountInboxLayout() {
  const d = $('#previewDlg'), composer = $('#composer'), clip = $('#clipCard');
  if (!inboxListMode()) {
    if (PANEL) $('.inbox-search-row').after($('#kindFilters'));
    $('#listComposerHost').append(clip, composer);
    // Tray panel: the composer is pinned to the bottom and the list scrolls on its own in between (see panel.css)
    if (PANEL) $('.inbox-list-column').append($('#listComposerHost'));
    return;
  }
  const wide = inboxWide(), open = d.open;
  const focused = document.activeElement;
  const host = wide ? $('#readerHost') : document.body;
  if (d.parentElement !== host) {
    if (open) d.close();
    d.classList.toggle('inline-preview', wide);
    host.append(d);
    // A narrow-screen dialog may have been closed; desktop still needs the
    // selected item visible in its persistent reading pane.
    if (PV && (open || wide)) wide ? d.show() : d.showModal();
    if (open && focused?.isConnected && d.contains(focused)) focused.focus({ preventScroll: true });
  }
  (wide ? $('#readerComposerHost') : $('#listComposerHost')).append(clip, composer);
  $('#readerEmpty').hidden = !wide || !!PV;
  if (wide && S.connected) syncInboxReader(S.view);
}

function renderInboxTools() {
  if (!inboxListMode() || !PV) return;
  const it = PV;
  $('#readerFavorite').innerHTML = it.fav ? ICON.starOn : ICON.star;
  $('#readerFavorite').classList.toggle('on', !!it.fav);
  $('#readerFavorite').setAttribute('aria-pressed', String(!!it.fav));
  $('#readerTitle').textContent = inboxTitle(it).slice(0, 180);
  $('#readerTitle').hidden = !inboxWide() || it.kind === 'todo' || (it.kind === 'text' && !it.title && (PVS.mono || !it.text.includes('\n') || inboxTitle(it).length > 180));
  $('#readerEmpty').hidden = true;
  renderReaderTabs();
  if (inboxWide() && PVS.mode === 'view') {
    const mod = KIND === 'mac' ? '⌘' : 'Ctrl+';
    $('#pvFoot').innerHTML = it.kind === 'text' ? `<span><kbd>↩</kbd> 复制全部</span><span><kbd>${mod}E</kbd> 编辑</span><span>选中文字后复制</span>` : '';
  }
}

function openInboxMore(anchor) {
  if (!PV) return;
  const it = PV;
  openMenu(anchor, '内容操作', [
    ...(it.kind === 'todo' ? [{ label: '重命名', run: () => uiPrompt('清单名称', { value: it.text || '', ok: '保存' }).then(v => { if (v !== null && v.trim() !== it.text) todoChange(it, { title: v }); }) }] : []),
    ...(it.kind === 'todo' && todoLive(it).some(e => e.done) ? [{ label: '清除已完成', run: () => todoChange(it, { clear: true }) }] : []),
    { label: '标签', run: () => openTagMenu(it, anchor) },
    { label: '评论', run: () => { if (PVS.mode === 'history') closeHistory(); PVS.com = true; renderComPanel(); renderReaderTabs(); $('#pvComInput').focus(); } },
    { label: '保留时长', run: () => openExpiryMenu(it, anchor) },
    { label: '删除', run: () => api('DELETE', '/api/items/' + it.id).catch(e => toast('删除失败：' + e.message)) },
  ]);
}

/* ---- Batch delete and clear ----
   After "Select", clicking an item ticks it; "Select all" and "Delete" only apply to the items
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
  $('#selectBtn').innerHTML = ICON.check;
  $('#clearBtn').innerHTML = ICON.trash;
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

/* ---- Devices in the sidebar: click to see only what came from that device, click again to clear (in sync with the "All devices" dropdown above the list) ---- */

function renderSideDevices() {
  const box = $('#sideDevicesBox');
  if (!box || !inboxListMode()) return;
  const online = new Set(S.devices.map(d => d.name));
  const count = new Map();
  for (const it of S.items) count.set(it.from, (count.get(it.from) || 0) + 1);
  for (const n of online) if (!count.has(n)) count.set(n, 0);
  // Online devices first, then by number of items
  const names = [...count.keys()].sort((a, b) => online.has(b) - online.has(a) || count.get(b) - count.get(a) || a.localeCompare(b));
  box.hidden = names.length < 2;
  const html = names.map(n => {
    const dev = S.devices.find(d => d.name === n), on = S.from === n;
    return `<button class="side-device${on ? ' on' : ''}" data-from="${esc(n)}" aria-pressed="${on}">
      <span class="side-dot${online.has(n) ? ' online' : ''}" aria-hidden="true"></span>
      <span class="side-dev-name"><span data-raw>${esc(n)}</span><small>${online.has(n) ? (dev?.node ? '在线' : '在线 · 浏览器') : '离线'}</small></span>
      <span class="nav-count">${count.get(n)}</span></button>`;
  }).join('');
  const list = $('#sideDevices');
  if (list.dataset.html !== html) { list.innerHTML = html; list.dataset.html = html; }
}

function setFromFilter(name) {
  S.from = S.from === name ? '' : name;
  const sel = $('#fromFilter');
  if (sel) sel.value = [...sel.options].some(o => o.value === S.from) ? S.from : '';
  S.sel = 0;
  renderList();
}

/* ---- Tabs in the reading pane: content / comments / version history (only in the wide persistent reading pane, replacing the comment and history icon buttons) ---- */

function readerTab() {
  if (PVS.mode === 'history') return 'history';
  return PVS.com ? 'comments' : 'content';
}

function renderReaderTabs() {
  const tabs = $('#pvTabs'), d = $('#previewDlg');
  const on = inboxWide() && !!PV && PVS.mode !== 'edit';
  tabs.hidden = !on;
  d.classList.toggle('tabbed', on);
  if (!on) { d.classList.remove('tab-comments'); return; }
  const it = PV, cur = readerTab();
  const defs = [['content', '内容'], ['comments', it.comments ? `评论 ${it.comments}` : '评论']];
  if (it.kind === 'text') defs.push(['history', it.rev ? `历史版本 ${it.rev + 1}` : '历史版本']);
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

/* ---- Settings sections ---- */

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

function setInboxSidebarCollapsed(collapsed, persist = true) {
  const sidebar = $('#workspaceSidebar'), button = $('#sidebarToggle');
  // Move focus out before hiding the navigation so keyboard users can reopen it.
  if (collapsed && sidebar.contains(document.activeElement)) button.focus({ preventScroll: true });
  sidebar.hidden = collapsed;
  document.body.classList.toggle('sidebar-collapsed', collapsed);
  button.classList.toggle('on', !collapsed);
  button.setAttribute('aria-expanded', String(!collapsed));
  button.setAttribute('aria-label', collapsed ? '展开侧边栏' : '收起侧边栏');
  button.title = collapsed ? '展开侧边栏' : '收起侧边栏';
  $('#sidebarCollapse').setAttribute('aria-expanded', String(!collapsed));
  if (persist) store.set('sidebarCollapsed', collapsed ? '1' : '0');
  refreshInboxWidths();
}

function setInboxListCollapsed(collapsed, persist = true) {
  const panel = $('#inboxListPanel'), button = $('#listToggle');
  const keepToggleFocus = document.activeElement === button || (collapsed && panel.contains(document.activeElement));
  if (collapsed && panel.contains(document.activeElement)) button.focus({ preventScroll: true });
  document.body.classList.toggle('list-collapsed', collapsed);
  panel.hidden = collapsed;
  button.classList.toggle('on', !collapsed);
  button.setAttribute('aria-expanded', String(!collapsed));
  button.setAttribute('aria-label', collapsed ? '展开内容列表' : '收起内容列表');
  button.title = collapsed ? '展开内容列表' : '收起内容列表';
  $('#listCollapse').setAttribute('aria-expanded', String(!collapsed));
  // Restoring the inbox on a phone should reveal the list, while an active
  // editor remains visible in its modal and retains all unsaved text.
  if (!collapsed && !inboxWide() && PVS.mode !== 'edit') $('#previewDlg').close();
  mountInboxLayout();
  if (keepToggleFocus) button.focus({ preventScroll: true });
  if (S.connected) renderList();
  if (persist) store.set('listCollapsed', collapsed ? '1' : '0');
  refreshInboxWidths();
}

function bindInbox() {
  if (!inboxListMode()) { $('#selectBtn').hidden = $('#clearBtn').hidden = true; mountInboxLayout(); return; }
  bindPicking();
  $('#sideDevices').addEventListener('click', e => { const b = e.target.closest('[data-from]'); if (b) setFromFilter(b.dataset.from); });
  $('#pvTabs').addEventListener('click', e => { const b = e.target.closest('[data-tab]'); if (b) pickReaderTab(b.dataset.tab); });
  document.body.classList.add('inbox');
  document.documentElement.classList.add('inbox-root');
  $('#sidebarToggle').innerHTML = ICON.sidebar;
  $('#sidebarToggle').onclick = () => setInboxSidebarCollapsed(!document.body.classList.contains('sidebar-collapsed'));
  $('#sidebarCollapse').innerHTML = ICON.collapsePane;
  $('#sidebarCollapse').onclick = () => setInboxSidebarCollapsed(true);
  $('#listToggle').innerHTML = ICON.listPane;
  $('#listToggle').onclick = () => setInboxListCollapsed(!document.body.classList.contains('list-collapsed'));
  $('#listCollapse').innerHTML = ICON.collapsePane;
  $('#listCollapse').onclick = () => setInboxListCollapsed(true);
  setInboxSidebarCollapsed(store.get('sidebarCollapsed') === '1', false);
  setInboxListCollapsed(store.get('listCollapsed') === '1', false);
  bindInboxWidths();
  const icons = { all: ICON.copy, text: ICON.pencil, file: ICON.file, todo: ICON.check, fav: ICON.star };
  document.querySelectorAll('#kindFilters [data-kind]').forEach(b => { $('.nav-symbol', b).innerHTML = icons[b.dataset.kind]; });
  $('#newShareBtn').innerHTML = ICON.pencil;
  $('#readerFavorite').innerHTML = ICON.star;
  $('#readerMore').innerHTML = ICON.more;
  $('#sidebarSettings').innerHTML = ICON.gear;
  $('#sidebarSettings').onclick = () => $('#settingsBtn').click();
  $('#newShareBtn').onclick = () => $('#input').focus();
  $('#pairingBtn').onclick = () => { loadInfo(); $('#pairingDlg').showModal(); };
  $('#readerFavorite').onclick = e => PV && toggleFav(PV, e.currentTarget);
  $('#readerMore').onclick = e => openInboxMore(e.currentTarget);
  matchMedia('(min-width: 1000px)').addEventListener('change', () => { mountInboxLayout(); renderList(); });
  $('#list').addEventListener('keydown', e => {
    if (!e.target.closest('.inbox-entry') || !['ArrowDown', 'ArrowUp'].includes(e.key)) return;
    e.preventDefault();
    const index = S.view.findIndex(it => it.id === inboxSelection);
    const next = Math.max(0, Math.min(S.view.length - 1, index + (e.key === 'ArrowDown' ? 1 : -1)));
    const it = S.view[next];
    if (!it) return;
    if (next >= S.limit) S.limit = next + PAGE;
    selectInboxItem(it).then(() => $(`.item[data-id="${it.id}"] .inbox-entry`)?.focus());
  });
  mountInboxLayout();
}
