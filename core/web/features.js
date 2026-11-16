'use strict';

// Rendering of send mode, in-page dialogs, favorites, per-item retention, storage usage and manually added devices.

/* ================= Active Copy / Active Paste ================= */

// Active copy: send automatically after copying on this machine (settings.watch); active paste: received text is written to the local clipboard automatically (settings.autoApply). The two are independent
const isActive = () => S.info?.settings?.watch !== false;
const isAutoPaste = () => !!S.info?.settings?.autoApply;

const pausedUntil = () => S.info?.settings?.pausedUntil || 0;
const isPaused = () => { const p = pausedUntil(); return p === -1 || p > Date.now() + S.offset; };
const hhmm = ms => new Date(ms).toTimeString().slice(0, 5);

function renderMode() {
  if (!S.local || !S.info) return;
  const active = isActive(), paste = isAutoPaste();
  renderPause();
  $('#optWatch').checked = active;
  $('#optApply').checked = paste;
  document.querySelectorAll('#modeMenu [data-mode]').forEach(c => { c.checked = c.dataset.mode === 'watch' ? active : paste; });
  const b = $('#modeBtn');
  b.hidden = PREVIEW;
  b.classList.toggle('passive', !active && !paste);
  b.classList.toggle('paused', isPaused());
  if (isPaused()) {
    const p = pausedUntil();
    b.innerHTML = `<span class="mode-dot"></span><span>已暂停</span>${p > 0 ? ` <span>· 到 ${hhmm(p)}</span>` : ''}`;
    b.title = '同步已暂停，点击恢复';
  } else {
    b.innerHTML = `<span class="mode-dot"></span>${active && paste ? '主动复制 · 粘贴' : active ? '主动复制' : paste ? '主动粘贴' : '手动'}`;
    b.title = `主动复制：${active ? '开' : '关'} · 主动粘贴：${paste ? '开' : '关'}。点击修改`;
  }
  $('#clipCard').hidden = active || PREVIEW;
  $('#clipHint').textContent = '也可以在托盘菜单里选“发送当前剪贴板”';
  if (!active) refreshClip();
}

/* ---- Pause Sync ---- */

function renderPause() {
  const row = $('#pauseRow');
  if (!row) return;
  if (isPaused()) {
    const p = pausedUntil();
    row.innerHTML = `<span class="paused-text">${p > 0 ? `已暂停到 ${hhmm(p)}` : '已暂停，直到手动恢复'}</span><button class="btn sm primary" data-pause="0">恢复同步</button>`;
  } else {
    row.innerHTML = [[15, '15 分钟'], [60, '1 小时'], [-1, '直到手动恢复']]
      .map(([m, l]) => `<button class="btn sm" data-pause="${m}">${l}</button>`).join('');
  }
}

async function pause(minutes) {
  try {
    S.info = await api('POST', '/api/pause', { minutes });
    renderMode();
    toast(minutes === 0 ? '已恢复同步' : '已暂停同步');
  } catch (e) { toast(e.message); }
}

/* ---- App Blocklist ---- */

function renderBlock() {
  const list = S.info?.settings?.blockApps || [];
  $('#blockList').innerHTML = list.map(a =>
    `<span class="chip-item"><span data-raw>${esc(a)}</span><button class="icon-btn" data-unblock="${esc(a)}" aria-label="移除">${ICON.close}</button></span>`).join('')
    || '<span class="muted small">还没有添加</span>';
  const lower = new Set(list.map(a => a.toLowerCase()));
  const recent = (S.info?.recentApps || []).filter(a => !lower.has((a.name || '').toLowerCase()) && !lower.has((a.id || '').toLowerCase()));
  $('#recentApps').innerHTML = recent.length
    ? '<span class="muted small">最近复制来源：</span>' + recent.slice(0, 8).map(a =>
      `<button class="chip-item add" data-block="${esc(a.name || a.id)}" title="${esc(a.id)}">＋ <span data-raw>${esc(a.name || a.id)}</span></button>`).join('')
    : '';
}

