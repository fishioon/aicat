import { host } from './client.js';

const $ = selector => document.querySelector(selector);
const viewport = $('#rows-viewport');
const rowHeight = 30;
const overscan = 6;
const records = new Map();
const views = { request: 'headers', response: 'headers' };
const bodyCache = new Map();
const metrics = { batches: 0, rowsRendered: 0, renderMs: [], filterMs: [], deliveryMs: [], framesMs: [], errors: [], maxRowsInDOM: 0 };
let filtered = [];
let selected = 0;
let detail = null;
let generation = 0;
let hostFilter = '';
let recording = true;
let rowFrame = 0;

function remember(array, value) {
  array.push(value);
  if (array.length > 600) array.shift();
}

// The existing native matcher accepts case-insensitive fuzzy terms joined by
// AND, without reordering capture order. This page preserves that membership.
function matches(url, query) {
  const text = url.toLowerCase();
  return query.toLowerCase().trim().split(/\s+/u).filter(Boolean).every(term => {
    let position = 0;
    for (const char of term) {
      const found = text.indexOf(char, position);
      if (found < 0) return false;
      position = found + char.length;
    }
    return true;
  });
}

function filter() {
  const started = performance.now();
  const query = $('#search').value;
  filtered = [...records.values()].sort((a, b) => a.id - b.id).filter(row =>
    (!hostFilter || new URL(row.url).host === hostFilter) && matches(row.url, query));
  $('#count').textContent = `${filtered.length} 个请求`;
  $('#list-empty').hidden = filtered.length > 0;
  $('#list-empty').textContent = records.size ? '没有匹配的请求' : '等待流量 · 请在客户端配置代理';
  $('#rows-space').style.height = `${filtered.length * rowHeight}px`;
  if (selected && !filtered.some(row => row.id === selected)) clearSelection();
  remember(metrics.filterMs, performance.now() - started);
  scheduleRows();
}

function scheduleRows() {
  if (rowFrame) return;
  rowFrame = requestAnimationFrame(() => { rowFrame = 0; renderRows(); });
}

function renderRows() {
  const started = performance.now();
  const first = Math.max(0, Math.floor(viewport.scrollTop / rowHeight) - overscan);
  const last = Math.min(filtered.length, Math.ceil((viewport.scrollTop + viewport.clientHeight) / rowHeight) + overscan);
  const fragment = document.createDocumentFragment();
  for (let index = first; index < last; index++) {
    const record = filtered[index];
    const row = document.createElement('div');
    row.className = 'columns traffic-row';
    row.dataset.id = String(record.id);
    row.dataset.stripe = String(index % 2 === 1);
    row.setAttribute('role', 'row');
    row.setAttribute('aria-selected', String(record.id === selected));
    const values = [record.id, record.url, record.method, record.statusCode || '—', `${(record.durationNs / 1e6).toFixed(0)} ms`, `${record.responseBytes} B`];
    const classes = ['id', 'url', 'method', 'status', 'duration', 'size'];
    values.forEach((value, column) => {
      const cell = document.createElement('span');
      cell.className = classes[column];
      cell.textContent = value;
      row.append(cell);
    });
    fragment.append(row);
  }
  $('#rows').style.transform = `translateY(${first * rowHeight}px)`;
  $('#rows').replaceChildren(fragment);
  metrics.rowsRendered++;
  metrics.maxRowsInDOM = Math.max(metrics.maxRowsInDOM, last - first);
  remember(metrics.renderMs, performance.now() - started);
}

function renderDomains() {
  const counts = new Map();
  for (const row of records.values()) {
    const domain = new URL(row.url).host;
    counts.set(domain, (counts.get(domain) || 0) + 1);
  }
  const fragment = document.createDocumentFragment();
  const query = $('#domain-search').value.toLowerCase();
  for (const [domain, count] of [...counts].sort()) {
    if (!domain.toLowerCase().includes(query)) continue;
    const button = document.createElement('button');
    button.className = 'domain' + (hostFilter === domain ? ' active' : '');
    button.dataset.host = domain;
    const name = document.createElement('span');
    name.textContent = domain;
    const amount = document.createElement('span');
    amount.textContent = count;
    button.append(name, amount);
    fragment.append(button);
  }
  $('#domains').replaceChildren(fragment);
  $('#domain-count').textContent = counts.size;
  $('#all-domains').classList.toggle('active', !hostFilter);
}

function clearSelection() {
  selected = 0;
  detail = null;
  generation++;
  bodyCache.clear();
  $('#detail-empty').hidden = false;
  $('#detail-content').hidden = true;
  $('#selection').textContent = '已选择 0 项';
  scheduleRows();
}

async function select(id) {
  const version = ++generation;
  selected = id;
  detail = null;
  bodyCache.clear();
  $('#detail-empty').hidden = true;
  $('#detail-content').hidden = false;
  $('#detail-title').textContent = `#${id} · 正在读取…`;
  for (const panel of document.querySelectorAll('.inspector')) {
    panel.querySelector('.headers').replaceChildren();
    panel.querySelector('.raw').textContent = '';
    panel.querySelector('.body-notice').textContent = '';
  }
  $('#selection').textContent = '已选择 1 项';
  scheduleRows();
  try {
    const record = await host.record(id);
    if (version !== generation || !records.has(id)) return;
    detail = record;
    $('#detail-title').textContent = `#${id}　${record.method}　${record.url}`;
    await Promise.all(['request', 'response'].map(side => renderDetail(side, version)));
  } catch (error) {
    if (version === generation) $('#detail-title').textContent = error.message;
  }
}

