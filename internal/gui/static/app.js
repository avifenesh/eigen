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
const featureWorkspace = $('feature-workspace');
const desktopOverview = $('desktop-overview');
const overviewSurface = $('overview-surface');
const overviewContext = $('overview-context');
const overviewTokens = $('overview-tokens');
const overviewTools = $('overview-tools');
const overviewShells = $('overview-shells');

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

async function getUserProfile() {
  if (hasDesktopBridge()) return desktop().UserProfile();
  const out = await api('/api/profile');
  return out.profile || '';
}

async function saveUserProfile(profile) {
  if (hasDesktopBridge()) return desktop().WriteUserProfile(profile);
  return api('/api/profile', {method: 'POST', body: JSON.stringify({profile})});
}

async function createSession(opts = {}) {
  const dir = opts.dir || '';
  const model = opts.model || '';
  const perm = opts.perm || '';
  if (hasDesktopBridge()) return {id: await desktop().NewSession(dir, model, perm)};
  return api('/api/sessions', {method: 'POST', body: JSON.stringify({dir, model, perm})});
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

async function sessionSetting(id, setting, value) {
  if (hasDesktopBridge()) {
    if (setting === 'model') return desktop().SetModel(id, value);
    if (setting === 'effort') return desktop().SetEffort(id, value);
    if (setting === 'perm') return desktop().SetPerm(id, value);
    if (setting === 'search') return desktop().SetSearch(id, value);
    if (setting === 'fast') return desktop().SetFast(id, !!value);
  }
  return api(`/api/sessions/${encodeURIComponent(id)}/${setting}`, {
    method: 'POST',
    body: JSON.stringify({value}),
  });
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
  updateComposerState(running);
  const messages = snap.messages || snap.Messages || [];
  const beforeMessages = before?.messages || before?.Messages || [];
  updateControls(snap);
  if (opts.force || messagesSignature(messages) !== messagesSignature(beforeMessages)) {
    renderTimeline(messages);
  }
  syncPendingApprovals(snap.pending || snap.Pending || []);
  updateInspector(snap);
  updateDesktopOverview(snap);
  renderFeatureWorkspace();
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
  effortControl?.classList.toggle('hidden', !effort && !state.active);
  if (permSelect) permSelect.value = perm === 'auto' ? 'auto' : 'gated';
  if (searchSelect) searchSelect.value = search || 'off';
  searchControl?.classList.toggle('hidden', !search);
  fastToggle?.classList.toggle('hidden', !fastOK);
  fastToggle?.classList.toggle('active', fast);
  if (fastToggle) fastToggle.textContent = fast ? 'Fast on' : 'Fast';
}

function updateDesktopOverview(snap = {}) {
  if (!overviewSurface) return;
  const tokens = snap.tokens || snap.Tokens || 0;
  const maxTokens = snap.max_tokens || snap.MaxTokens || 0;
  const tools = snap.tools || snap.Tools || [];
  const shells = snap.shells || snap.Shells || [];
  overviewSurface.textContent = state.feature;
  overviewContext.textContent = state.active ? `${titleEl.textContent || state.active} · ${metaEl.textContent || 'session'}` : 'Select a session to inspect live context.';
  overviewTokens.textContent = maxTokens ? `${tokens}/${maxTokens}` : String(tokens || 0);
  overviewTools.textContent = String(tools.length || 0);
  overviewShells.textContent = String(shells.length || 0);
}

function setFeature(feature) {
  state.feature = feature || 'chat';
  for (const btn of featureNav?.querySelectorAll('[data-feature]') || []) {
    btn.classList.toggle('active', btn.dataset.feature === state.feature);
  }
  updateDesktopOverview(state.state || {});
  renderFeatureWorkspace();
}

function compactActionButton(label, attrs = '') {
  return `<button class="ghost compact" type="button" ${attrs}>${escapeHtml(label)}</button>`;
}

function renderFeatureWorkspace() {
  if (!featureWorkspace) return;
  const snap = state.state || {};
  if (state.feature === 'chat') {
    featureWorkspace.classList.add('hidden');
    timelineEl.classList.remove('hidden');
    return;
  }
  timelineEl.classList.add('hidden');
  featureWorkspace.classList.remove('hidden');
  const roots = snap.roots || snap.Roots || [];
  const tools = snap.tools || snap.Tools || [];
  const shells = snap.shells || snap.Shells || [];
  const pending = snap.pending || snap.Pending || [];
  const featureMap = {
    changes: ['Changes', 'Review edits, diffs, and tool output before they disappear into the transcript.', [
      ['Diff rendering', 'Unified diffs are highlighted inline with add/delete/hunk lanes.', compactActionButton('Refresh state', 'data-feature-action="refresh"')],
      ['Tool history', `${Object.keys(state.tools).length} streamed tool cards tracked in this session.`, compactActionButton('Focus latest tool', 'data-feature-action="focus-latest-tool"')],
      ['Workspace roots', roots.length ? roots.map(shortPath).join(' · ') : 'No roots reported yet.', compactActionButton('Open system', 'data-feature-action="system"')],
    ]],
    tools: ['Tools', 'Inspect and operate the automation surface Eigen can use for this session.', tools.length ? tools.slice(0, 9).map(t => [t.name || t.Name || 'tool', (t.read_only ?? t.ReadOnly) ? 'read-only' : 'mutating', compactActionButton('Insert', `data-feature-action="insert-tool" data-tool-name="${escapeAttr(t.name || t.Name || 'tool')}"`)]) : [['No tools loaded', 'Tool registry will appear after session state arrives.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    shells: ['Shells', 'Track and control background commands without leaving the desktop.', shells.length ? shells.slice(0, 9).map(s => [s.id || s.ID || s.command || s.Command || 'shell', `${s.status || s.Status || 'unknown'} ${s.last_line || s.LastLine || ''}`, compactActionButton('Poll', `data-feature-action="poll-shell" data-shell-id="${escapeAttr(s.id || s.ID || '')}"`)]) : [['No background shells', 'Shells launched by tools appear here with status and last output.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    approvals: ['Approvals', 'Handle gated tool calls deliberately with visible status and arguments.', pending.length ? pending.map(p => [p.tool || p.Tool || 'approval', p.id || p.ID || p.result || p.Result || 'pending', `${compactActionButton('Approve', `data-feature-action="approve" data-approval-id="${escapeAttr(p.id || p.ID || p.result || p.Result || '')}"`)} ${compactActionButton('Deny', `data-feature-action="deny" data-approval-id="${escapeAttr(p.id || p.ID || p.result || p.Result || '')}"`)}`]) : [['No pending approvals', 'Gated operations will request approval here and in the timeline.', compactActionButton('Refresh', 'data-feature-action="refresh"')]]],
    memory: ['Memory', 'Keep durable context visible while planning and editing.', [
      ['Goal', snap.goal || snap.Goal || 'No active goal reported by daemon state.', compactActionButton('Refresh', 'data-feature-action="refresh"')],
      ['Roots', roots.length ? roots.map(shortPath).join(' · ') : 'No roots reported yet.', compactActionButton('Open system', 'data-feature-action="system"')],
      ['Profile', 'Use the Profile modal to edit global personalization.', compactActionButton('Edit profile', 'data-feature-action="profile"')],
    ]],
    plugins: ['Plugins', 'Manage extension capability from the desktop context.', [
      ['Available tools', `${tools.length || 0} tools exposed by skills, plugins, and built-ins.`, compactActionButton('Refresh', 'data-feature-action="refresh"')],
      ['Marketplace', 'Open plugins in the app shell for install, update, disable, and rollback flows.', compactActionButton('Insert /plugins', 'data-feature-action="insert-command" data-command="/plugins"')],
      ['Agent roles', 'Plugin-provided roles surface through the same command infrastructure.', compactActionButton('Insert /agents', 'data-feature-action="insert-command" data-command="/agents"')],
    ]],
    config: ['Config', 'Tune model, effort, permissions, search, and fast mode for the active session.', [
      ['Model', modelInput?.value || snap.model || snap.Model || 'default', compactActionButton('Apply model', 'data-feature-action="apply-model"')],
      ['Permission', permSelect?.value || snap.perm || snap.Perm || 'gated', compactActionButton('Apply perm', 'data-feature-action="apply-perm"')],
      ['Search / fast', `${searchSelect?.value || snap.search || snap.Search || 'off'} · ${fastToggle?.classList.contains('active') ? 'fast on' : 'fast off'}`, `${compactActionButton('Apply search', 'data-feature-action="apply-search"')} ${compactActionButton('Toggle fast', 'data-feature-action="toggle-fast"')}`],
    ]],
  };
  const [title, copy, cells] = featureMap[state.feature] || featureMap.changes;
  featureWorkspace.innerHTML = `<div class="feature-head"><div><div class="feature-title">${escapeHtml(title)}</div><div class="feature-copy">${escapeHtml(copy)}</div></div><button class="ghost compact" type="button" data-feature-close>Back to chat</button></div><div class="feature-grid">${cells.map(([k, v, action]) => `<div class="feature-cell"><strong>${escapeHtml(k)}</strong><span>${escapeHtml(v)}</span>${action ? `<div class="feature-actions">${action}</div>` : ''}</div>`).join('')}</div>`;
  featureWorkspace.querySelector('[data-feature-close]')?.addEventListener('click', () => setFeature('chat'));
  featureWorkspace.querySelectorAll('[data-feature-action]').forEach(btn => btn.addEventListener('click', () => runFeatureAction(btn)));
}

async function runFeatureAction(btn) {
  const action = btn.dataset.featureAction;
  if (action === 'refresh') return refreshActiveState({force: true});
  if (action === 'system') return openSystemModal();
  if (action === 'profile') return openProfileModal();
  if (action === 'insert-command') return insertComposerText(btn.dataset.command || '');
  if (action === 'insert-tool') return insertComposerText(`Use ${btn.dataset.toolName || 'tool'} to `);
  if (action === 'focus-latest-tool') {
    const last = timelineEl.querySelector('[data-tool-card]:last-of-type');
    if (last) scrollToVisible(last);
    return;
  }
  if (!state.active) return;
  if (action === 'approve' || action === 'deny') return answerApproval(btn.dataset.approvalId, action === 'approve');
  if (action === 'apply-model') return applySettingFromFeature('model', modelInput?.value?.trim());
  if (action === 'apply-perm') return applySettingFromFeature('perm', permSelect?.value || 'gated');
  if (action === 'apply-search') return applySettingFromFeature('search', searchSelect?.value || 'off');
  if (action === 'toggle-fast') {
    const snap = state.state || {};
    return applySettingFromFeature('fast', !(snap.fast || snap.Fast));
  }
  if (action === 'poll-shell') return refreshActiveState({force: true});
}

async function applySettingFromFeature(setting, value) {
  if (!state.active) return;
  await sessionSetting(state.active, setting, value);
  await refreshActiveState({force: true});
}

function insertComposerText(text) {
  if (!text || !inputEl) return;
  inputEl.value = `${inputEl.value || ''}${text}`;
  inputEl.focus();
  autoGrowInput();
  updateSendAvailability();
}

function updateInspector(snap) {
  const pendingApprovals = Object.values(state.approvals).filter(a => a.status === 'pending');
  const roots = snap.roots || snap.Roots || [];
  const shells = snap.shells || snap.Shells || [];
  const tools = snap.tools || snap.Tools || [];
  const tokens = snap.tokens || snap.Tokens || 0;
  const maxTokens = snap.max_tokens || snap.MaxTokens || 0;
  const goal = snap.goal || snap.Goal || '';
  inspectorEl.innerHTML = `
    <div class="inspector-card">
      <div class="card-label">Context</div>
      <div class="kv"><span>tokens</span><strong>${escapeHtml(tokens)}${maxTokens ? ` / ${escapeHtml(maxTokens)}` : ''}</strong></div>
      <div class="kv"><span>roots</span><strong>${escapeHtml(roots.length || 0)}</strong></div>
      <div class="kv"><span>tools</span><strong>${escapeHtml(tools.length || 0)}</strong></div>
      <div class="kv"><span>shells</span><strong>${escapeHtml(shells.length || 0)}</strong></div>
    </div>
    ${pendingApprovals.length ? `<div class="inspector-card"><div class="card-label">Pending approvals</div>${pendingApprovals.map(approvalSummaryHTML).join('')}</div>` : ''}
    ${shells.length ? `<div class="inspector-card"><div class="card-label">Background shells</div>${shells.slice(0, 6).map(shellSummaryHTML).join('')}</div>` : ''}
    ${tools.length ? `<div class="inspector-card"><div class="card-label">Available tools</div>${tools.slice(0, 10).map(toolSummaryHTML).join('')}${tools.length > 10 ? `<div class="small-copy">+${escapeHtml(tools.length - 10)} more tools</div>` : ''}</div>` : ''}
    ${goal ? `<div class="inspector-card"><div class="card-label">Goal</div><div class="small-copy">${escapeHtml(goal)}</div></div>` : ''}
    ${roots.length ? `<div class="inspector-card"><div class="card-label">Workspace roots</div>${roots.slice(0, 4).map(r => `<div class="path-row">${escapeHtml(shortPath(r))}</div>`).join('')}</div>` : ''}
  `;
  for (const button of inspectorEl.querySelectorAll('[data-approval-action]')) {
    button.addEventListener('click', () => answerApproval(button.dataset.approvalId, button.dataset.approvalAction === 'allow'));
  }
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

function approvalSummaryHTML(a) {
  return `<div class="approval-mini"><div><strong>${escapeHtml(a.tool)}</strong><span>${escapeHtml(a.id)}</span></div><div class="approval-mini-actions"><button class="primary compact" data-approval-id="${escapeAttr(a.id)}" data-approval-action="allow">Approve</button><button class="ghost compact" data-approval-id="${escapeAttr(a.id)}" data-approval-action="deny">Deny</button></div></div>`;
}

function shellSummaryHTML(raw) {
  const id = raw.id || raw.ID || '';
  const command = raw.command || raw.Command || '';
  const status = raw.status || raw.Status || 'unknown';
  const exitCode = raw.exit_code ?? raw.ExitCode;
  const lastLine = raw.last_line || raw.LastLine || '';
  return `<div class="shell-mini">
    <div class="shell-mini-head"><strong>${escapeHtml(id || command || 'shell')}</strong><span class="shell-status ${escapeAttr(status)}">${escapeHtml(status)}${exitCode ? ` · ${escapeHtml(exitCode)}` : ''}</span></div>
    ${command ? `<div class="shell-command">${escapeHtml(command)}</div>` : ''}
    ${lastLine ? `<div class="shell-last">${escapeHtml(lastLine)}</div>` : ''}
  </div>`;
}

function toolSummaryHTML(raw) {
  const name = raw.name || raw.Name || '';
  const readOnly = raw.read_only ?? raw.ReadOnly;
  return `<div class="tool-mini"><span>${escapeHtml(name)}</span><strong>${readOnly ? 'read' : 'write'}</strong></div>`;
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
  scrollToBottom();
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
  if (kind === 'tool_start') return ensureToolCard(e);
  if (kind === 'tool_result') return finishToolCard(e);
  if (kind === 'approval') {
    const approval = normalizeApproval({
      id: e.result || e.Result,
      tool: e.tool || e.ToolName,
      args: e.text || e.Text || e.tool_args || e.ToolArgs,
    });
    rememberApproval(approval);
    ensureApprovalCard(approval);
    updateInspector(state.state || {});
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
  const pinned = isPinnedToBottom();
  let last = timelineEl.lastElementChild;
  if (!last || !last.classList.contains('assistant') || last.dataset.streaming !== '1') {
    last = document.createElement('article');
    last.className = 'message assistant';
    last.dataset.streaming = '1';
    last.innerHTML = `<div class="role">assistant</div><div class="content"></div>`;
    timelineEl.appendChild(last);
  }
  last.querySelector('.content').textContent += text;
  settleScroll(pinned);
}

function appendEventBlock(cls, label, text) {
  const pinned = isPinnedToBottom();
  const el = document.createElement('article');
  el.className = `event ${cls}`;
  el.innerHTML = `<div class="kind">${escapeHtml(label)}</div><div class="content">${escapeHtml(text)}</div>`;
  timelineEl.appendChild(el);
  settleScroll(pinned);
}


function ensureToolCard(e) {
  const tool = normalizeToolEvent(e);
  state.tools[tool.id] = {...tool, status: 'running'};
  const existing = timelineEl.querySelector(`[data-tool-card="${cssEscape(tool.id)}"]`);
  if (existing) return updateToolCard(existing, state.tools[tool.id]);
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
  return {
    id,
    tool,
    step,
    args: e.tool_args || e.ToolArgs || '',
    result: e.result || e.Result || '',
    isError: !!(e.is_error || e.IsError),
  };
}

function toolCardHTML(tool) {
  const args = pretty(tool.args);
  const result = String(tool.result || '');
  const status = tool.status || 'running';
  const renderedResult = result ? renderToolResult(result, tool.isError) : '';
  return `
    <div class="tool-card-head">
      <div>
        <div class="kind">Tool · ${escapeHtml(tool.tool)}</div>
        <div class="tool-id">${escapeHtml(tool.id)}${tool.step ? ` · step ${escapeHtml(tool.step)}` : ''}</div>
      </div>
      <span class="tool-status ${escapeAttr(status)}">${escapeHtml(status)}</span>
    </div>
    ${args ? `<details class="tool-section" open><summary>Arguments</summary><pre>${escapeHtml(args)}</pre></details>` : ''}
    ${renderedResult}
  `;
}

function updateToolCard(el, tool) {
  el.dataset.status = tool.status || 'running';
  if (tool.isError) el.dataset.error = 'true';
  el.innerHTML = toolCardHTML(tool);
  wireToolCard(el);
}

function wireToolCard(el) {
  for (const details of el.querySelectorAll('details')) {
    details.addEventListener('toggle', () => {
      if (details.open) scrollToVisible(details);
    });
  }
}

function compactToolResult(text) {
  const s = String(text || '');
  if (s.length <= 8000) return s;
  return `${s.slice(0, 7800)}

… ${s.length - 7800} more characters`;
}

function renderToolResult(result, isError) {
  const label = isError ? 'Error' : isUnifiedDiff(result) ? 'Diff' : 'Result';
  if (isUnifiedDiff(result)) {
    return `<details class="tool-section diff-section" open><summary>${label}</summary><div class="diff-view">${renderUnifiedDiff(result)}</div></details>`;
  }
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
}

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
  el.className = 'event approval approval-card';
  el.dataset.approvalCard = approval.id;
  el.innerHTML = `
    <div class="kind">Approval · ${escapeHtml(approval.tool)}</div>
    <div class="content approval-content">${escapeHtml(formatApprovalArgs(approval))}</div>
    <div class="approval-actions">
      <button class="primary" type="button" data-action="allow">Approve</button>
      <button class="ghost" type="button" data-action="deny">Deny</button>
      <span class="approval-state">pending</span>
    </div>`;
  el.querySelector('[data-action="allow"]').addEventListener('click', () => answerApproval(approval.id, true));
  el.querySelector('[data-action="deny"]').addEventListener('click', () => answerApproval(approval.id, false));
  timelineEl.appendChild(el);
  settleScroll(pinned);
}

function setApprovalStatus(id, status) {
  if (state.approvals[id]) state.approvals[id].status = status;
  const card = timelineEl.querySelector(`[data-approval-card="${cssEscape(id)}"]`);
  if (!card) return;
  card.dataset.status = status;
  const label = card.querySelector('.approval-state');
  if (label) label.textContent = status;
  for (const btn of card.querySelectorAll('button')) btn.disabled = status !== 'pending';
}

function formatApprovalArgs(approval) {
  const args = String(approval.args || '').trim();
  if (!args) return approval.tool;
  if (args.startsWith(approval.tool + ' ')) return args.slice(approval.tool.length + 1);
  return args;
}

$('new-session').onclick = () => openNewSessionModal();

function handleRailAction(e) {
  const target = e.target?.closest?.('#new-session, #profile-button, #system-button');
  if (!target) return;
  e.preventDefault();
  if (target.id === 'new-session') openNewSessionModal();
  if (target.id === 'profile-button') openProfileModal();
  if (target.id === 'system-button') openSystemModal();
}

document.addEventListener('pointerdown', handleRailAction, true);
document.addEventListener('mousedown', handleRailAction, true);
featureNav?.addEventListener('click', (e) => {
  const btn = e.target?.closest?.('[data-feature]');
  if (!btn) return;
  setFeature(btn.dataset.feature);
});


newSessionClose.onclick = () => closeNewSessionModal();
newSessionCancel.onclick = () => closeNewSessionModal();
newSessionModal.addEventListener('click', (e) => {
  if (e.target === newSessionModal) closeNewSessionModal();
});

newSessionForm.onsubmit = async (e) => {
  e.preventDefault();
  setModalError('');
  const submit = newSessionForm.querySelector('[type="submit"]');
  submit.disabled = true;
  submit.textContent = 'Creating…';
  try {
    const out = await createSession({
      dir: sessionDirInput.value.trim(),
      model: sessionModelInput.value.trim(),
      perm: sessionPermInput.value,
    });
    closeNewSessionModal();
    await refreshSessions();
    await openSession(out.id || out.ID || out);
  } catch (err) {
    setModalError(err.message || String(err));
  } finally {
    submit.disabled = false;
    submit.textContent = 'Create session';
  }
};

profileButton.onclick = () => openProfileModal();
systemButton.onclick = () => openSystemModal();
systemClose.onclick = () => closeSystemModal();
systemCancel.onclick = () => closeSystemModal();
systemRefresh.onclick = () => renderSystemHealth();
systemModal.addEventListener('click', (e) => {
  if (e.target === systemModal) closeSystemModal();
});
profileClose.onclick = () => closeProfileModal();
profileCancel.onclick = () => closeProfileModal();
profileModal.addEventListener('click', (e) => {
  if (e.target === profileModal) closeProfileModal();
});
profileClear.onclick = () => {
  profileText.value = '';
  profileText.focus();
};

profileForm.onsubmit = async (e) => {
  e.preventDefault();
  setProfileError('');
  const submit = profileForm.querySelector('[type="submit"]');
  submit.disabled = true;
  submit.textContent = 'Saving…';
  try {
    await saveUserProfile(profileText.value);
    closeProfileModal();
  } catch (err) {
    setProfileError(err.message || String(err));
  } finally {
    submit.disabled = false;
    submit.textContent = 'Save profile';
  }
};

$('interrupt').onclick = async () => {
  if (!state.active) return;
  await sessionAction(state.active, 'interrupt');
};

$('resend').onclick = async () => {
  if (!state.active) return;
  await sessionAction(state.active, 'resend');
};

$('clear').onclick = async () => {
  if (!state.active) return;
  if (!confirm('Clear this session transcript?')) return;
  await sessionAction(state.active, 'clear');
  await refreshActiveState({force: true});
};

modelInput.addEventListener('change', async () => {
  if (!state.active) return;
  const value = modelInput.value.trim();
  if (!value) return;
  await sessionSetting(state.active, 'model', value);
  await refreshActiveState({force: true});
});

modelInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    modelInput.blur();
  }
});

effortSelect.onchange = async () => {
  if (!state.active) return;
  await sessionSetting(state.active, 'effort', effortSelect.value);
  await refreshActiveState({force: true});
};

permSelect.onchange = async () => {
  if (!state.active) return;
  await sessionSetting(state.active, 'perm', permSelect.value);
  await refreshActiveState({force: true});
};

searchSelect.onchange = async () => {
  if (!state.active) return;
  await sessionSetting(state.active, 'search', searchSelect.value);
  await refreshActiveState({force: true});
};

fastToggle.onclick = async () => {
  if (!state.active) return;
  const snap = state.state || {};
  const next = !(snap.fast || snap.Fast);
  await sessionSetting(state.active, 'fast', next);
  await refreshActiveState({force: true});
};

$('composer').onsubmit = async (e) => {
  e.preventDefault();
  if (!state.active) return;
  const running = !!(state.state?.running || state.state?.Running);
  if (running && !inputEl.value.trim()) {
    await sessionAction(state.active, 'interrupt');
    return;
  }
  const text = inputEl.value.trim();
  if (!text) return;
  appendMessage('user', text);
  scrollToBottom();
  inputEl.value = '';
  autoGrowInput();
  updateSendAvailability();
  await sendInput(state.active, text);
};

jumpLatestEl.onclick = () => scrollToBottom();

timelineEl.addEventListener('scroll', () => {
  state.userPinnedBottom = isPinnedToBottom();
  jumpLatestEl.classList.toggle('hidden', state.userPinnedBottom);
});

inputEl.addEventListener('input', () => {
  autoGrowInput();
  updateSendAvailability();
});

inputEl.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    $('composer').requestSubmit();
  }
});

