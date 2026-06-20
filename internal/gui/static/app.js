/* Eigen desktop GUI app logic.
 * Real chat core: markdown rendering, incremental timeline, working indicator,
 * correct send/state/stream contract. */
'use strict';

const state = {
  sessions: [],
  active: null,
  source: null,
  state: null,
  poll: null,
  streaming: false,
  desktopEvents: false,
  userPinnedBottom: true,
  approvals: {},
  tools: {},
  feature: 'chat',
  turnTokens: {in: 0, out: 0},
  // rendered timeline model: maps a stable key -> node so polling can update
  // in place instead of rebuilding the whole transcript (which flickered and
  // destroyed the streaming assistant message).
  rendered: new Map(),
};

const $ = (id) => document.getElementById(id);
const sessionsEl = $('sessions');
const timelineEl = $('timeline');
const titleEl = $('title');
const metaEl = $('meta');
const daemonEl = $('daemon');
const inspectorEl = $('inspector');
const inputEl = $('input');
const sendEl = $('send');
const jumpLatestEl = $('jump-latest');
const modelInput = $('model-input');
const effortSelect = $('effort-select');
const effortControl = $('effort-control');
const permSelect = $('perm-select');
const searchSelect = $('search-select');
const searchControl = $('search-control');
const fastToggle = $('fast-toggle');
const newSessionModal = $('new-session-modal');
const newSessionForm = $('new-session-form');
const newSessionClose = $('new-session-close');
const newSessionCancel = $('new-session-cancel');
const newSessionError = $('new-session-error');
const sessionDirInput = $('session-dir');
const sessionModelInput = $('session-model');
const sessionPermInput = $('session-perm');
const profileButton = $('profile-button');
const profileModal = $('profile-modal');
const profileForm = $('profile-form');
const profileClose = $('profile-close');
const profileCancel = $('profile-cancel');
const profileClear = $('profile-clear');
const profileError = $('profile-error');
const profileText = $('profile-text');
const systemButton = $('system-button');
const systemModal = $('system-modal');
const systemClose = $('system-close');
const systemCancel = $('system-cancel');
const systemRefresh = $('system-refresh');
const systemBody = $('system-body');
const featureNav = $('feature-nav');
const featureStage = $('feature-stage');
const chatStage = $('chat-stage');
const workspaceEl = $('workspace');
const statusIndicator = $('status-indicator');
const statusText = $('status-text');
const tokenUsage = $('token-usage');
const goalBar = $('goal-bar');
const goalText = $('goal-text');
const interruptBtn = $('interrupt');

/* ---------------- Desktop bridge / API ---------------- */
function desktop() {
  if (!window.go) return null;
  if (window.go.gui?.DesktopApp) return window.go.gui.DesktopApp;
  for (const pkg of Object.values(window.go)) {
    if (pkg?.DesktopApp) return pkg.DesktopApp;
  }
  return null;
}
const hasDesktopBridge = () => !!desktop();
async function waitForDesktopBridge() {
  for (let i = 0; i < 20; i++) {
    if (hasDesktopBridge()) return true;
    await new Promise(r => setTimeout(r, 50));
  }
  return false;
}
async function api(path, opts = {}) {
  const res = await fetch(path, {headers: {'content-type': 'application/json'}, ...opts});
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) throw new Error(data?.error || res.statusText);
  return data;
}
async function getHealth() { return hasDesktopBridge() ? desktop().Health() : api('/api/health'); }
async function getSessions() { return hasDesktopBridge() ? desktop().Sessions() : api('/api/sessions'); }
async function getUserProfile() { return hasDesktopBridge() ? desktop().UserProfile() : (await api('/api/profile')).profile || ''; }
async function saveUserProfile(p) { return hasDesktopBridge() ? desktop().WriteUserProfile(p) : api('/api/profile', {method: 'POST', body: JSON.stringify({profile: p})}); }
async function createSession(opts = {}) {
  const dir = opts.dir || '', model = opts.model || '', perm = opts.perm || '';
  return hasDesktopBridge() ? {id: await desktop().NewSession(dir, model, perm)} : api('/api/sessions', {method: 'POST', body: JSON.stringify({dir, model, perm})});
}
async function getState(id) { return hasDesktopBridge() ? desktop().State(id) : api(`/api/sessions/${encodeURIComponent(id)}/state`); }
async function sendInput(id, text, allowTools = []) {
  if (hasDesktopBridge()) return {steered: await desktop().Input(id, text, allowTools)};
  return api(`/api/sessions/${encodeURIComponent(id)}/input`, {method: 'POST', body: JSON.stringify({text, allow_tools: allowTools})});
}
async function approveCall(id, approval, allow) { return hasDesktopBridge() ? desktop().Approve(id, approval, allow) : api(`/api/sessions/${encodeURIComponent(id)}/approve`, {method: 'POST', body: JSON.stringify({approval, allow})}); }
async function sessionAction(id, action) {
  if (hasDesktopBridge()) {
    if (action === 'interrupt') return desktop().Interrupt(id);
    if (action === 'resend') return desktop().Resend(id);
    if (action === 'clear') return desktop().Clear(id);
  }
  return api(`/api/sessions/${encodeURIComponent(id)}/${action}`, {method: 'POST', body: '{}'});
}
async function sessionSetting(id, setting, value) {
  if (hasDesktopBridge()) {
    if (setting === 'model') return desktop().SetModel(id, value);
    if (setting === 'effort') return desktop().SetEffort(id, value);
    if (setting === 'perm') return desktop().SetPerm(id, value);
    if (setting === 'search') return desktop().SetSearch(id, value);
    if (setting === 'fast') return desktop().SetFast(id, !!value);
    if (setting === 'goal') return desktop().SetGoal(id, value);
  }
  return api(`/api/sessions/${encodeURIComponent(id)}/${setting}`, {method: 'POST', body: JSON.stringify({value})});
}
async function killShell(id, shell) { return hasDesktopBridge() ? desktop().KillShell(id, shell) : api(`/api/sessions/${encodeURIComponent(id)}/kill-shell`, {method: 'POST', body: JSON.stringify({shell})}); }
async function detachBash(id) { return hasDesktopBridge() ? desktop().DetachBash(id) : api(`/api/sessions/${encodeURIComponent(id)}/detach-bash`, {method: 'POST'}); }
async function compactSession(id, target = 0) { return hasDesktopBridge() ? desktop().Compact(id, target) : api(`/api/sessions/${encodeURIComponent(id)}/compact`, {method: 'POST', body: JSON.stringify({target})}); }
async function addDir(id, path) { return hasDesktopBridge() ? desktop().AddDir(id, path) : api(`/api/sessions/${encodeURIComponent(id)}/add-dir`, {method: 'POST', body: JSON.stringify({path})}); }
async function renameSession(id, title) {
  if (hasDesktopBridge() && desktop().SetTitle) return desktop().SetTitle(id, title);
  return api(`/api/sessions/${encodeURIComponent(id)}/title`, {method: 'POST', body: JSON.stringify({value: title})});
}
async function deleteSession(id) {
  if (hasDesktopBridge() && desktop().Remove) return desktop().Remove(id);
  return api('/api/sessions', {method: 'DELETE', body: JSON.stringify({id})});
}

