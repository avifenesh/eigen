const state = {
  sessions: [],
  active: null,
  source: null,
  state: null,
  poll: null,
  streaming: false,
  desktopEvents: false,
};

const $ = (id) => document.getElementById(id);
const sessionsEl = $('sessions');
const timelineEl = $('timeline');
const titleEl = $('title');
const metaEl = $('meta');
const daemonEl = $('daemon');
const inspectorEl = $('inspector');
const inputEl = $('input');

function desktop() {
  if (!window.go) return null;
  if (window.go.gui?.DesktopApp) return window.go.gui.DesktopApp;
  for (const pkg of Object.values(window.go)) {
    if (pkg?.DesktopApp) return pkg.DesktopApp;
  }
  return null;
}

function hasDesktopBridge() {
  return !!desktop();
}

async function waitForDesktopBridge() {
  for (let i = 0; i < 20; i++) {
    if (hasDesktopBridge()) return true;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  return false;
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: {'content-type': 'application/json'},
    ...opts,
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) throw new Error(data?.error || res.statusText);
  return data;
}

async function getHealth() {
  if (hasDesktopBridge()) return desktop().Health();
  return api('/api/health');
}

async function getSessions() {
  if (hasDesktopBridge()) return desktop().Sessions();
  return api('/api/sessions');
}

async function createSession() {
  if (hasDesktopBridge()) return {id: await desktop().NewSession('', '', '')};
  return api('/api/sessions', {method: 'POST', body: JSON.stringify({})});
}

async function getState(id) {
  if (hasDesktopBridge()) return desktop().State(id);
  return api(`/api/sessions/${encodeURIComponent(id)}/state`);
}

async function sendInput(id, text) {
  if (hasDesktopBridge()) return {steered: await desktop().Input(id, text)};
  return api(`/api/sessions/${encodeURIComponent(id)}/input`, {
    method: 'POST',
    body: JSON.stringify({text}),
  });
}

async function approveCall(id, approval, allow) {
  if (hasDesktopBridge()) return desktop().Approve(id, approval, allow);
  return api(`/api/sessions/${encodeURIComponent(id)}/approve`, {
    method: 'POST',
    body: JSON.stringify({approval, allow}),
  });
}

