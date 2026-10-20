'use strict';

// 事件绑定、快捷面板、语言与启动。最后加载。

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
  // 选择文件夹：手机浏览器大多不支持，只在电脑上显示
  $('#folderPicker').hidden = isTouch || !('webkitdirectory' in document.createElement('input'));
  $('#folderPicker').onclick = () => $('#folderInput').click();
  $('#folderInput').onchange = e => {
    const files = [...e.target.files];
    e.target.value = '';
    if (!files.length) return;
    const root = (files[0].webkitRelativePath || '').split('/')[0] || '文件夹';
    sendFolder(root, files.map(f => ({ path: f.webkitRelativePath || f.name, file: f })));
  };

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
    // 目录要在 drop 事件里同步取出 entry，之后就拿不到了
    const items = [...(e.dataTransfer?.items || [])].filter(i => i.kind === 'file');
    const entries = items.map(i => i.webkitGetAsEntry?.());
    if (!entries.some(en => en?.isDirectory)) { [...(e.dataTransfer?.files || [])].forEach(uploadFile); return; }
    items.forEach((it, k) => {
      const en = entries[k];
      if (en?.isDirectory) sendFolderEntry(en);
      else { const f = it.getAsFile(); if (f) uploadFile(f); }
    });
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
        copyItem(it, el.classList.contains('btn') ? el : $('[data-act="copy"].btn', li));
        break;
      case 'code': doCopy(el.dataset.v, el); break;
      case 'expand': li.classList.toggle('open'); el.textContent = li.classList.contains('open') ? '收起' : '展开全部'; break;
      case 'download': download(it); break;
      case 'view': case 'preview': openPreview(it); break;
      case 'edit': openPreview(it, 'edit'); break;
      case 'comments': openPreview(it, 'comments'); break;
      case 'expiry': openExpiryMenu(it, el); break;
      case 'fav': toggleFav(it, el); break;
      case 'delete':
        api('DELETE', '/api/items/' + it.id).catch(err => toast('删除失败：' + err.message));
        break;
    }
  });

  $('#search').addEventListener('input', e => { S.filter = e.target.value; renderList(); deepSearch(); });
  // 分页：滚动到列表底部自动加载下一页
  const loadMore = () => { S.limit += PAGE; renderList(); };
  $('#listMoreBtn').onclick = loadMore;
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(es => { if (es.some(e => e.isIntersecting) && !$('#listMore').hidden) loadMore(); }, { rootMargin: '600px' }).observe($('#listMore'));
  }
  // 回到前台时补上隐藏期间跳过的渲染
  document.addEventListener('visibilitychange', () => { if (!document.hidden && S.renderPending) renderList(); });
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
  $('#modeBtn').onclick = () => (isPaused() ? pause(0) : setMode(!S.info?.settings?.watch));
  $('#pauseRow').addEventListener('click', e => { const b = e.target.closest('[data-pause]'); if (b) pause(+b.dataset.pause); });
  $('#optNotify').onchange = e => saveSettings({ notify: e.target.checked });
  $('#blockForm').onsubmit = e => {
    e.preventDefault();
    const v = $('#blockInput').value.trim();
    if (!v) return;
    $('#blockInput').value = '';
    setBlockList([...(S.info?.settings?.blockApps || []), v]);
  };
  $('#blockList').addEventListener('click', e => {
    const b = e.target.closest('[data-unblock]');
    if (b) setBlockList((S.info?.settings?.blockApps || []).filter(a => a !== b.dataset.unblock));
  });
  $('#recentApps').addEventListener('click', e => {
    const b = e.target.closest('[data-block]');
    if (b) setBlockList([...(S.info?.settings?.blockApps || []), b.dataset.block]);
  });
  // 暂停到点后界面自动恢复显示
  setInterval(() => { if (S.local && S.info && pausedUntil() > 0) renderMode(); }, 30e3);
  $('#clipSend').onclick = sendClipboard;
  bindExpiryMenu();
  addEventListener('focus', refreshClip);
  setInterval(() => { if (!document.hidden) refreshClip(); }, 2000);
  $('#optApply').onchange = e => saveSettings({ autoApply: e.target.checked });
  $('#optFetch').onchange = e => saveSettings({ autoFetchMB: +e.target.value });
  $('#optLang').onchange = e => { applyLang(e.target.value); saveSettings({ lang: e.target.value }); };
  $('#myLang').onchange = e => applyLang(e.target.value);
  $('#optStrict').onchange = async e => {
    const old = (S.devices || []).filter(d => d.node && !d.host && !d.enc);
    if (e.target.checked && old.length && !(await uiConfirm(`${old.map(d => d.name).join('、')} 是旧版本，不支持加密。开启后将断开与它们的同步，确定吗？`, { ok: '开启', danger: true }))) {
      e.target.checked = false; return;
    }
    saveSettings({ strictEnc: e.target.checked });
  };
  $('#optToken').onchange = async e => {
    await saveSettings({ tokenOff: !e.target.checked });
    $('#tokenBox').hidden = !e.target.checked;
  };
  $('#resetToken').onclick = async () => {
    if (!(await uiConfirm('重新生成后，旧 token 立即失效，使用它的快捷指令和脚本需要更新。确定吗？', { ok: '重新生成', danger: true }))) return;
    try { await api('POST', '/api/token/reset'); await loadInfo(); toast('已重新生成 token'); }
    catch (e) { toast('操作失败：' + e.message); }
  };
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
    renderList(); // 增量渲染：只有选中状态变化的两张卡片会更新
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
      it.kind === 'text' ? copyItem(it) : saveLocal(it, 'reveal');
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

// 语言：手动选择的优先，否则跟随系统。本机的设置保存在电脑上（托盘菜单也跟着变），手机记在本地。
function applyLang(pref) {
  pref ? store.set('lang', pref) : store.del('lang');
  I18N.setLang(pref || I18N.auto());
  ['#optLang', '#myLang'].forEach(s => { const el = $(s); if (el) el.value = pref || ''; });
}

async function boot() {
  I18N.start(store.get('lang') || I18N.auto());
  ['#optLang', '#myLang'].forEach(s => { $(s).value = store.get('lang') || ''; });
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