function headerText(message) {
  return Object.entries(message.headers || {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n');
}

async function renderDetail(side, version = generation) {
  if (!detail) return;
  const record = detail;
  const panel = $(`[data-side="${side}"]`);
  const raw = views[side] === 'raw';
  panel.querySelector('.raw').hidden = !raw;
  panel.querySelector('.headers').hidden = raw;
  for (const tab of panel.querySelectorAll('.tab')) tab.classList.toggle('active', tab.dataset.view === views[side]);
  const message = side === 'request' ? record.requestReceived : record.responseSent;
  if (!message) {
    panel.querySelector('.raw').textContent = '响应尚未到达';
    return;
  }
  const heading = side === 'request'
    ? `${message.method} ${new URL(record.url).pathname + new URL(record.url).search} ${message.proto}`
    : `${message.proto || 'HTTP'} ${message.statusCode}`;
  if (!raw) {
    const fragment = document.createDocumentFragment();
    for (const [name, values] of Object.entries(message.headers || {})) for (const value of values) {
      const row = document.createElement('div');
      row.className = 'header-row';
      for (const text of [name, value]) {
        const cell = document.createElement('span');
        cell.textContent = text;
        row.append(cell);
      }
      fragment.append(row);
    }
    panel.querySelector('.headers').replaceChildren(fragment);
    return;
  }
  panel.querySelector('.raw').textContent = `${heading}\n${headerText(message)}\n\n正在读取正文…`;
  try {
    const info = message.body;
    let text = '';
    let notice = '';
    if (info.id && !record.tunnel) {
      const key = `${info.id}:${info.captured}:${info.state}`;
      if (!bodyCache.has(key)) bodyCache.set(key, host.body(info.id));
      const body = await bodyCache.get(key);
      const bytes = Uint8Array.from(atob(body.data), char => char.charCodeAt(0));
      try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
      catch { text = [...bytes].map(byte => byte.toString(16).padStart(2, '0')).join(' '); notice = '二进制正文，以十六进制显示'; }
      if (body.info.state !== 'complete') notice = `正文状态 ${body.info.state} · 保留 ${body.info.captured} B / 已观察 ${body.info.observed} B`;
    } else notice = record.tunnel ? 'CONNECT 加密隧道' : '正文未采集';
    if (version !== generation || views[side] !== 'raw') return;
    panel.querySelector('.raw').textContent = `${heading}\n${headerText(message)}\n\n${text}`;
    panel.querySelector('.body-notice').textContent = notice;
  } catch (error) {
    if (version === generation) panel.querySelector('.body-notice').textContent = error.message;
  }
}

$('#search').addEventListener('input', () => { viewport.scrollTop = 0; filter(); });
$('#domain-search').addEventListener('input', renderDomains);
viewport.addEventListener('scroll', scheduleRows, { passive: true });
new ResizeObserver(scheduleRows).observe(viewport);
$('#rows').addEventListener('click', event => {
  const row = event.target.closest('[data-id]');
  if (row) select(Number(row.dataset.id));
});
$('#domains').addEventListener('click', event => {
  const row = event.target.closest('[data-host]');
  if (row) { hostFilter = row.dataset.host; viewport.scrollTop = 0; filter(); renderDomains(); }
});
$('#all-domains').addEventListener('click', () => { hostFilter = ''; filter(); renderDomains(); });
for (const tab of document.querySelectorAll('.tab')) tab.addEventListener('click', () => {
  const side = tab.closest('[data-side]').dataset.side;
  views[side] = tab.dataset.view;
  renderDetail(side);
});
$('#theme').addEventListener('click', () => {
  document.documentElement.dataset.theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
});
$('#clear').addEventListener('click', async () => {
  await host.clear();
  hostFilter = '';
  $('#search').value = '';
  $('#domain-search').value = '';
  clearSelection();
});
$('#recording').addEventListener('click', async () => {
  await host.recording(!recording);
  recording = !recording;
  $('#recording-label').textContent = recording ? '正在录制' : '继续录制';
});
document.addEventListener('keydown', event => {
  if (event.key === 'Escape') clearSelection();
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); $('#search').focus(); }
});
window.addEventListener('error', event => metrics.errors.push(event.message));
window.addEventListener('unhandledrejection', event => metrics.errors.push(String(event.reason)));

let lastFrame = performance.now();
function frame(now) {
  if (document.visibilityState === 'visible') remember(metrics.framesMs, now - lastFrame);
  lastFrame = now;
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);
window.probe = {
  metrics,
  state: () => ({ total: records.size, visible: filtered.length, selected, ids: filtered.map(row => row.id),
    domRows: $('#rows').childElementCount, focused: document.activeElement?.id,
    scrollTop: viewport.scrollTop, detailRevision: detail?.revision || 0 }),
  status: () => host.status(),
};

const controller = new AbortController();
window.addEventListener('pagehide', () => controller.abort());
host.watch(update => {
  metrics.batches++;
  if (update.reset) records.clear();
  for (const id of update.removed) records.delete(id);
  for (const row of update.items) records.set(row.id, row);
  filter();
  renderDomains();
  if (selected && update.items.some(row => row.id === selected && row.revision > (detail?.revision || 0))) select(selected);
  requestAnimationFrame(() => remember(metrics.deliveryMs, Date.now() - update.sentAt));
}, (connected, error) => {
  $('#connection').textContent = connected ? '代理监听中' : '正在重新连接…';
  if (error) metrics.errors.push(error);
}, controller.signal);
