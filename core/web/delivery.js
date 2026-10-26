'use strict';

// Receipt state is separate from content: refreshing it cannot discard edits.
const DELIVERIES = new Map();
let lastDelivery = null, sendBusy = false, sendError = null;
function deliveryLabel(d) {
  if (!d.targets.length) return '已保存到本机 · 暂无其他设备在线';
  const received = d.targets.filter(t => t.state === 'received').length;
  if (d.targets.some(t => t.state === 'pending')) return `等待接收确认 · ${received}/${d.targets.length}`;
  if (received < d.targets.length) return `部分设备未确认 · ${received}/${d.targets.length}`;
  return d.kind === 'file' ? `文件信息已接收 · ${received} 台设备` : `已接收 · ${received} 台设备`;
}
function deliveryClass(d) {
  return d.targets.some(t => t.state === 'pending') ? 'pending'
    : d.targets.some(t => t.state !== 'received') ? 'failed' : 'received';
}
function deliveryBadge(id) {
  const d = DELIVERIES.get(id);
  if (!d) return '';
  const retry = !d.targets.some(t => t.state === 'pending') && (!d.targets.length || d.targets.some(t => t.state !== 'received'));
  return `<div class="delivery-row"><span class="delivery-badge ${deliveryClass(d)}">${esc(deliveryLabel(d))}</span>${retry ? `<button type="button" class="delivery-retry" data-delivery-retry="${d.id}">重试同步</button>` : ''}</div>`;
}
function receiveDelivery(d) {
  if (!d?.id || !Array.isArray(d.targets)) return;
  DELIVERIES.set(d.id, d);
  if (DELIVERIES.size > 128) DELIVERIES.delete(DELIVERIES.keys().next().value);
  if (!lastDelivery || d.ts >= (DELIVERIES.get(lastDelivery)?.ts || 0)) lastDelivery = d.id;
  renderList(); renderDelivery();
}
function renderDelivery() {
  const box = $('#sendStatus');
  if (!box || sendBusy) return;
  if (sendError) { box.hidden = false; box.className = 'send-status failed'; box.textContent = sendError; return; }
  const d = DELIVERIES.get(lastDelivery);
  box.hidden = !d && !lastDelivery;
  if (!d) {
    if (lastDelivery) { box.className = 'send-status pending'; box.textContent = '已保存到本机 · 等待同步状态'; }
    return;
  }
  const pending = d.targets.some(t => t.state === 'pending');
  const retry = !pending && (!d.targets.length || d.targets.some(t => t.state !== 'received'));
  const states = {pending:'等待确认',received:'已接收',failed:'未确认',blocked:'需升级以支持加密'};
  box.className = 'send-status ' + deliveryClass(d);
  box.innerHTML = `<div class="delivery-summary"><span>${esc(deliveryLabel(d))}</span>${retry ? '<button type="button" class="delivery-retry">重试同步</button>' : ''}</div>`
    + (d.targets.length ? `<details><summary>查看设备结果</summary><ul>${d.targets.map(t => `<li><span data-raw>${esc(t.name)}</span><span>${states[t.state] || '未确认'}</span></li>`).join('')}</ul></details>` : '')
    + (d.kind === 'file' ? '<div class="delivery-note">接收确认针对文件信息，文件内容按需下载。</div>' : '');
  box.querySelector('button')?.addEventListener('click', async e => {
    e.currentTarget.disabled = true;
    try { await api('POST', `/api/deliveries/${d.id}/retry`); }
    catch (err) { toast('重试失败：' + err.message); renderDelivery(); }
  });
}
function sendingStatus() {
  sendError = null;
  sendBusy = true;
  const box = $('#sendStatus');
  box.hidden = false; box.className = 'send-status pending'; box.textContent = '正在发送…';
}
function sendFailure(err) {
  sendBusy = false;
  sendError = '发送失败，内容已保留，请重试：' + err.message;
  renderDelivery();
}
async function acknowledgeDelivery(id) {
  if (!S.you || S.local || !S.items.some(it => it.id === id)) return;
  try { await api('POST', '/api/deliveries/ack', {id, client:S.you}); } catch { /* Timeout stays visible on sender. */ }
}
document.addEventListener('click', async e => {
  const b = e.target.closest('[data-delivery-retry]');
  if (!b) return;
  b.disabled = true;
  try { await api('POST', `/api/deliveries/${b.dataset.deliveryRetry}/retry`); }
  catch (err) { toast('重试失败：' + err.message); b.disabled = false; }
});