async function setBlockList(list) {
  await saveSettings({ blockApps: list });
  renderBlock();
}

async function setMode(key, on) {
  await saveSettings({ [key]: on });
  renderMode();
  toast(key === 'watch'
    ? (on ? '已开启主动复制：复制后自动发送' : '已关闭主动复制：需要手动发送')
    : (on ? '已开启主动粘贴：收到的文字自动放进剪贴板' : '已关闭主动粘贴：收到的内容只出现在列表里'));
}

function bindModeMenu() {
  const m = $('#modeMenu'), btn = $('#modeBtn');
  btn.onclick = () => {
    if (isPaused()) return pause(0); // clicking while paused resumes sync
    if (!m.hidden) { m.hidden = true; return; }
    renderMode();
    m.hidden = false;
    const r = btn.getBoundingClientRect(), w = m.offsetWidth;
    m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
    m.style.top = r.bottom + 6 + 'px';
    $('input', m).focus();
  };
  m.addEventListener('change', e => { const c = e.target.closest('[data-mode]'); if (c) setMode(c.dataset.mode, c.checked); });
  m.addEventListener('keydown', e => { if (e.key === 'Escape') { m.hidden = true; btn.focus(); } });
  document.addEventListener('mousedown', e => { if (!m.hidden && !m.contains(e.target) && !btn.contains(e.target)) m.hidden = true; });
}

let clipBusy = false;
async function refreshClip() {
  if (!S.local || !S.info || isActive() || PREVIEW || clipBusy) return;
  clipBusy = true;
  try {
    const c = await api('GET', '/api/clipboard');
    const box = $('#clipText');
    if (c.image) {
      delete box.dataset.raw;
      box.textContent = `剪贴板里是一张图片（${fmtSize(c.image)}）`;
      box.classList.remove('empty');
    } else if (c.reason) {
      delete box.dataset.raw; // the hint text needs translation
      box.textContent = c.reason;
      box.classList.add('empty');
    } else {
      box.dataset.raw = ''; // clipboard content is shown verbatim
      box.textContent = c.text + (c.length > [...c.text].length ? '…' : '');
      box.classList.remove('empty');
    }
    $('#clipInfo').textContent = c.reason ? '' : c.sent ? '已发送' : c.image ? '' : `${c.length} 字`;
    $('#clipSend').disabled = !!c.reason || c.sent;
    $('#clipSend').textContent = c.sent ? '已发送' : '发送剪贴板';
  } catch {} finally { clipBusy = false; }
}

async function sendClipboard() {
  try {
    await api('POST', '/api/send-clipboard');
    toast('已保存到本机，可查看接收结果');
  } catch (e) { toast(e.message); }
  refreshClip();
}

/* ================= In-Page Dialogs (no dependency on the browser / WebView confirm, prompt) ================= */

