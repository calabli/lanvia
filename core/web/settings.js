'use strict';

// 本机仪表盘（设置、配对二维码）与配对流程。

/* ================= 本机仪表盘 ================= */

async function loadInfo() {
  try { S.info = await api('GET', '/api/info'); } catch { return; }
  const i = S.info;
  $('#pairCode').textContent = i.code;
  const sel = $('#urlSelect');
  const prev = sel.value;
  sel.innerHTML = (i.urls || []).map(u => `<option>${esc(u)}</option>`).join('') || '<option>未检测到局域网地址</option>';
  if (prev && i.urls?.includes(prev)) sel.value = prev;
  // 只有一个地址时没必要让人选
  const single = (i.urls || []).length <= 1;
  sel.hidden = single;
  $('#urlText').hidden = !single;
  $('#urlText').textContent = (i.urls || [])[0] || '未检测到局域网地址';
  drawQR();
  $('#nodeName').value = i.name;
  renderMode();
  renderBlock();
  $('#optNotify').checked = i.settings.notify !== false;
  loadUpdate();
  $('#optApply').checked = i.settings.autoApply;
  $('#optStrict').checked = !!i.settings.strictEnc;
  if ((i.settings.lang || '') !== (store.get('lang') || '')) applyLang(i.settings.lang || '');
  const keep = String(i.settings.historyDays ?? 30), ks = $('#optKeep');
  if (![...ks.options].some(o => o.value === keep)) ks.add(new Option(`${keep} 天`, keep)); // 手动改过配置文件的情况
  ks.value = keep;
  const fetchMB = String(i.settings.autoFetchMB ?? 100), fs = $('#optFetch');
  if (![...fs.options].some(o => o.value === fetchMB)) fs.add(new Option(`${fetchMB} MB 以下`, fetchMB));
  fs.value = fetchMB;
  const st = String(i.settings.storageMB ?? 2048), ss = $('#optStorage');
  if (![...ss.options].some(o => o.value === st)) ss.add(new Option(`${st} MB`, st));
  ss.value = st;
  renderStorage(i.storage, i.settings.storageMB);
  $('#clipWarn').hidden = !!i.clipboard;
  $('#token').textContent = i.token;
  $('#optToken').checked = !i.settings.tokenOff;
  $('#tokenBox').hidden = !!i.settings.tokenOff;
  const base = (i.urls || [])[0] || `http://localhost:${i.port}`;
  $('#curl').textContent =
`# 发送文本
curl -H "Authorization: Bearer ${i.token}" --data-binary "你好" ${base}/api/clip
# 获取最新文本
curl -H "Authorization: Bearer ${i.token}" ${base}/api/latest
# 发送文件
curl -H "Authorization: Bearer ${i.token}" --data-binary @photo.jpg "${base}/api/upload?name=photo.jpg"`;
  $('#dataDir').textContent = '数据目录：' + i.dataDir;
  renderManual(i.manual || []);
  $('#nearby').innerHTML = (i.nearby || []).map(n =>
    `<div class="nearby"><span>${kindIcon(osKind(n.os))} <b data-raw>${esc(n.name)}</b> 在同一网络，但配对码不同</span><span class="muted">${esc(n.addr.split(':')[0])}</span></div>`).join('');
}
const osKind = os => ({ darwin: 'mac', windows: 'windows', linux: 'linux' }[os] || 'web');

async function drawQR() {
  const url = $('#urlSelect').value;
  if (!S.info || !url.startsWith('http')) { $('#qr').innerHTML = ''; return; }
  try {
    const { size, bits } = await api('GET', '/api/qr?text=' + encodeURIComponent(`${url}/#k=${normCode(S.info.code)}`));
    let d = '';
    for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (bits[y * size + x] === '1') d += `M${x} ${y}h1v1h-1z`;
    $('#qr').innerHTML = `<svg viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges"><path fill="#111" d="${d}"/></svg>`;
  } catch {}
}

async function saveSettings(patch) {
  try { S.info = await api('POST', '/api/settings', patch); if (patch.name) { S.node.name = S.info.name; connect(); } }
  catch (e) { toast('保存失败：' + e.message); }
}

/* ================= 配对 ================= */

function showPair(err) {
  $('#app').hidden = true;
  $('#pair').hidden = false;
  $('#pairError').textContent = err || '';
  setTimeout(() => $('#pairInput').focus(), 50);
}
function unpair(msg) {
  store.del('code'); setCode(null);
  if (S.es) { S.es.close(); S.es = null; }
  showPair(msg);
}
async function tryCode(code) {
  setCode(code);
  try {
    const r = await send('GET', '/api/info');
    if (r.status === 401) return false;
    return r.ok;
  } catch { return false; }
}
function startApp() {
  $('#pair').hidden = true;
  $('#app').hidden = PREVIEW;
  document.body.classList.toggle('preview-mode', PREVIEW);
  connect();
}
