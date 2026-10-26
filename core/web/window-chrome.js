'use strict';

// Only the Windows main WebView requests custom chrome; browsers, the tray,
// preview windows and other operating systems keep their existing frame.
const integratedWindow = isLoopback && DESKTOP === 'main' && QS.get('chrome') === 'windows';
function setNativeWindowMaximized(maximized) {
  if (!integratedWindow) return;
  document.body.classList.toggle('window-maximized', !!maximized);
  const button = $('#windowMaximise');
  button.setAttribute('aria-label', maximized ? '还原窗口' : '最大化');
  button.title = maximized ? '还原窗口' : '最大化';
  button.innerHTML = maximized
    ? '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5.5 5.5v-3h8v8h-3"/><rect x="2.5" y="5.5" width="8" height="8"/></svg>'
    : '<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="3.5" width="9" height="9"/></svg>';
}
function bindWindowChrome() {
  if (!integratedWindow) return;
  document.body.classList.add('native-chrome');
  $('#windowControls').hidden = false;
  $('#windowControls').addEventListener('click', async e => {
    const button = e.target.closest('[data-window-action]');
    if (!button || button.disabled) return;
    button.disabled = true;
    try { await api('POST', '/api/desktop/window-' + button.dataset.windowAction); }
    catch (err) { toast('窗口操作失败：' + err.message); }
    finally { button.disabled = false; }
  });
  api('POST', '/api/desktop/window-state').catch(() => {});
  // Native drag regions handle window movement and double-click maximize.
  // No JS drag simulation: snap, DPI changes and restoring from maximized stay native.
}
