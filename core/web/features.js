'use strict';

// 发送模式、页内对话框、收藏、单条保留时长、文件占用与手动添加设备的渲染。

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
      delete box.dataset.raw; // 提示文字需要翻译
      box.textContent = c.reason;
      box.classList.add('empty');
    } else {
      box.dataset.raw = ''; // 剪贴板内容原样显示
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
