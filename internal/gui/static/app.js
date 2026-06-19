const state = {
  sessions: [],
  active: null,
  source: null,
  state: null,
  poll: null,
  streaming: false,
};

const $ = (id) => document.getElementById(id);
const sessionsEl = $('sessions');
const timelineEl = $('timeline');
const titleEl = $('title');
const metaEl = $('meta');
const daemonEl = $('daemon');
const inspectorEl = $('inspector');
const inputEl = $('input');

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

async function boot() {
  await refreshHealth();
  await refreshSessions();
  setInterval(refreshSessions, 3500);
}

async function refreshHealth() {
  try {
    const h = await api('/api/health');
    daemonEl.textContent = h.ok ? 'daemon connected' : 'daemon offline';
  } catch (err) {
    daemonEl.textContent = 'daemon error';
  }
}

async function refreshSessions() {
  try {
    state.sessions = await api('/api/sessions');
    renderSessions();
    if (!state.active && state.sessions.length) openSession(state.sessions[0].id);
  } catch (err) {
    sessionsEl.innerHTML = `<div class="session"><div class="session-title">${escapeHtml(err.message)}</div></div>`;
  }
}

function renderSessions() {
  sessionsEl.innerHTML = '';
  for (const s of state.sessions) {
    const row = document.createElement('button');
    row.className = `session ${state.active === s.id ? 'active' : ''}`;
    row.innerHTML = `
      <div class="session-title">${escapeHtml(s.title || s.id)}</div>
      <div class="badge ${s.status === 'error' ? 'error' : ''}">${escapeHtml(s.status || 'idle')}</div>
      <div class="session-dir">${escapeHtml(shortPath(s.dir || ''))}</div>
    `;
    row.onclick = () => openSession(s.id);
    sessionsEl.appendChild(row);
  }
}

async function openSession(id) {
  state.active = id;
  renderSessions();
  if (state.source) state.source.close();
  if (state.poll) clearInterval(state.poll);
  const snap = await api(`/api/sessions/${encodeURIComponent(id)}/state`);
  applyState(id, snap, {force: true});
  connectEvents(id);
  state.poll = setInterval(() => refreshActiveState({force: !state.streaming}), 2200);
}

async function refreshActiveState(opts = {}) {
  if (!state.active) return;
  try {
    const snap = await api(`/api/sessions/${encodeURIComponent(state.active)}/state`);
    applyState(state.active, snap, opts);
  } catch (err) {
    inspectorEl.textContent = `State refresh failed: ${err.message}`;
  }
}

function applyState(id, snap, opts = {}) {
  const before = state.state;
  state.state = snap;
  titleEl.textContent = snap.title || id;
  metaEl.textContent = `${snap.provider || 'provider'} · ${snap.model || 'model'} · perm=${snap.perm || 'gated'}${snap.running ? ' · running' : ''}`;
  const messages = snap.messages || [];
  if (opts.force || messagesSignature(messages) !== messagesSignature(before?.messages || [])) {
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

function connectEvents(id) {
  state.streaming = false;
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
  if (replay) return;
  timelineEl.classList.remove('empty');
  const kind = e.kind || 'event';
  if (kind === 'text') return appendDelta('assistant', e.text || '');
  if (kind === 'reasoning') return appendEventBlock('reasoning', 'reasoning', e.text || '');
  if (kind === 'tool_start') return appendEventBlock('tool', `tool · ${e.tool || ''}`, pretty(e.tool_args));
  if (kind === 'tool_result') return appendEventBlock('tool', `result · ${e.tool || ''}`, e.result || '');
  if (kind === 'approval') {
    appendEventBlock('approval', `approval · ${e.tool || ''}`, e.text || '');
    inspectorEl.innerHTML = `
      <div class="approval">
        <div class="panel-title">Approval requested</div>
        <p>${escapeHtml(e.text || e.tool || 'tool call')}</p>
        <button class="primary" onclick="answerApproval('${escapeAttr(e.result)}', true)">Approve</button>
        <button class="ghost" onclick="answerApproval('${escapeAttr(e.result)}', false)">Deny</button>
      </div>`;
    return;
  }
  if (kind === 'done') {
    refreshSessions();
    if (state.active) setTimeout(() => openSession(state.active), 250);
    return;
  }
  if (kind === 'note') return appendEventBlock('event', 'note', e.text || '');
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
  await api(`/api/sessions/${encodeURIComponent(state.active)}/approve`, {
    method: 'POST',
    body: JSON.stringify({approval, allow}),
  });
  inspectorEl.textContent = allow ? 'Approved.' : 'Denied.';
};

$('new-session').onclick = async () => {
  const out = await api('/api/sessions', {method: 'POST', body: JSON.stringify({})});
  await refreshSessions();
  await openSession(out.id);
};

$('interrupt').onclick = async () => {
  if (!state.active) return;
  await api(`/api/sessions/${encodeURIComponent(state.active)}/interrupt`, {method: 'POST', body: '{}'});
};

$('resend').onclick = async () => {
  if (!state.active) return;
  await api(`/api/sessions/${encodeURIComponent(state.active)}/resend`, {method: 'POST', body: '{}'});
};

$('composer').onsubmit = async (e) => {
  e.preventDefault();
  const text = inputEl.value.trim();
  if (!text || !state.active) return;
  appendMessage('user', text);
  inputEl.value = '';
  await api(`/api/sessions/${encodeURIComponent(state.active)}/input`, {
    method: 'POST',
    body: JSON.stringify({text}),
  });
};

inputEl.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    $('composer').requestSubmit();
  }
});

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