async function sessionAction(id, action) {
  if (hasDesktopBridge()) {
    if (action === 'interrupt') return desktop().Interrupt(id);
    if (action === 'resend') return desktop().Resend(id);
    if (action === 'clear') return desktop().Clear(id);
  }
  return api(`/api/sessions/${encodeURIComponent(id)}/${action}`, {method: 'POST', body: '{}'});
}

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
  } catch (err) {
    daemonEl.textContent = 'daemon error';
  }
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
      <div class="session-dir">${escapeHtml(shortPath(s.dir || s.Dir || ''))}</div>
    `;
    row.onclick = () => openSession(id);
    sessionsEl.appendChild(row);
  }
}

async function openSession(id) {
  state.active = id;
  renderSessions();
  closeLiveStream();
  if (state.poll) clearInterval(state.poll);
  const snap = await getState(id);
  applyState(id, snap, {force: true});
  connectEvents(id);
  state.poll = setInterval(() => refreshActiveState({force: !state.streaming}), 2200);
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
  const provider = snap.provider || snap.Provider || 'provider';
  const model = snap.model || snap.Model || 'model';
  const perm = snap.perm || snap.Perm || 'gated';
  const running = snap.running || snap.Running;
  metaEl.textContent = `${provider} · ${model} · perm=${perm}${running ? ' · running' : ''}`;
  const messages = snap.messages || snap.Messages || [];
  const beforeMessages = before?.messages || before?.Messages || [];
  if (opts.force || messagesSignature(messages) !== messagesSignature(beforeMessages)) {
    renderTimeline(messages);
  }
}

function renderTimeline(messages) {
  timelineEl.classList.toggle('empty', messages.length === 0);
  timelineEl.innerHTML = '';
  if (messages.length === 0) {
    timelineEl.innerHTML = `
      <div class="empty-state">
        <div class="empty-title">Ready for work.</div>
        <div class="empty-copy">Send a message. Tool calls and approvals will stream into this workspace.</div>
      </div>`;
    return;
  }
  for (const m of messages) appendMessage(m.role || m.Role || 'message', m.text || m.Text || '');
  timelineEl.scrollTop = timelineEl.scrollHeight;
}

function closeLiveStream() {
  state.streaming = false;
  if (state.source) {
    state.source.close();
    state.source = null;
  }
  if (state.desktopEvents && window.runtime?.EventsOff) {
    window.runtime.EventsOff('gui:ready');
    window.runtime.EventsOff('gui:event');
    state.desktopEvents = false;
  }
  if (hasDesktopBridge() && desktop().Unsubscribe) {
    desktop().Unsubscribe().catch(() => {});
  }
}

function connectEvents(id) {
  state.streaming = false;
  if (hasDesktopBridge() && window.runtime?.EventsOn && desktop().Subscribe) {
    window.runtime.EventsOff?.('gui:ready');
    window.runtime.EventsOff?.('gui:event');
    window.runtime.EventsOn('gui:ready', () => {
      state.streaming = true;
      inspectorEl.textContent = 'Desktop event stream connected.';
    });
    window.runtime.EventsOn('gui:event', (ev) => {
      state.streaming = true;
      appendEvent(ev.event || ev.Event, ev.replay || ev.Replay);
    });
    state.desktopEvents = true;
    desktop().Subscribe(id).catch((err) => {
      state.streaming = false;
      inspectorEl.textContent = `Desktop stream unavailable: ${err}. Using state polling.`;
    });
    return;
  }
  if (!window.EventSource) {
    inspectorEl.textContent = 'Live stream unavailable. Using state polling.';
    return;
  }
  const es = new EventSource(`/api/sessions/${encodeURIComponent(id)}/events`);
  state.source = es;
  es.addEventListener('ready', () => {
    state.streaming = true;
    inspectorEl.textContent = 'Live event stream connected.';
  });
  es.addEventListener('event', (msg) => {
    state.streaming = true;
    const ev = JSON.parse(msg.data);
    appendEvent(ev.event, ev.replay);
  });
  es.addEventListener('error', () => {
    state.streaming = false;
    es.close();
    if (state.source === es) state.source = null;
    inspectorEl.textContent = 'Live stream unavailable in this shell. Using state polling.';
  });
}

function appendMessage(role, text) {
  const el = document.createElement('article');
  el.className = `message ${role}`;
  el.innerHTML = `<div class="role">${escapeHtml(role)}</div><div class="content">${escapeHtml(text)}</div>`;
  timelineEl.appendChild(el);
}

function appendEvent(e, replay) {
  if (replay || !e) return;
  timelineEl.classList.remove('empty');
  const kind = e.kind || e.Kind || 'event';
  if (kind === 'text') return appendDelta('assistant', e.text || e.Text || '');
  if (kind === 'reasoning') return appendEventBlock('reasoning', 'reasoning', e.text || e.Text || '');
  if (kind === 'tool_start') return appendEventBlock('tool', `tool · ${e.tool || e.ToolName || ''}`, pretty(e.tool_args || e.ToolArgs));
  if (kind === 'tool_result') return appendEventBlock('tool', `result · ${e.tool || e.ToolName || ''}`, e.result || e.Result || '');
  if (kind === 'approval') {
    appendEventBlock('approval', `approval · ${e.tool || e.ToolName || ''}`, e.text || e.Text || '');
    inspectorEl.innerHTML = `
      <div class="approval">
        <div class="panel-title">Approval requested</div>
        <p>${escapeHtml(e.text || e.Text || e.tool || e.ToolName || 'tool call')}</p>
        <button class="primary" onclick="answerApproval('${escapeAttr(e.result || e.Result)}', true)">Approve</button>
        <button class="ghost" onclick="answerApproval('${escapeAttr(e.result || e.Result)}', false)">Deny</button>
      </div>`;
    return;
  }
  if (kind === 'done') {
    refreshSessions();
    if (state.active) setTimeout(() => refreshActiveState({force: true}), 250);
    return;
  }
  if (kind === 'note') return appendEventBlock('event', 'note', e.text || e.Text || '');
}

function appendDelta(role, text) {
  let last = timelineEl.lastElementChild;
  if (!last || !last.classList.contains('assistant') || last.dataset.streaming !== '1') {
    last = document.createElement('article');
    last.className = 'message assistant';
    last.dataset.streaming = '1';
    last.innerHTML = `<div class="role">assistant</div><div class="content"></div>`;
    timelineEl.appendChild(last);
  }
  last.querySelector('.content').textContent += text;
  timelineEl.scrollTop = timelineEl.scrollHeight;
}

function appendEventBlock(cls, label, text) {
  const el = document.createElement('article');
  el.className = `event ${cls}`;
  el.innerHTML = `<div class="kind">${escapeHtml(label)}</div><div class="content">${escapeHtml(text)}</div>`;
  timelineEl.appendChild(el);
  timelineEl.scrollTop = timelineEl.scrollHeight;
}

window.answerApproval = async (approval, allow) => {
  if (!state.active) return;
  await approveCall(state.active, approval, allow);
  inspectorEl.textContent = allow ? 'Approved.' : 'Denied.';
};

$('new-session').onclick = async () => {
  const out = await createSession();
  await refreshSessions();
  await openSession(out.id || out.ID || out);
};

$('interrupt').onclick = async () => {
  if (!state.active) return;
  await sessionAction(state.active, 'interrupt');
};

$('resend').onclick = async () => {
  if (!state.active) return;
  await sessionAction(state.active, 'resend');
};

$('composer').onsubmit = async (e) => {
  e.preventDefault();
  const text = inputEl.value.trim();
  if (!text || !state.active) return;
  appendMessage('user', text);
  inputEl.value = '';
  await sendInput(state.active, text);
};

inputEl.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    $('composer').requestSubmit();
  }
});

function sessionID(s) { return s.id || s.ID; }
function sessionStatus(s) { return s.status || s.Status; }
function messagesSignature(messages) {
  if (!messages.length) return '0';
  const last = messages[messages.length - 1];
  return `${messages.length}:${last.role || last.Role}:${(last.text || last.Text || '').length}`;
}
function shortPath(p) {
  const home = '~';
  const parts = p.split('/').filter(Boolean);
  if (parts.length > 3) return `${home}/${parts.slice(-3).join('/')}`;
  return p;
}
function pretty(v) {
  if (!v) return '';
  try { return JSON.stringify(typeof v === 'string' ? JSON.parse(v) : v, null, 2); }
  catch { return String(v); }
}
function escapeHtml(s) { return String(s).replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c])); }
function escapeAttr(s) { return escapeHtml(s).replace(/`/g, '&#96;'); }

boot();