function handleGlobalShortcut(e) {
  if (e.key !== 'Escape') return;
  if (!newSessionModal.classList.contains('hidden')) closeNewSessionModal();
  if (!profileModal.classList.contains('hidden')) closeProfileModal();
  if (!systemModal.classList.contains('hidden')) closeSystemModal();
}

document.addEventListener('keydown', handleGlobalShortcut, true);

window.openNewSessionModal = openNewSessionModal;
window.openProfileModal = openProfileModal;

function openNewSessionModal() {
  setModalError('');
  sessionDirInput.value = '';
  sessionModelInput.value = '';
  sessionPermInput.value = 'gated';
  newSessionModal.classList.remove('hidden');
  setTimeout(() => sessionDirInput.focus(), 0);
}

function closeNewSessionModal() {
  newSessionModal.classList.add('hidden');
}

function setModalError(message) {
  newSessionError.textContent = message;
  newSessionError.classList.toggle('hidden', !message);
}

async function openProfileModal() {
  setProfileError('');
  profileText.value = 'Loading…';
  profileModal.classList.remove('hidden');
  try {
    profileText.value = await getUserProfile();
    profileText.focus();
  } catch (err) {
    profileText.value = '';
    setProfileError(err.message || String(err));
  }
}

function closeProfileModal() {
  profileModal.classList.add('hidden');
}

