'use strict';

// 设置页里的“关于与更新”：当前版本、检查更新、下载进度、更新内容和“自动更新”开关。
// 下载和安装由桌面客户端完成（见 cmd/lanvia-desktop/updater.go），状态通过 SSE 的 updater 事件推过来。

let UPD = null;

async function loadUpdate() {
  if (!S.local) return;
  try { renderUpdate(await api('GET', '/api/update')); } catch {}
}

function renderUpdate(st) {
  UPD = st;
  const sec = $('#updSection');
  if (!sec || !st) return;
  $('#updCur').textContent = 'v' + st.current;
  const btn = $('#updBtn'), bar = $('#updBar'), status = $('#updStatus'), notes = $('#updNotes');
  $('#updAuto').hidden = btn.hidden = !st.supported;
  $('#optAutoUpdate').checked = S.info?.settings?.autoUpdate !== false;
  status.classList.toggle('warn', !!st.error);
  bar.hidden = !st.downloading;
  if (st.downloading) $('i', bar).style.width = Math.round((st.progress || 0) * 100) + '%';
  const auto = S.info?.settings?.autoUpdate !== false;
  let text, label = '检查更新', busy = st.checking || st.downloading || st.installing;
  if (!st.supported) text = '命令行版不能自动更新，请到 GitHub 下载新版本';
  else if (st.installing) text = '正在安装，Lanvia 会自动重新打开';
  else if (st.downloading) text = `正在下载 ${st.latest} · ${Math.round((st.progress || 0) * 100)}%`;
  else if (st.checking) text = '正在检查更新…';
  else if (st.error) text = st.error;
  else if (st.ready) text = auto ? `${st.latest} 已下载，会在你没在用 Lanvia 窗口时自动安装` : `${st.latest} 已下载，点“立即更新”安装`;
  else if (st.latest) text = `有新版本 ${st.latest}`;
  else text = st.checkedAt ? `已是最新版本 · ${relTime(st.checkedAt)}检查` : '已是最新版本';
  if (st.latest && !busy) label = '立即更新';
  status.textContent = text;
  btn.textContent = st.installing ? '正在更新…' : st.downloading ? '正在下载…' : st.checking ? '正在检查…' : label;
  btn.disabled = busy;
  btn.classList.toggle('primary', !!st.latest && !busy);
  notes.hidden = !st.latest || !st.notes;
  if (st.latest && st.notes) $('#updNotesText').textContent = plainNotes(st.notes);
}

// 发布说明是 Markdown：去掉标题井号、加粗、代码和链接标记，按纯文本显示
const plainNotes = md => md.replace(/\r/g, '').replace(/^#{1,6}\s*/gm, '').replace(/\*\*(.+?)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1')
  .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/\n{3,}/g, '\n\n').trim();

function bindUpdate() {
  $('#updBtn').onclick = async () => {
    const action = UPD?.latest ? 'install' : 'check';
    try { renderUpdate(await api('POST', '/api/update/' + action)); }
    catch (e) { toast(e.message); }
  };
  $('#optAutoUpdate').onchange = async e => {
    await saveSettings({ autoUpdate: e.target.checked });
    renderUpdate(UPD);
    // 刚打开自动更新、而且已经发现新版本：顺便开始下载
    if (e.target.checked && UPD?.latest && !UPD.ready) api('POST', '/api/update/check').catch(() => {});
  };
}