/* ---------------- Boot / sessions ---------------- */
async function boot() {
  await waitForDesktopBridge();
  await refreshHealth();
  await refreshSessions();
  setInterval(refreshSessions, 3500);
}

async function refreshHealth() {
  try {
    const h = await getHealth();
    daemonEl.textContent = h.ok ? (hasDesktopBridge() ? 'desktop connected' : 'daemon connected') : 'daemon offline';
  } catch { daemonEl.textContent = 'daemon error'; }
}

async function refreshSessions() {
  try {
    state.sessions = await getSessions();
    renderSessions();
    if (!state.active && state.sessions.length) openSession(sessionID(state.sessions[0]));
  } catch (err) {
    sessionsEl.innerHTML = `<div class="session"><div class="session-title">${escapeHtml(err.message)}</div></div>`;
  }
}

function renderSessions() {
  sessionsEl.innerHTML = '';
  for (const s of state.sessions) {
    const id = sessionID(s);
    const row = document.createElement('button');
    row.className = `session ${state.active === id ? 'active' : ''}`;
    row.innerHTML = `
      <div class="session-title">${escapeHtml(s.title || s.Title || id)}</div>
      <div class="badge ${sessionStatus(s) === 'error' ? 'error' : ''}">${escapeHtml(sessionStatus(s) || 'idle')}</div>
      <div class="session-dir">${escapeHtml(shortPath(s.dir || s.Dir || ''))}</div>`;
    row.onclick = () => openSession(id);
    sessionsEl.appendChild(row);
  }
}

async function openSession(id) {
  state.active = id;
  state.rendered.clear();
  state.tools = {};
  state.approvals = {};
  timelineEl.innerHTML = '';
  renderSessions();
  closeLiveStream();
  if (state.poll) clearInterval(state.poll);
  inputEl.focus();
  try {
    const snap = await getState(id);
    applyState(id, snap, {force: true});
    connectEvents(id);
    state.poll = setInterval(() => refreshActiveState({force: !state.streaming}), 2000);
  } catch (err) {
    inspectorEl.textContent = `Failed to open session: ${err.message}`;
    setStatus('error', `Failed to open session`);
  }
}

async function refreshActiveState(opts = {}) {
  if (!state.active) return;
  try {
    const snap = await getState(state.active);
    applyState(state.active, snap, opts);
  } catch (err) {
    inspectorEl.textContent = `State refresh failed: ${err.message}`;
  }
}

function applyState(id, snap, opts = {}) {
  const before = state.state;
  state.state = snap;
  titleEl.textContent = snap.title || snap.Title || id;
  const provider = snap.provider || snap.Provider || '';
  const model = snap.model || snap.Model || '';
  const perm = snap.perm || snap.Perm || 'gated';
  const running = !!(snap.running || snap.Running);
  metaEl.textContent = [provider, model, `perm ${perm}`, running ? 'running' : 'idle'].filter(Boolean).join(' · ');
  updateControls(snap);
  updateComposerState(running);
  const messages = snap.messages || snap.Messages || [];
  const beforeMessages = before?.messages || before?.Messages || [];
  // Incremental: only diff-render when the message set actually changed.
  if (opts.force || messagesSignature(messages) !== messagesSignature(beforeMessages)) {
    renderTimeline(messages);
  }
  setStatus(running ? 'running' : 'idle', running ? 'Agent working…' : (state.active ? 'Ready' : 'No session'));
  updateGoalBar(snap);
  syncPendingApprovals(snap.pending || snap.Pending || []);
  updateInspector(snap);
  renderFeatureStage();
}

function updateGoalBar(snap) {
  const goal = (snap.goal || snap.Goal || '').trim();
  if (!goalBar) return;
  if (goal) {
    goalText.textContent = goal;
    goalBar.classList.remove('hidden');
  } else {
    goalBar.classList.add('hidden');
  }
}

function updateTokenUsage() {
  if (!tokenUsage) return;
  const {in: ti, out: to} = state.turnTokens;
  if (ti || to) {
    tokenUsage.classList.remove('hidden');
    let inEl = tokenUsage.querySelector('.tu-in');
    if (!inEl) { inEl = document.createElement('span'); inEl.className = 'tu-in'; tokenUsage.appendChild(inEl); }
    let outEl = tokenUsage.querySelector('.tu-out');
    if (!outEl) { outEl = document.createElement('span'); outEl.className = 'tu-out'; tokenUsage.appendChild(outEl); }
    inEl.textContent = `↑${formatTokens(ti)}`;
    outEl.textContent = `↓${formatTokens(to)}`;
  } else {
    tokenUsage.classList.add('hidden');
  }
}
function formatTokens(n) {
  n = Number(n || 0);
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}

function updateControls(snap) {
  const model = snap.model || snap.Model || '';
  const effort = snap.effort || snap.Effort || '';
  const perm = snap.perm || snap.Perm || 'gated';
  const search = snap.search || snap.Search || '';
  const fast = !!(snap.fast || snap.Fast);
  const fastOK = !!(snap.fast_ok || snap.FastOK);
  if (modelInput && document.activeElement !== modelInput) modelInput.value = model;
  if (effortSelect) effortSelect.value = effort || '';
  if (permSelect) permSelect.value = perm === 'auto' ? 'auto' : 'gated';
  if (searchSelect) searchSelect.value = search || 'off';
  searchControl?.classList.toggle('hidden', !search);
  fastToggle?.classList.toggle('hidden', !fastOK);
  fastToggle?.classList.toggle('active', fast);
  if (fastToggle) fastToggle.textContent = fast ? 'Fast on' : 'Fast';
}

function setStatus(level, text) {
  statusIndicator.className = `status-indicator ${level}`;
  statusText.textContent = text;
}
let flashTimer = null;
function flashStatus(msg, isError = false) {
  statusText.textContent = msg;
  if (flashTimer) clearTimeout(flashTimer);
  flashTimer = setTimeout(() => {
    const running = !!(state.state?.running || state.state?.Running);
    setStatus(running ? 'running' : (isError ? 'error' : 'idle'), running ? 'Agent working…' : 'Ready');
  }, 3500);
}

/* ---------------- Feature routing ---------------- */
function setFeature(feature) {
  state.feature = feature;
  workspaceEl.dataset.feature = feature;
  for (const btn of featureNav?.querySelectorAll('[data-feature]') || []) {
    btn.classList.toggle('active', btn.dataset.feature === feature);
  }
  renderFeatureStage();
}

/* ---------------- Markdown rendering ---------------- */
// Minimal, safe-ish markdown: escapes HTML, then applies fenced code, inline
// code, headings, bold/italic, links, lists. Code fences get copy buttons.
function renderMarkdown(src) {
  const parts = [];
  let i = 0;
  const text = String(src == null ? '' : src);
  // Split out fenced code blocks first.
  const fence = /```([\w+-]*)\n([\s\S]*?)```/g;
  let last = 0, m;
  while ((m = fence.exec(text)) !== null) {
    if (m.index > last) parts.push(renderInline(text.slice(last, m.index)));
    parts.push(codeBlockHTML(m[1] || '', m[2]));
    last = fence.lastIndex;
  }
  if (last < text.length) parts.push(renderInline(text.slice(last)));
  return parts.join('');
}