function setProfileError(message) {
  profileError.textContent = message;
  profileError.classList.toggle('hidden', !message);
}

async function openSystemModal() {
  systemModal.classList.remove('hidden');
  await renderSystemHealth();
}

function closeSystemModal() {
  systemModal.classList.add('hidden');
}

async function renderSystemHealth() {
  systemBody.innerHTML = '<div class="modal-copy">Loading daemon health…</div>';
  try {
    const h = await getHealth();
    const st = h.stats || h.Stats || {};
    const rows = [
      ['daemon', h.ok ? 'connected' : 'offline'],
      ['socket', h.socket || h.Socket || ''],
      ['version', st.version || st.Version || ''],
      ['executable', st.executable || st.Executable || ''],
      ['revision', st.vcs_revision || st.VCSRevision || ''],
      ['uptime', formatDuration(st.uptime_sec || st.UptimeSec || 0)],
      ['sessions', st.sessions ?? st.Sessions ?? 0],
      ['views', st.views ?? st.Views ?? 0],
      ['running turns', st.running_turns ?? st.RunningTurns ?? 0],
      ['background tasks', st.bg_tasks ?? st.BgTasks ?? 0],
      ['goroutines', st.goroutines ?? st.Goroutines ?? 0],
      ['heap', formatBytes(st.heap_alloc_b || st.HeapAllocB || 0)],
      ['rss', formatBytes(st.rss_b || st.RSSB || 0)],
      ['input tokens', st.input_tokens ?? st.InputTokens ?? 0],
      ['output tokens', st.output_tokens ?? st.OutputTokens ?? 0],
      ['cache read', st.cache_read_tokens ?? st.CacheReadTokens ?? 0],
      ['cache write', st.cache_write_tokens ?? st.CacheWriteTokens ?? 0],
    ];
    systemBody.innerHTML = rows.filter(([, v]) => v !== '').map(([k, v]) => `
      <div class="system-row"><span>${escapeHtml(k)}</span><strong>${escapeHtml(v)}</strong></div>
    `).join('') + (h.error || h.Error ? `<div class="modal-error">${escapeHtml(h.error || h.Error)}</div>` : '');
  } catch (err) {
    systemBody.innerHTML = `<div class="modal-error">${escapeHtml(err.message || String(err))}</div>`;
  }
}

