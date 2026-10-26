'use strict';

// Remember preferred widths; temporary window constraints never overwrite them.
const inboxWidths = { nav: null, list: null };
let inboxSplitters = null, inboxDrag = null;
const paneClamp = (v, min, max) => Math.max(min, Math.min(max, v));

function inboxWidthLimits() {
  const layout = $('.inbox-layout'), width = layout.clientWidth;
  const nav = !$('#workspaceSidebar').hidden, list = !$('#inboxListPanel').hidden;
  const grid = matchMedia('(min-width: 651px)').matches;
  const wide = matchMedia('(min-width: 1000px)').matches;
  const css = getComputedStyle(layout);
  const navDefault = parseFloat(css.getPropertyValue('--inbox-nav-default'));
  const listDefault = parseFloat(css.getPropertyValue('--inbox-list-default'));
  const navMin = 148, listMin = 240, readerMin = 320;
  const navMax = Math.min(320, width - (wide && list ? listMin + readerMin : readerMin));
  const n = nav ? paneClamp(inboxWidths.nav ?? navDefault, navMin, navMax) : 0;
  const listMax = Math.min(640, width - n - readerMin);
  const l = list ? paneClamp(inboxWidths.list ?? listDefault, listMin, listMax) : 0;
  return { layout, grid, wide, nav, list, n, l, navDefault, listDefault,
    navMin, listMin, navMax: Math.min(navMax, wide && list ? width - l - readerMin : navMax), listMax };
}

function refreshInboxWidths() {
  if (!inboxSplitters) return;
  const s = inboxWidthLimits();
  s.layout.style.setProperty('--inbox-nav-size', s.n + 'px');
  s.layout.style.setProperty('--inbox-list-size', s.l + 'px');
  for (const [kind, el] of Object.entries(inboxSplitters)) {
    el.hidden = !s.grid || (kind === 'nav' ? !s.nav : !s.wide || !s.list);
    const width = kind === 'nav' ? s.n : s.l;
    el.style.left = (kind === 'nav' ? s.n : s.n + s.l) + 'px';
    el.setAttribute('aria-valuemin', s[kind + 'Min']);
    el.setAttribute('aria-valuemax', s[kind + 'Max']);
    el.setAttribute('aria-valuenow', Math.round(width));
    el.setAttribute('aria-valuetext', Math.round(width) + ' px');
  }
}

function saveInboxWidths() {
  store.set('paneWidths', JSON.stringify(inboxWidths));
}

function endInboxDrag(cancel = false) {
  if (!inboxDrag) return;
  const d = inboxDrag;
  inboxDrag = null;
  if (cancel) inboxWidths[d.kind] = d.previous;
  else saveInboxWidths();
  document.body.classList.remove('resizing-panes');
  if (d.el.hasPointerCapture(d.pointer)) d.el.releasePointerCapture(d.pointer);
  refreshInboxWidths();
}

function resetInboxWidth(kind) {
  inboxWidths[kind] = null;
  saveInboxWidths();
  refreshInboxWidths();
}

function bindInboxWidths() {
  try {
    const saved = JSON.parse(store.get('paneWidths') || '{}');
    for (const kind of ['nav', 'list']) {
      const v = saved[kind];
      if (typeof v === 'number' && Number.isFinite(v)) inboxWidths[kind] = paneClamp(v, kind === 'nav' ? 148 : 240, kind === 'nav' ? 320 : 640);
    }
  } catch { /* Invalid preferences fall back to the responsive defaults. */ }
  inboxSplitters = {};
  const layout = $('.inbox-layout');
  for (const [kind, label, controls] of [['nav', '调整侧边栏宽度', 'workspaceSidebar'], ['list', '调整内容列表宽度', 'inboxListPanel']]) {
    const el = document.createElement('div');
    el.id = kind + 'Splitter'; el.className = 'pane-splitter'; el.tabIndex = 0;
    el.setAttribute('role', 'separator'); el.setAttribute('aria-orientation', 'vertical');
    el.setAttribute('aria-label', label); el.setAttribute('aria-controls', controls);
    el.title = '拖动调整宽度 · 双击恢复默认';
    layout.append(el); inboxSplitters[kind] = el;
    el.addEventListener('pointerdown', e => {
      if (e.button !== 0 || !e.isPrimary || inboxDrag) return;
      e.preventDefault();
      const s = inboxWidthLimits();
      inboxDrag = { kind, el, pointer: e.pointerId, x: e.clientX, width: kind === 'nav' ? s.n : s.l, previous: inboxWidths[kind] };
      el.focus({ preventScroll: true }); el.setPointerCapture(e.pointerId);
      document.body.classList.add('resizing-panes');
    });
    el.addEventListener('pointermove', e => {
      const d = inboxDrag;
      if (!d || d.el !== el || d.pointer !== e.pointerId) return;
      const s = inboxWidthLimits();
      inboxWidths[kind] = paneClamp(d.width + e.clientX - d.x, s[kind + 'Min'], s[kind + 'Max']);
      refreshInboxWidths();
    });
    el.addEventListener('pointerup', () => endInboxDrag());
    el.addEventListener('pointercancel', () => endInboxDrag(true));
    el.addEventListener('lostpointercapture', () => endInboxDrag(true));
    el.addEventListener('dblclick', () => resetInboxWidth(kind));
    el.addEventListener('keydown', e => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Enter'].includes(e.key)) return;
      e.preventDefault();
      if (e.key === 'Enter') { resetInboxWidth(kind); return; }
      const s = inboxWidthLimits(), current = kind === 'nav' ? s.n : s.l;
      const next = e.key === 'Home' ? s[kind + 'Min'] : e.key === 'End' ? s[kind + 'Max'] : current + (e.key === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? 48 : 16);
      inboxWidths[kind] = paneClamp(next, s[kind + 'Min'], s[kind + 'Max']);
      saveInboxWidths(); refreshInboxWidths();
    });
  }
  document.addEventListener('keydown', e => { if (e.key === 'Escape' && inboxDrag) { e.preventDefault(); e.stopImmediatePropagation(); endInboxDrag(true); } }, true);
  window.addEventListener('blur', () => endInboxDrag());
  new ResizeObserver(() => { endInboxDrag(); refreshInboxWidths(); }).observe(layout);
  refreshInboxWidths();
}
