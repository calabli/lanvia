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
  return `<button class="inbox-entry" data-act="select" aria-pressed="${selected}" aria-label="${esc(title.slice(0, 120))}">
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
    if (PANEL) $('.inbox-list-column').prepend($('#listComposerHost'));
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
    { label: '评论', run: () => { PVS.com = true; renderComPanel(); $('#pvComInput').focus(); } },
    { label: '保留时长', run: () => openExpiryMenu(it, anchor) },
    { label: '删除', run: () => api('DELETE', '/api/items/' + it.id).catch(e => toast('删除失败：' + e.message)) },
  ]);
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
  if (!inboxListMode()) { mountInboxLayout(); return; }
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