function formatBytes(n) {
  n = Number(n || 0);
  if (n < 1024) return `${n} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
  return `${n.toFixed(n >= 10 ? 1 : 2)} ${units[i]}`;
}

function formatDuration(sec) {
  sec = Number(sec || 0);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

function updateComposerState(running) {
  const hasSession = !!state.active;
  inputEl.disabled = !hasSession;
  inputEl.placeholder = !hasSession
    ? 'Create or select a session to begin…'
    : running
      ? 'Steer the running turn, or press Stop with an empty composer…'
      : 'Ask Eigen to build, inspect, fix, or explain…';
  if (sendEl) {
    sendEl.textContent = running && !inputEl.value.trim() ? 'Stop' : 'Send';
    sendEl.classList.toggle('stop', running && !inputEl.value.trim());
  }
  updateSendAvailability();
}

function updateSendAvailability() {
  if (!sendEl) return;
  const running = !!(state.state?.running || state.state?.Running);
  sendEl.disabled = !state.active || (!running && !inputEl.value.trim());
  sendEl.textContent = running && !inputEl.value.trim() ? 'Stop' : 'Send';
  sendEl.classList.toggle('stop', running && !inputEl.value.trim());
}

function autoGrowInput() {
  inputEl.style.height = '0px';
  inputEl.style.height = Math.min(inputEl.scrollHeight, 170) + 'px';
}

function isPinnedToBottom() {
  return timelineEl.scrollHeight - timelineEl.scrollTop - timelineEl.clientHeight < 80;
}

function scrollToBottom() {
  timelineEl.scrollTop = timelineEl.scrollHeight;
  state.userPinnedBottom = true;
  jumpLatestEl?.classList.add('hidden');
}

function settleScroll(wasPinned) {
  if (wasPinned || state.userPinnedBottom) scrollToBottom();
  else jumpLatestEl?.classList.remove('hidden');
}

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
function cssEscape(s) {
  if (window.CSS?.escape) return window.CSS.escape(String(s));
  return String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&');
}

updateComposerState(false);
autoGrowInput();
boot();