// check: adds a checkbox below the message (e.g. “keep favorites and list”); OK returns { checked }, cancel returns false
function uiAsk(message, { input = false, value = '', placeholder = '', ok = '确定', cancel = '取消', danger = false, check = '', checked = false, onCheck = null } = {}) {
  return new Promise(resolve => {
    const d = $('#askDlg');
    $('#askMsg').textContent = message;
    const inp = $('#askInput');
    inp.hidden = !input; inp.value = value; inp.placeholder = placeholder;
    const box = $('#askCheck');
    $('#askCheckRow').hidden = !check;
    $('#askCheckText').textContent = check;
    box.checked = checked;
    // the message and the buttons can be rewritten when the checkbox changes (e.g. the count)
    box.onchange = () => { const r = onCheck?.(box.checked); if (r) { $('#askMsg').textContent = r.message; $('#askOk').textContent = r.ok; } };
    $('#askOk').textContent = ok; $('#askCancel').textContent = cancel;
    $('#askOk').classList.toggle('danger', danger);
    const done = v => { d.close(); resolve(v); };
    $('#askOk').onclick = () => done(input ? inp.value : check ? { checked: box.checked } : true);
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

/* ================= Favorites / Snippets ================= */

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

// generic small menu (shares the styling of the retention menu)
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

/* ================= Per-Item Retention ================= */

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

/* ================= Tag Editing ================= */

let tagItem = null, tagAnchor = null;
function openTagMenu(it, anchor) {
  tagItem = it; tagAnchor = anchor;
  const m = $('#tagMenu');
  m.innerHTML = `<div class="pop-title">标签 <span class="muted">· 同步到所有设备</span></div>
    <div class="tag-cur"></div>
    <form class="tag-form"><input id="tagInput" placeholder="输入标签，回车添加" maxlength="30" autocomplete="off" enterkeyhint="done" aria-label="新标签"></form>
    <div class="tag-sugg"></div>`;
  m.hidden = false;
  renderTagMenu();
  placeTagMenu();
  $('#tagInput').focus();
}
function placeTagMenu() {
  const m = $('#tagMenu');
  if (m.hidden) return;
  // the card may be rebuilt when the tags change, so the button has to be looked up again
  if (!tagAnchor?.isConnected) tagAnchor = $(`.item[data-id="${tagItem?.id}"] [data-act="tags"]`);
  if (!tagAnchor) return;
  const r = tagAnchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
  m.style.top = (r.bottom + h + 8 > innerHeight ? Math.max(8, r.top - h - 6) : r.bottom + 6) + 'px';
}
function renderTagMenu() {
  const m = $('#tagMenu');
  if (m.hidden || !tagItem) return;
  const it = S.items.find(x => x.id === tagItem.id) || tagItem;
  const cur = liveTags(it), q = ($('#tagInput')?.value || '').trim().replace(/^[#＃]/, '').toLowerCase();
  $('.tag-cur', m).innerHTML = cur.length ? cur.map(t => `<span class="tag on"><span data-raw>#${esc(t)}</span><button data-rm="${esc(t)}" aria-label="移除标签" title="移除标签">${ICON.close}</button></span>`).join('')
    : '<span class="muted small">还没有标签</span>';
  const sugg = allTags().map(([t]) => t).filter(t => !cur.includes(t) && (!q || t.toLowerCase().includes(q))).slice(0, 12);
  $('.tag-sugg', m).innerHTML = sugg.map(t => `<button class="tag" data-add="${esc(t)}"><span data-raw>#${esc(t)}</span></button>`).join('');
}
async function changeTags(add, remove) {
  if (!tagItem) return;
  try { upsertItem(await api('POST', `/api/items/${tagItem.id}/tags`, { add, remove })); }
  catch (e) { toast('设置失败：' + e.message); }
  renderTagMenu();
  placeTagMenu();
}
function bindTagMenu() {
  const m = $('#tagMenu');
  m.addEventListener('submit', e => {
    e.preventDefault();
    const inp = $('#tagInput'), v = inp.value.trim();
    if (!v) return;
    inp.value = '';
    changeTags(v.split(/[,，]/), []);
  });
  m.addEventListener('input', renderTagMenu);
  m.addEventListener('click', e => {
    const rm = e.target.closest('[data-rm]'), add = e.target.closest('[data-add]');
    if (rm) changeTags([], [rm.dataset.rm]);
    else if (add) changeTags([add.dataset.add], []);
  });
  m.addEventListener('keydown', e => { if (e.key === 'Escape') { e.preventDefault(); m.hidden = true; } });
  document.addEventListener('mousedown', e => { if (!m.hidden && !m.contains(e.target) && !e.target.closest('[data-act="tags"]')) m.hidden = true; });
}

function openFontMenu(anchor) {
  const m = $('#fontMenu'), cur = applyFont(store.get('fontSize'));
  m.innerHTML = `<div class="pop-title">字体大小 <span class="muted">· 仅本设备</span></div>
    <div class="fs-row"><span class="fs-a" aria-hidden="true">A</span>
      <input id="fsRange" type="range" min="${FS_MIN}" max="${FS_MAX}" step="${FS_STEP}" value="${cur}" aria-label="字体大小">
      <span class="fs-a big" aria-hidden="true">A</span></div>
    <div class="fs-foot"><span id="fsVal" class="fs-val"></span><button id="fsReset" class="btn ghost sm">恢复默认</button></div>
    ${DESKTOP ? `<div class="pop-foot">快捷键：<kbd>${KIND === 'mac' ? '⌘' : 'Ctrl'}</kbd> + <kbd>+</kbd> / <kbd>-</kbd> / <kbd>0</kbd></div>` : ''}`;
  showFont(cur);
  m.hidden = false;
  const r = anchor.getBoundingClientRect(), w = m.offsetWidth;
  m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px';
  m.style.top = r.bottom + 6 + 'px';
  $('#fsRange').focus();
}

function showFont(v) {
  const val = $('#fsVal'), range = $('#fsRange');
  if (val) val.textContent = Math.round(v * 100) + '%';
  if (range && +range.value !== v) range.value = v;
  if (range) range.style.setProperty('--p', ((v - FS_MIN) / (FS_MAX - FS_MIN) * 100) + '%');
}

function bindFontMenu() {
  const m = $('#fontMenu'), btn = $('#fontBtn');
  btn.innerHTML = ICON.fontSize;
  btn.onclick = () => (m.hidden ? openFontMenu(btn) : (m.hidden = true));
  // applies live while dragging (the font size changes, so the menu and button positions change too and are recomputed every time)
  m.addEventListener('input', e => { if (e.target.id === 'fsRange') { showFont(setFont(e.target.value)); place(); } });
  m.addEventListener('click', e => { if (e.target.closest('#fsReset')) { showFont(setFont(1)); place(); } });
  const place = () => { const r = btn.getBoundingClientRect(), w = m.offsetWidth; m.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + 'px'; m.style.top = r.bottom + 6 + 'px'; };
  m.addEventListener('keydown', e => { if (e.key === 'Escape') { m.hidden = true; btn.focus(); } });
  document.addEventListener('mousedown', e => { if (!m.hidden && !m.contains(e.target) && !btn.contains(e.target)) m.hidden = true; });
  // Desktop: ⌘/Ctrl + plus, minus, 0 to change the font size (in the browser we keep the browser's own zoom)
  if (DESKTOP) addEventListener('keydown', e => {
    if (!(e.metaKey || e.ctrlKey) || e.altKey) return;
    const cur = applyFont(store.get('fontSize'));
    let v;
    if (e.key === '=' || e.key === '+') v = cur + 0.1;
    else if (e.key === '-' || e.key === '_') v = cur - 0.1;
    else if (e.key === '0') v = 1;
    else return;
    e.preventDefault();
    v = setFont(v);
    showFont(v);
    if (!m.hidden) place();
    toast(`字体大小 ${Math.round(v * 100)}%`);
  });
  // several desktop windows share the setting: change it in one window and the others follow
  addEventListener('storage', e => { if (e.key === 'lanvia.fontSize') showFont(applyFont(e.newValue)); });
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
  $('#stText').textContent = quota
    ? `已用 ${fmtSize(total)} / ${fmtSize(quota)} · 原件 ${fmtSize(u.own)} · 副本 ${fmtSize(u.replicas)}（${u.count} 个）`
    : `已用 ${fmtSize(total)} · 原件 ${fmtSize(u.own)} · 副本 ${fmtSize(u.replicas)}（${u.count} 个）`;
  $('#clearReplicas').disabled = !u.count;
}

function renderManual(list) {
  $('#manualList').innerHTML = list.map(m => `<li>
    <span class="dot${m.connected ? ' on' : ''}"></span>
    <span class="mono">${esc(m.addr)}</span>
    <span class="muted small">${m.connected ? `<span>已连接</span> · <span data-raw>${esc(m.name)}</span>` : '未连接，会自动重试'}</span>
    <button class="btn ghost sm" data-remove="${esc(m.addr)}">移除</button>
  </li>`).join('');
}