function codeBlockHTML(lang, code) {
  const id = 'cb' + Math.random().toString(36).slice(2, 9);
  const label = lang ? escapeHtml(lang) : 'text';
  return `<div class="code-block"><div class="code-block-head"><span>${label}</span><button class="copy-btn" type="button" data-copy-for="${id}">copy</button></div><pre id="${id}">${escapeHtml(code.replace(/\n$/, ''))}</pre></div>`;
}

function renderInline(text) {
  let s = escapeHtml(text);
  // headings
  s = s.replace(/^######\s+(.+)$/gm, '<h3>$1</h3>');
  s = s.replace(/^#####\s+(.+)$/gm, '<h3>$1</h3>');
  s = s.replace(/^####\s+(.+)$/gm, '<h3>$1</h3>');
  s = s.replace(/^###\s+(.+)$/gm, '<h3>$1</h3>');
  s = s.replace(/^##\s+(.+)$/gm, '<h2>$1</h2>');
  s = s.replace(/^#\s+(.+)$/gm, '<h1>$1</h1>');
  // lists (simple): group consecutive bullet/number lines
  s = wrapLists(s);
  // inline code
  s = s.replace(/`([^`\n]+)`/g, '<code class="inline">$1</code>');
  // bold then italic
  s = s.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>');
  // links [text](url) — url must be http/https/relative
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+|\/[^\s)]*)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  // paragraphs: wrap blocks separated by blank lines
  return s.split(/\n{2,}/).map(block => {
    if (/^\s*<(h\d|ul|ol|pre|div|p)/.test(block.trim())) return block;
    return '<p>' + block.trim().replace(/\n/g, '<br>') + '</p>';
  }).join('');
}

function wrapLists(s) {
  return s.replace(/((?:^[ \t]*[-*]\s+.+\n?)+)/gm, (block) => {
    const items = block.trim().split('\n').map(l => '<li>' + l.replace(/^[ \t]*[-*]\s+/, '') + '</li>').join('');
    return '<ul>' + items + '</ul>';
  }).replace(/((?:^[ \t]*\d+\.\s+.+\n?)+)/gm, (block) => {
    const items = block.trim().split('\n').map(l => '<li>' + l.replace(/^[ \t]*\d+\.\s+/, '') + '</li>').join('');
    return '<ol>' + items + '</ol>';
  });
}

/* ---------------- Timeline (incremental) ---------------- */
function renderTimeline(messages) {
  timelineEl.classList.toggle('empty', messages.length === 0);
  if (messages.length === 0) {
    timelineEl.innerHTML = `<div class="empty-state"><div class="empty-title">Ready for work.</div><div class="empty-copy">Send a message. Tool calls and approvals stream in here.</div></div>`;
    state.rendered.clear();
    return;
  }
  if (timelineEl.classList.contains('empty') || timelineEl.querySelector('.empty-state')) {
    timelineEl.innerHTML = '';
  }
  // Reconcile: keep existing nodes, add new ones, remove stale ones.
  const want = new Set();
  for (const msg of messages) {
    const role = (msg.role || msg.Role || 'message').toLowerCase();
    const text = msg.text || msg.Text || '';
    const key = `m:${role}:${messages.indexOf(msg)}:${text.length}`;
    want.add(key);
    let node = state.rendered.get(key);
    if (!node) {
      node = makeMessageNode(role, text);
      timelineEl.appendChild(node.el);
      state.rendered.set(key, node);
    } else if (node.text !== text) {
      // Update content in place (e.g. growing assistant message).
      node.bodyEl.innerHTML = role === 'assistant' || role === 'user' ? renderMarkdown(text) : escapeHtml(text);
      node.text = text;
    }
  }
  for (const [key, node] of state.rendered) {
    if (key.startsWith('m:') && !want.has(key)) {
      node.el.remove();
      state.rendered.delete(key);
    }
  }
  scrollToBottom();
}

function makeMessageNode(role, text) {
  const el = document.createElement('article');
  el.className = `message ${role}`;
  const label = document.createElement('div');
  label.className = 'role';
  label.textContent = role;
  const body = document.createElement('div');
  body.className = 'body';
  body.innerHTML = (role === 'assistant' || role === 'user') ? renderMarkdown(text) : escapeHtml(text);
  el.appendChild(label);
  el.appendChild(body);
  return {el, bodyEl: body, text};
}

function closeLiveStream() {
  state.streaming = false;
  if (state.source) { state.source.close(); state.source = null; }
  if (state.desktopEvents && window.runtime?.EventsOff) {
    window.runtime.EventsOff('gui:ready');
    window.runtime.EventsOff('gui:event');
    state.desktopEvents = false;
  }
  if (hasDesktopBridge() && desktop().Unsubscribe) desktop().Unsubscribe().catch(() => {});
}

function connectEvents(id) {
  state.streaming = false;
  if (hasDesktopBridge() && window.runtime?.EventsOn && desktop().Subscribe) {
    window.runtime.EventsOff?.('gui:ready');
    window.runtime.EventsOff?.('gui:event');
    window.runtime.EventsOn('gui:ready', () => {
      state.streaming = true;
      setStatus('running', 'Agent working…');
    });
    window.runtime.EventsOn('gui:event', (ev) => {
      state.streaming = true;
      appendEvent(ev.event || ev.Event, ev.replay || ev.Replay);
    });
    state.desktopEvents = true;
    desktop().Subscribe(id).catch(() => { state.streaming = false; });
    return;
  }
  if (!window.EventSource) return;
  const es = new EventSource(`/api/sessions/${encodeURIComponent(id)}/events`);
  state.source = es;
  es.addEventListener('ready', () => { state.streaming = true; });
  es.addEventListener('event', (msg) => {
    state.streaming = true;
    appendEvent(JSON.parse(msg.data).event, JSON.parse(msg.data).replay);
  });
  es.addEventListener('error', () => {
    state.streaming = false;
    es.close();
    if (state.source === es) state.source = null;
  });
}

function appendEvent(e, replay) {
  if (replay || !e) return;
  timelineEl.classList.remove('empty');
  const kind = e.kind || e.Kind || 'event';
  // Token accounting: every event may carry incremental token usage.
  const inT = Number(e.in_toks || e.InToks || 0);
  const outT = Number(e.out_toks || e.OutToks || 0);
  if (inT || outT) {
    state.turnTokens.in += inT;
    state.turnTokens.out += outT;
    updateTokenUsage();
  }
  if (kind === 'text') return appendDelta('assistant', e.text || e.Text || '');
  if (kind === 'reasoning') return appendEventBlock('reasoning', 'reasoning', e.text || e.Text || '');
  if (kind === 'tool_start') return ensureToolCard(e);
  if (kind === 'tool_result') return finishToolCard(e);
  if (kind === 'approval') {
    const approval = normalizeApproval({id: e.result || e.Result, tool: e.tool || e.ToolName, args: e.text || e.Text || e.tool_args || e.ToolArgs});
    rememberApproval(approval);
    ensureApprovalCard(approval);
    updateInspector(state.state || {});
    return;
  }
  if (kind === 'done') {
    state.streaming = false;
    setStatus('idle', 'Ready');
    refreshSessions();
    if (state.active) setTimeout(() => refreshActiveState({force: true}), 200);
    return;
  }
  if (kind === 'note') return appendEventBlock('event', 'note', e.text || e.Text || '');
}

function appendDelta(role, text) {
  const pinned = isPinnedToBottom();
  let node = state.rendered.get('streaming');
  if (!node) {
    const made = makeMessageNode('assistant', '');
    made.el.dataset.streaming = '1';
    timelineEl.appendChild(made.el);
    node = made;
    state.rendered.set('streaming', node);
  }
  node.text += text;
  node.bodyEl.innerHTML = renderMarkdown(node.text);
  setStatus('running', 'Agent working…');
  settleScroll(pinned);
}

function appendEventBlock(cls, label, text) {
  const pinned = isPinnedToBottom();
  const el = document.createElement('article');
  el.className = `event ${cls}`;
  const lab = document.createElement('div'); lab.className = 'role'; lab.textContent = label;
  const body = document.createElement('div'); body.className = 'body'; body.innerHTML = cls === 'reasoning' ? escapeHtml(text) : renderMarkdown(text);
  el.appendChild(lab); el.appendChild(body);
  timelineEl.appendChild(el);
  settleScroll(pinned);
}

function ensureToolCard(e) {
  const tool = normalizeToolEvent(e);
  state.tools[tool.id] = {...tool, status: 'running'};
  const existing = timelineEl.querySelector(`[data-tool-card="${cssEscape(tool.id)}"]`);
  if (existing) { updateToolCard(existing, state.tools[tool.id]); return; }
  timelineEl.classList.remove('empty');
  const pinned = isPinnedToBottom();
  const el = document.createElement('article');
  el.className = 'event tool tool-card';
  el.dataset.toolCard = tool.id;
  el.innerHTML = toolCardHTML(state.tools[tool.id]);
  timelineEl.appendChild(el);
  wireToolCard(el);
  settleScroll(pinned);
}
function finishToolCard(e) {
  const tool = normalizeToolEvent(e);
  const prev = state.tools[tool.id] || tool;
  const next = {...prev, ...tool, status: tool.isError ? 'error' : 'done'};
  state.tools[tool.id] = next;
  let el = timelineEl.querySelector(`[data-tool-card="${cssEscape(tool.id)}"]`);
  if (!el) {
    timelineEl.classList.remove('empty');
    const pinned = isPinnedToBottom();
    el = document.createElement('article');
    el.className = 'event tool tool-card';
    el.dataset.toolCard = tool.id;
    timelineEl.appendChild(el);
    updateToolCard(el, next);
    wireToolCard(el);
    settleScroll(pinned);
    return;
  }
  updateToolCard(el, next);
}
function normalizeToolEvent(e) {
  const tool = e.tool || e.ToolName || 'tool';
  const toolID = e.tool_id || e.ToolID || e.id || e.ID || '';
  const step = e.step || e.Step || '';
  const id = toolID || `${step}:${tool}:${Object.keys(state.tools).length}`;
  return {id, tool, step, args: e.tool_args || e.ToolArgs || '', result: e.result || e.Result || '', isError: !!(e.is_error || e.IsError)};
}
function toolCardHTML(tool) {
  const args = pretty(tool.args);
  const result = String(tool.result || '');
  const status = tool.status || 'running';
  const renderedResult = result ? renderToolResult(result, tool.isError) : '';
  return `<div class="tool-card-inner"><div class="tool-card-head"><div><div class="kind">Tool · ${escapeHtml(tool.tool)}</div><div class="tool-id">${escapeHtml(tool.id)}${tool.step ? ` · step ${escapeHtml(tool.step)}` : ''}</div></div><span class="tool-status ${escapeAttr(status)}">${escapeHtml(status)}</span></div>${args ? `<details class="tool-section"><summary>Arguments</summary><pre>${escapeHtml(args)}</pre></details>` : ''}${renderedResult}</div>`;
}
function updateToolCard(el, tool) {
  el.dataset.status = tool.status || 'running';
  if (tool.isError) el.dataset.error = 'true';
  el.innerHTML = toolCardHTML(tool);
  wireToolCard(el);
}
function wireToolCard(el) {
  for (const details of el.querySelectorAll('details')) {
    details.addEventListener('toggle', () => { if (details.open) scrollToVisible(details); });
  }
}
function compactToolResult(text) {
  const s = String(text || '');
  return s.length <= 8000 ? s : `${s.slice(0, 7800)}\n\n… ${s.length - 7800} more characters`;
}
function renderToolResult(result, isError) {
  const label = isError ? 'Error' : isUnifiedDiff(result) ? 'Diff' : 'Result';
  if (isUnifiedDiff(result)) return `<details class="tool-section diff-section" open><summary>${label}</summary><div class="diff-view">${renderUnifiedDiff(result)}</div></details>`;
  return `<details class="tool-section" open><summary>${label}</summary><pre>${escapeHtml(compactToolResult(result))}</pre></details>`;
}
function isUnifiedDiff(text) {
  const s = String(text || '');
  return /(^|\n)diff --git /.test(s) || /(^|\n)@@ [-+0-9, ]+@@/.test(s) || (/^--- /m.test(s) && /^\+\+\+ /m.test(s));
}
function renderUnifiedDiff(text) {
  return compactToolResult(text).split('\n').map(line => {
    let cls = 'ctx';
    if (line.startsWith('diff --git')) cls = 'file';
    else if (line.startsWith('@@')) cls = 'hunk';
    else if (line.startsWith('+++') || line.startsWith('---')) cls = 'meta';
    else if (line.startsWith('+')) cls = 'add';
    else if (line.startsWith('-')) cls = 'del';
    return `<div class="diff-line ${cls}"><span class="diff-prefix">${escapeHtml(line.slice(0, 1) || ' ')}</span><code>${escapeHtml(line)}</code></div>`;
  }).join('');
}
function scrollToVisible(el) {
  const rect = el.getBoundingClientRect();
  const parent = timelineEl.getBoundingClientRect();
  if (rect.bottom > parent.bottom) timelineEl.scrollTop += rect.bottom - parent.bottom + 24;
  if (rect.top < parent.top) timelineEl.scrollTop -= parent.top - rect.top + 24;
}

/* ---------------- Approvals ---------------- */
async function answerApproval(approval, allow) {
  if (!state.active || !approval) return;
  setApprovalStatus(approval, allow ? 'approved' : 'denied');
  await approveCall(state.active, approval, allow);
  updateInspector(state.state || {});
}
function ensureApprovalCard(approval) {
  if (!approval.id || timelineEl.querySelector(`[data-approval-card="${cssEscape(approval.id)}"]`)) return;
  timelineEl.classList.remove('empty');
  const pinned = isPinnedToBottom();
  const el = document.createElement('article');
  el.className = 'event approval approval-card tool-card';
  el.dataset.approvalCard = approval.id;
  el.innerHTML = `<div class="tool-card-inner"><div class="tool-card-head"><div><div class="kind">Approval · ${escapeHtml(approval.tool)}</div></div><span class="tool-status running">pending</span></div><div class="body approval-content">${escapeHtml(formatApprovalArgs(approval))}</div><div class="approval-actions"><button class="primary" type="button" data-approval-id="${escapeAttr(approval.id)}" data-approval-action="allow">Approve</button><button class="ghost" type="button" data-approval-id="${escapeAttr(approval.id)}" data-approval-action="deny">Deny</button></div></div>`;
  timelineEl.appendChild(el);
  settleScroll(pinned);
}
function setApprovalStatus(id, status) {
  if (state.approvals[id]) state.approvals[id].status = status;
  const card = timelineEl.querySelector(`[data-approval-card="${cssEscape(id)}"]`);
  if (!card) return;
  const badge = card.querySelector('.tool-status');
  if (badge) { badge.className = `tool-status ${status === 'approved' ? 'done' : status === 'denied' ? 'error' : 'running'}`; badge.textContent = status; }
  for (const btn of card.querySelectorAll('button[data-action]')) btn.disabled = status !== 'pending';
}
function formatApprovalArgs(approval) {
  const args = String(approval.args || '').trim();
  if (!args) return approval.tool;
  if (args.startsWith(approval.tool + ' ')) return args.slice(approval.tool.length + 1);
  return args;
}
function syncPendingApprovals(pending) {
  const seen = new Set();
  for (const p of pending) {
    const approval = normalizeApproval(p);
    if (!approval.id) continue;
    seen.add(approval.id);
    rememberApproval(approval);
    ensureApprovalCard(approval);
  }
  for (const [id, approval] of Object.entries(state.approvals)) {
    if (approval.status === 'pending' && !seen.has(id)) setApprovalStatus(id, 'resolved');
  }
}
function normalizeApproval(raw) {
  const id = raw.id || raw.ID || raw.result || raw.Result || '';
  const tool = raw.tool || raw.Tool || raw.ToolName || 'tool';
  const args = raw.args || raw.Args || raw.text || raw.Text || raw.tool_args || raw.ToolArgs || '';
  return {id, tool, args: typeof args === 'string' ? args : pretty(args), status: raw.status || 'pending'};
}
function rememberApproval(approval) {
  const prev = state.approvals[approval.id];
  state.approvals[approval.id] = {...approval, status: prev?.status || approval.status || 'pending'};
}

/* ---------------- Inspector ---------------- */
function updateInspector(snap) {
  const pendingApprovals = Object.values(state.approvals).filter(a => a.status === 'pending');
  const roots = snap.roots || snap.Roots || [];
  const shells = snap.shells || snap.Shells || [];
  const tools = snap.tools || snap.Tools || [];
  const tokens = snap.tokens || snap.Tokens || 0;
  const maxTokens = snap.max_tokens || snap.MaxTokens || 0;
  const goal = snap.goal || snap.Goal || '';
  inspectorEl.innerHTML = `
    <div class="inspector-card"><div class="card-label">Context</div>
      <div class="kv"><span>tokens</span><strong>${escapeHtml(tokens)}${maxTokens ? ` / ${escapeHtml(maxTokens)}` : ''}</strong></div>
      <div class="kv"><span>roots</span><strong>${escapeHtml(roots.length || 0)}</strong></div>
      <div class="kv"><span>tools</span><strong>${escapeHtml(tools.length || 0)}</strong></div>
      <div class="kv"><span>shells</span><strong>${escapeHtml(shells.length || 0)}</strong></div>
    </div>
    ${pendingApprovals.length ? `<div class="inspector-card"><div class="card-label">Pending approvals</div>${pendingApprovals.map(approvalSummaryHTML).join('')}</div>` : ''}
    ${shells.length ? `<div class="inspector-card"><div class="card-label">Background shells</div>${shells.slice(0, 6).map(shellSummaryHTML).join('')}</div>` : ''}
    ${tools.length ? `<div class="inspector-card"><div class="card-label">Available tools</div>${tools.slice(0, 10).map(toolSummaryHTML).join('')}${tools.length > 10 ? `<div class="small-copy">+${escapeHtml(tools.length - 10)} more</div>` : ''}</div>` : ''}
    ${goal ? `<div class="inspector-card"><div class="card-label">Goal</div><div class="small-copy">${escapeHtml(goal)}</div></div>` : ''}
    ${roots.length ? `<div class="inspector-card"><div class="card-label">Workspace roots</div>${roots.slice(0, 4).map(r => `<div class="path-row">${escapeHtml(shortPath(r))}</div>`).join('')}</div>` : ''}`;
  for (const button of inspectorEl.querySelectorAll('[data-approval-action]')) {
    button.addEventListener('click', () => answerApproval(button.dataset.approvalId, button.dataset.approvalAction === 'allow'));
  }
}
function approvalSummaryHTML(a) { return `<div class="approval-mini"><div><strong>${escapeHtml(a.tool)}</strong><span>${escapeHtml(a.id)}</span></div><div class="approval-mini-actions"><button class="primary compact" data-approval-id="${escapeAttr(a.id)}" data-approval-action="allow">Approve</button><button class="ghost compact" data-approval-id="${escapeAttr(a.id)}" data-approval-action="deny">Deny</button></div></div>`; }
function shellSummaryHTML(raw) {
  const id = raw.id || raw.ID || '', command = raw.command || raw.Command || '', status = raw.status || raw.Status || 'unknown';
  const exitCode = raw.exit_code ?? raw.ExitCode, lastLine = raw.last_line || raw.LastLine || '';
  return `<div class="shell-mini"><div class="shell-mini-head"><strong>${escapeHtml(id || command || 'shell')}</strong><span class="shell-status ${escapeAttr(status)}">${escapeHtml(status)}${exitCode !== undefined && exitCode !== null ? ` · ${escapeHtml(exitCode)}` : ''}</span></div>${command ? `<div class="shell-command">${escapeHtml(command)}</div>` : ''}${lastLine ? `<div class="shell-last">${escapeHtml(lastLine)}</div>` : ''}</div>`;
}
function toolSummaryHTML(raw) {
  const name = raw.name || raw.Name || '', readOnly = raw.read_only ?? raw.ReadOnly;
  return `<div class="tool-mini"><span>${escapeHtml(name)}</span><strong>${readOnly ? 'read' : 'write'}</strong></div>`;
}

/* ---------------- Feature stage (non-chat surfaces) ---------------- */
function compactActionButton(label, attrs = '') { return `<button class="ghost compact" type="button" ${attrs}>${escapeHtml(label)}</button>`; }

function renderFeatureStage() {
  if (state.feature === 'chat' || !featureStage) return;
  const snap = state.state || {};
  const roots = snap.roots || snap.Roots || [];
  const tools = snap.tools || snap.Tools || [];
  const shells = snap.shells || snap.Shells || [];
  const pending = snap.pending || snap.Pending || [];
  const featureMap = {
    changes: ['Changes', 'Review edits, diffs, and tool output.', [
      ['Latest diffs', 'Unified diffs are highlighted inline with add/delete lanes in the chat timeline.', compactActionButton('Focus latest tool', 'data-feature-action="focus-latest-tool"')],
      ['Tool history', `${Object.keys(state.tools).length} tool calls tracked this session.`, compactActionButton('Back to chat', 'data-feature-action="goto-chat"')],
      ['Workspace roots', roots.length ? roots.map(shortPath).join(' · ') : 'No roots reported.', compactActionButton('Open system', 'data-feature-action="system"')],
    ]],
    tools: ['Tools', 'Inspect and operate the automation surface.', tools.length ? tools.slice(0, 9).map(t => [t.name || t.Name || 'tool', (t.read_only ?? t.ReadOnly) ? 'read-only' : 'mutating', `${compactActionButton('Allow next turn', `data-feature-action="allow-tool" data-tool-name="${escapeAttr(t.name || t.Name || 'tool')}"`)}`]) : [['Tool registry', 'No registry payload yet.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    shells: ['Shells', 'Track and control background commands.', shells.length ? shells.slice(0, 9).map(s => { const status = s.status || s.Status || 'unknown'; const id = s.id || s.ID || ''; const kill = status === 'running' ? ` ${compactActionButton('Kill', `data-feature-action="kill-shell" data-shell-id="${escapeAttr(id)}"`)}` : ''; return [id || s.command || 'shell', `${status} ${s.last_line || s.LastLine || ''}`, `${compactActionButton('Poll', `data-feature-action="poll-shell" data-shell-id="${escapeAttr(id)}"`)}${kill}`]; }) : [['No background shells', 'Background commands appear here with status and controls.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    approvals: ['Approvals', 'Handle gated tool calls deliberately.', pending.length ? pending.map(p => [p.tool || 'approval', p.id || p.ID || 'pending', `${compactActionButton('Approve', `data-feature-action="approve" data-approval-id="${escapeAttr(p.id || p.ID || p.result || '')}"`)} ${compactActionButton('Deny', `data-feature-action="deny" data-approval-id="${escapeAttr(p.id || p.ID || p.result || '')}"`)}`]) : [['No pending approvals', 'Gated operations request approval here.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    memory: ['Memory', 'Keep durable context visible.', [['Goal', snap.goal || snap.Goal || 'No active goal.', `${compactActionButton('Set from composer', 'data-feature-action="set-goal"')} ${compactActionButton('Compact', 'data-feature-action="compact"')}`], ['Roots', roots.length ? roots.map(shortPath).join(' · ') : 'No roots reported.', `${compactActionButton('Add current dir', 'data-feature-action="add-dir"')} ${compactActionButton('Open system', 'data-feature-action="system"')}`], ['Profile', 'Edit USER.md global personalization.', compactActionButton('Edit profile', 'data-feature-action="profile"')]]],
    plugins: ['Plugins', 'Manage extension capability.', [['Available tools', `${tools.length || 0} tools from skills, plugins, and built-ins.`, compactActionButton('Refresh', 'data-feature-action="refresh"')], ['Marketplace', 'Install/update/disable/rollback via the app shell.', compactActionButton('Open system', 'data-feature-action="system"')], ['Agent roles', 'Plugin roles surface through the tool list.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    config: ['Config', 'Tune model, effort, permission, search, fast.', [['Model', modelInput?.value || snap.model || snap.Model || 'default', compactActionButton('Apply model', 'data-feature-action="apply-model"')], ['Permission', permSelect?.value || snap.perm || snap.Perm || 'gated', compactActionButton('Apply perm', 'data-feature-action="apply-perm"')], ['Search / fast', `${searchSelect?.value || snap.search || 'off'} · ${fastToggle?.classList.contains('active') ? 'fast on' : 'fast off'}`, `${compactActionButton('Apply search', 'data-feature-action="apply-search"')} ${compactActionButton('Toggle fast', 'data-feature-action="toggle-fast"')}`]]],
  };
  const [title, copy, cells] = featureMap[state.feature] || featureMap.changes;
  const cellsHTML = cells.map(([k, v, action]) => `<div class="feature-cell"><strong>${escapeHtml(k)}</strong><span>${escapeHtml(v)}</span>${action ? `<div class="feature-actions">${action}</div>` : ''}</div>`).join('');
  featureStage.innerHTML = `<div class="feature-head"><div><div class="feature-title">${escapeHtml(title)}</div><div class="feature-copy">${escapeHtml(copy)}</div></div><button class="ghost compact" type="button" data-feature-close>Back to chat</button></div><div class="feature-grid">${cellsHTML}</div>`;
  featureStage.querySelector('[data-feature-close]')?.addEventListener('click', () => setFeature('chat'));
  featureStage.querySelectorAll('[data-feature-action]').forEach(btn => btn.addEventListener('click', () => runFeatureAction(btn)));
}

async function runFeatureAction(btn) {
  const action = btn.dataset.featureAction;
  if (action === 'refresh') return refreshActiveState({force: true});
  if (action === 'system') return openSystemModal();
  if (action === 'profile') return openProfileModal();
  if (action === 'goto-config') return setFeature('config');
  if (action === 'goto-chat') return setFeature('chat');
  if (action === 'focus-latest-tool') {
    setFeature('chat');
    setTimeout(() => { const last = timelineEl.querySelector('[data-tool-card]:last-of-type'); if (last) { scrollToVisible(last); last.classList.add('focused-tool'); setTimeout(() => last.classList.remove('focused-tool'), 1200); } }, 0);
    return;
  }
  if (!state.active) return;
  if (action === 'approve' || action === 'deny') return answerApproval(btn.dataset.approvalId, action === 'approve');
  if (action === 'allow-tool') return sendAllowedToolTurn(btn.dataset.toolName || '');
  if (action === 'kill-shell') { await killShell(state.active, btn.dataset.shellId || ''); return refreshActiveState({force: true}); }
  if (action === 'detach-bash') { await detachBash(state.active); return refreshActiveState({force: true}); }
  if (action === 'compact') {
    try {
      const res = await compactSession(state.active, 0);
      const before = res?.before ?? res?.Before, after = res?.after ?? res?.After;
      if (before !== undefined && after !== undefined) flashStatus(`Compacted ${before} → ${after} messages`);
    } catch (err) { flashStatus(`Compact failed: ${err.message}`, true); }
    return refreshActiveState({force: true});
  }
  if (action === 'set-goal') return applySettingFromFeature('goal', inputEl?.value?.trim() || '');
  if (action === 'add-dir') { await addDir(state.active, '.'); return refreshActiveState({force: true}); }
  if (action === 'apply-model') return applySettingFromFeature('model', modelInput?.value?.trim());
  if (action === 'apply-perm') return applySettingFromFeature('perm', permSelect?.value || 'gated');
  if (action === 'apply-search') return applySettingFromFeature('search', searchSelect?.value || 'off');
  if (action === 'toggle-fast') { return applySettingFromFeature('fast', !(state.state?.fast || state.state?.Fast)); }
  if (action === 'poll-shell') return refreshActiveState({force: true});
}
async function applySettingFromFeature(setting, value) {
  if (!state.active) return;
  await sessionSetting(state.active, setting, value);
  await refreshActiveState({force: true});
}
async function sendAllowedToolTurn(toolName) {
  if (!state.active || !toolName) return;
  const text = inputEl?.value?.trim() || `Use ${toolName} for the current task.`;
  appendLocalMessage('user', text);
  scrollToBottom();
  inputEl.value = ''; autoGrowInput(); updateSendAvailability();
  await sendInput(state.active, text, [toolName]);
}
function appendLocalMessage(role, text) {
  // Immediately show the user's message before the daemon round-trips.
  timelineEl.classList.remove('empty');
  const node = makeMessageNode(role, text);
  timelineEl.appendChild(node.el);
}

/* ---------------- Composer / send ---------------- */
$('composer').onsubmit = async (e) => {
  e.preventDefault();
  if (!state.active) return;
  const running = !!(state.state?.running || state.state?.Running);
  if (running && !inputEl.value.trim()) { await sessionAction(state.active, 'interrupt'); return; }
  const text = inputEl.value.trim();
  if (!text) return;
  appendLocalMessage('user', text);
  scrollToBottom();
  inputEl.value = '';
  autoGrowInput();
  updateSendAvailability();
  state.turnTokens = {in: 0, out: 0};
  updateTokenUsage();
  setStatus('running', 'Sending…');
  try {
    await sendInput(state.active, text);
    // Nudge the stream/state immediately so the UI feels responsive.
    setTimeout(() => refreshActiveState({force: true}), 150);
  } catch (err) {
    setStatus('error', `Send failed: ${err.message}`);
  }
};

jumpLatestEl.onclick = () => scrollToBottom();
// Delegated approval actions so re-rendered cards keep working without re-wiring.
timelineEl.addEventListener('click', (e) => {
  const btn = e.target?.closest?.('[data-approval-action]');
  if (!btn) return;
  answerApproval(btn.dataset.approvalId, btn.dataset.approvalAction === 'allow');
});
timelineEl.addEventListener('scroll', () => {
  state.userPinnedBottom = isPinnedToBottom();
  jumpLatestEl.classList.toggle('hidden', state.userPinnedBottom);
});
inputEl.addEventListener('input', () => { autoGrowInput(); updateSendAvailability(); });
inputEl.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); $('composer').requestSubmit(); } });

function updateComposerState(running) {
  const hasSession = !!state.active;
  inputEl.disabled = !hasSession;
  inputEl.placeholder = !hasSession ? 'Create or select a session to begin…' : running ? 'Steer the running turn, or press Stop with empty input…' : 'Ask Eigen to build, inspect, fix, or explain…';
  interruptBtn.classList.toggle('hidden', !running);
  updateSendAvailability();
}
function updateSendAvailability() {
  const running = !!(state.state?.running || state.state?.Running);
  sendEl.disabled = !state.active || (!running && !inputEl.value.trim());
  sendEl.textContent = running && !inputEl.value.trim() ? 'Stop' : 'Send';
  sendEl.classList.toggle('stop', running && !inputEl.value.trim());
}
function autoGrowInput() { inputEl.style.height = '0px'; inputEl.style.height = Math.min(inputEl.scrollHeight, 180) + 'px'; }
function isPinnedToBottom() { return timelineEl.scrollHeight - timelineEl.scrollTop - timelineEl.clientHeight < 80; }
function scrollToBottom() { timelineEl.scrollTop = timelineEl.scrollHeight; state.userPinnedBottom = true; jumpLatestEl?.classList.add('hidden'); }
function settleScroll(wasPinned) { if (wasPinned || state.userPinnedBottom) scrollToBottom(); else jumpLatestEl?.classList.remove('hidden'); }

/* ---------------- Controls wiring ---------------- */
$('new-session').onclick = () => openNewSessionModal();
featureNav?.addEventListener('click', (e) => { const btn = e.target?.closest?.('[data-feature]'); if (btn) setFeature(btn.dataset.feature); });
$('rail-toggle')?.addEventListener('click', () => document.body.classList.toggle('rail-collapsed'));
document.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') { e.preventDefault(); document.body.classList.toggle('rail-collapsed'); return; }
  if (e.key !== 'Escape') return;
  if (!newSessionModal.classList.contains('hidden')) closeNewSessionModal();
  if (!profileModal.classList.contains('hidden')) closeProfileModal();
  if (!systemModal.classList.contains('hidden')) closeSystemModal();
}, true);

// Copy buttons (delegated, for code blocks).
document.addEventListener('click', (e) => {
  const btn = e.target?.closest?.('[data-copy-for]');
  if (!btn) return;
  const pre = document.getElementById(btn.dataset.copyFor);
  if (!pre) return;
  const text = pre.textContent;
  const done = () => { const t = btn.textContent; btn.textContent = 'copied'; setTimeout(() => { btn.textContent = t; }, 1200); };
  if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text).then(done).catch(() => {});
  else done();
});

$('interrupt').onclick = async () => { if (state.active) { await sessionAction(state.active, 'interrupt'); setStatus('idle', 'Interrupting…'); } };
$('resend').onclick = async () => { if (state.active) await sessionAction(state.active, 'resend'); };
$('clear').onclick = async () => { if (!state.active) return; if (!confirm('Clear this session transcript?')) return; await sessionAction(state.active, 'clear'); state.rendered.clear(); timelineEl.innerHTML = ''; await refreshActiveState({force: true}); };
$('rename-session').onclick = async () => {
  if (!state.active) return;
  const cur = titleEl.textContent || '';
  const name = prompt('Rename session', cur);
  if (name === null || name.trim() === cur) return;
  try { await renameSession(state.active, name.trim()); await refreshSessions(); await refreshActiveState({force: true}); }
  catch (err) { alert(`Rename failed: ${err.message}`); }
};
$('delete-session').onclick = async () => {
  if (!state.active) return;
  if (!confirm('Delete this session? This cannot be undone.')) return;
  const id = state.active;
  state.active = null;
  try { await deleteSession(id); state.rendered.clear(); timelineEl.innerHTML = ''; await refreshSessions(); if (state.sessions.length) openSession(sessionID(state.sessions[0])); else applyEmptyState(); }
  catch (err) { state.active = id; alert(`Delete failed: ${err.message}`); }
};
$('goal-edit').onclick = async () => {
  if (!state.active) return;
  const cur = (state.state?.goal || state.state?.Goal || '').trim();
  const g = prompt('Set the session goal', cur);
  if (g === null) return;
  await sessionSetting(state.active, 'goal', g.trim());
  await refreshActiveState({force: true});
};
$('goal-clear').onclick = async () => {
  if (!state.active) return;
  await sessionSetting(state.active, 'goal', '');
  await refreshActiveState({force: true});
};
function applyEmptyState() {
  titleEl.textContent = 'Select a session';
  metaEl.textContent = 'Live daemon workspace';
  timelineEl.innerHTML = `<div class="empty-state"><div class="empty-title">No session.</div><div class="empty-copy">Create a new session to begin.</div></div>`;
  goalBar.classList.add('hidden');
  tokenUsage.classList.add('hidden');
  updateComposerState(false);
}
modelInput.addEventListener('change', async () => { if (!state.active) return; const v = modelInput.value.trim(); if (v) { await sessionSetting(state.active, 'model', v); await refreshActiveState({force: true}); } });
modelInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); modelInput.blur(); } });
effortSelect.onchange = async () => { if (state.active) { await sessionSetting(state.active, 'effort', effortSelect.value); await refreshActiveState({force: true}); } };
permSelect.onchange = async () => { if (state.active) { await sessionSetting(state.active, 'perm', permSelect.value); await refreshActiveState({force: true}); } };
searchSelect.onchange = async () => { if (state.active) { await sessionSetting(state.active, 'search', searchSelect.value); await refreshActiveState({force: true}); } };
fastToggle.onclick = async () => { if (!state.active) return; await sessionSetting(state.active, 'fast', !(state.state?.fast || state.state?.Fast)); await refreshActiveState({force: true}); };

/* ---------------- Modals ---------------- */
newSessionClose.onclick = () => closeNewSessionModal();
newSessionCancel.onclick = () => closeNewSessionModal();
newSessionModal.addEventListener('click', (e) => { if (e.target === newSessionModal) closeNewSessionModal(); });
newSessionForm.onsubmit = async (e) => {
  e.preventDefault();
  setModalError('');
  const submit = newSessionForm.querySelector('[type="submit"]');
  submit.disabled = true; submit.textContent = 'Creating…';
  try {
    const out = await createSession({dir: sessionDirInput.value.trim(), model: sessionModelInput.value.trim(), perm: sessionPermInput.value});
    closeNewSessionModal();
    await refreshSessions();
    await openSession(out.id || out.ID || out);
  } catch (err) { setModalError(err.message || String(err)); }
  finally { submit.disabled = false; submit.textContent = 'Create session'; }
};
profileButton.onclick = () => openProfileModal();
systemButton.onclick = () => openSystemModal();
systemClose.onclick = () => closeSystemModal();
systemCancel.onclick = () => closeSystemModal();
systemRefresh.onclick = () => renderSystemHealth();
systemModal.addEventListener('click', (e) => { if (e.target === systemModal) closeSystemModal(); });
profileClose.onclick = () => closeProfileModal();
profileCancel.onclick = () => closeProfileModal();
profileModal.addEventListener('click', (e) => { if (e.target === profileModal) closeProfileModal(); });
profileClear.onclick = () => { profileText.value = ''; profileText.focus(); };
profileForm.onsubmit = async (e) => {
  e.preventDefault();
  setProfileError('');
  const submit = profileForm.querySelector('[type="submit"]');
  submit.disabled = true; submit.textContent = 'Saving…';
  try { await saveUserProfile(profileText.value); closeProfileModal(); }
  catch (err) { setProfileError(err.message || String(err)); }
  finally { submit.disabled = false; submit.textContent = 'Save profile'; }
};
window.openNewSessionModal = openNewSessionModal;
window.openProfileModal = openProfileModal;
function openNewSessionModal() { setModalError(''); sessionDirInput.value = ''; sessionModelInput.value = ''; sessionPermInput.value = 'gated'; newSessionModal.classList.remove('hidden'); setTimeout(() => sessionDirInput.focus(), 0); }
function closeNewSessionModal() { newSessionModal.classList.add('hidden'); }
function setModalError(m) { newSessionError.textContent = m; newSessionError.classList.toggle('hidden', !m); }
async function openProfileModal() { setProfileError(''); profileText.value = 'Loading…'; profileModal.classList.remove('hidden'); try { profileText.value = await getUserProfile(); profileText.focus(); } catch (err) { profileText.value = ''; setProfileError(err.message || String(err)); } }
function closeProfileModal() { profileModal.classList.add('hidden'); }
function setProfileError(m) { profileError.textContent = m; profileError.classList.toggle('hidden', !m); }
async function openSystemModal() { systemModal.classList.remove('hidden'); await renderSystemHealth(); }
function closeSystemModal() { systemModal.classList.add('hidden'); }
async function renderSystemHealth() {
  systemBody.innerHTML = '<div class="modal-copy">Loading daemon health…</div>';
  try {
    const h = await getHealth();
    const st = h.stats || h.Stats || {};
    const rows = [['daemon', h.ok ? 'connected' : 'offline'], ['socket', h.socket || h.Socket || ''], ['version', st.version || st.Version || ''], ['uptime', formatDuration(st.uptime_sec || st.UptimeSec || 0)], ['sessions', st.sessions ?? st.Sessions ?? 0], ['running turns', st.running_turns ?? st.RunningTurns ?? 0], ['goroutines', st.goroutines ?? st.Goroutines ?? 0], ['heap', formatBytes(st.heap_alloc_b || st.HeapAllocB || 0)], ['rss', formatBytes(st.rss_b || st.RSSB || 0)]];
    systemBody.innerHTML = rows.filter(([, v]) => v !== '').map(([k, v]) => `<div class="system-row"><span>${escapeHtml(k)}</span><strong>${escapeHtml(v)}</strong></div>`).join('') + (h.error || h.Error ? `<div class="modal-error">${escapeHtml(h.error || h.Error)}</div>` : '');
  } catch (err) { systemBody.innerHTML = `<div class="modal-error">${escapeHtml(err.message || String(err))}</div>`; }
}

/* ---------------- Helpers ---------------- */
function formatBytes(n) { n = Number(n || 0); if (n < 1024) return `${n} B`; const u = ['KiB', 'MiB', 'GiB', 'TiB']; let i = -1; do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1); return `${n.toFixed(n >= 10 ? 1 : 2)} ${u[i]}`; }
function formatDuration(sec) { sec = Number(sec || 0); const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = Math.floor(sec % 60); if (h) return `${h}h ${m}m`; if (m) return `${m}m ${s}s`; return `${s}s`; }
function sessionID(s) { return s.id || s.ID; }
function sessionStatus(s) { return s.status || s.Status; }
function messagesSignature(messages) { if (!messages.length) return '0'; const last = messages[messages.length - 1]; return `${messages.length}:${last.role || last.Role}:${(last.text || last.Text || '').length}`; }
function shortPath(p) { const parts = p.split('/').filter(Boolean); return parts.length > 3 ? `~/${parts.slice(-3).join('/')}` : p; }
function pretty(v) { if (!v) return ''; try { return JSON.stringify(typeof v === 'string' ? JSON.parse(v) : v, null, 2); } catch { return String(v); } }
function escapeHtml(s) { return String(s).replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c])); }
function escapeAttr(s) { return escapeHtml(s).replace(/`/g, '&#96;'); }
function cssEscape(s) { if (window.CSS?.escape) return window.CSS.escape(String(s)); return String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&'); }

updateComposerState(false);
autoGrowInput();
boot();
