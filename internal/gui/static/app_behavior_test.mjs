import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

class ClassList {
  constructor(el) { this.el = el; this.set = new Set(); }
  add(...names) { for (const n of names) this.set.add(n); this._sync(); }
  remove(...names) { for (const n of names) this.set.delete(n); this._sync(); }
  contains(n) { return this.set.has(n); }
  toggle(n, force) {
    const on = force === undefined ? !this.set.has(n) : !!force;
    if (on) this.set.add(n); else this.set.delete(n);
    this._sync();
    return on;
  }
  _sync() { this.el.className = [...this.set].join(' '); }
}

class Element {
  constructor(tag = 'div', id = '') {
    this.tagName = tag.toUpperCase();
    this.id = id;
    this.children = [];
    this.parentElement = null;
    this.dataset = {};
    this.style = {};
    this.attributes = {};
    this.eventHandlers = {};
    this.classList = new ClassList(this);
    this._className = '';
    this._innerHTML = '';
    this.textContent = '';
    this.value = '';
    this.disabled = false;
    this.scrollTop = 0;
    this.scrollHeight = 1000;
    this.clientHeight = 500;
  }
  get className() { return this._className; }
  set className(v) { this._className = String(v || ''); this.classList.set = new Set(this._className.split(/\s+/).filter(Boolean)); }
  get innerHTML() { return this._innerHTML; }
  set innerHTML(v) { this._innerHTML = String(v || ''); this.children = []; }
  appendChild(child) { child.parentElement = this; this.children.push(child); return child; }
  get lastElementChild() { return this.children[this.children.length - 1] || null; }
  querySelector(sel) {
    if (sel === '.content') return this._content || (this._content = new Element('div'));
    if (sel === '[data-feature-close]') return this._featureClose || (this._featureClose = new Element('button'));
    if (sel === '.approval-state') return this._approvalState || (this._approvalState = new Element('span'));
    if (sel === '[type="submit"]') return this._submit || (this._submit = new Element('button'));
    if (sel === '[data-action="allow"]') return this._allow || (this._allow = new Element('button'));
    if (sel === '[data-action="deny"]') return this._deny || (this._deny = new Element('button'));
    return null;
  }
  querySelectorAll(sel) {
    if (sel === '[data-tool-card]') return this.children.filter(child => child.dataset?.toolCard);
    return [];
  }
  matches(sel) {
    return sel === '[data-tool-card]' ? !!this.dataset?.toolCard : false;
  }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name]; }
  addEventListener(type, cb) { (this.eventHandlers[type] ||= []).push(cb); }
  dispatchEvent(type, ev = {}) { for (const cb of this.eventHandlers[type] || []) cb(ev); }
  closest() { return this; }
  getBoundingClientRect() { return {top: 0, bottom: 100, height: 100}; }
  focus() { this.focused = true; }
  blur() { this.blurred = true; }
  requestSubmit() { if (typeof this.onsubmit === 'function') this.onsubmit({preventDefault(){}}); }
}

class Document {
  constructor() { this.elements = new Map(); this.eventHandlers = {}; this.activeElement = null; this.body = new Element('body', 'body'); }
  getElementById(id) {
    if (!this.elements.has(id)) this.elements.set(id, new Element('div', id));
    return this.elements.get(id);
  }
  createElement(tag) { return new Element(tag); }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name]; }
  addEventListener(type, cb) { (this.eventHandlers[type] ||= []).push(cb); }
}

function makeContext() {
  const document = new Document();
  const intervals = [];
  const timeouts = [];
  const eventSourceInstances = [];
  class EventSourceMock {
    constructor(url) { this.url = url; this.closed = false; this.handlers = {}; eventSourceInstances.push(this); }
    addEventListener(type, cb) { this.handlers[type] = cb; }
    close() { this.closed = true; }
  }
  const apiLog = [];
  const ctx = {
    console,
    document,
    window: {CSS: {escape: s => String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&')}},
    queueMicrotask,
    EventSource: EventSourceMock,
    fetch: async (path, opts = {}) => {
      apiLog.push({path, opts});
      let data = {};
      if (path === '/api/health') data = {ok: true, socket: 'test', stats: {version: 'test', goroutines: 3, heap_alloc_b: 1024, rss_b: 2048}};
      else if (String(path).includes('/observe')) data = {enabled: true, path: '/tmp/eigen-observe/events.jsonl', limit: 5000, summary: {records: 42, errors: {tool: 2}, models: {'gpt': {requests: 4}}, tools: {bash: {calls: 3}}, routes: {routed: 5, skipped: 1, assessed: 2, by_model: {'gpt': 5}, skip_reasons: {cached: 1}}, runtime: {max_goroutines: 8, max_mem_alloc_bytes: 4096}}};
      else if (path === '/api/sessions') data = [{id: 's1', title: 'Session 1', status: 'idle', dir: '/tmp/project'}];
      else if (String(path).includes('/state')) data = {title: 'Session 1', provider: 'mock', model: 'm1', perm: 'gated', effort: 'medium', search: 'auto', fast_ok: true, messages: [], pending: [{id: 'ap1', tool: 'bash', args: 'echo ok'}], shells: [{id: 'sh1', command: 'sleep 10', status: 'running', last_line: 'tick'}, {id: 'sh0', command: 'true', status: 'exited', exit_code: 0, last_line: 'done'}], tools: [{name: 'bash', read_only: false}, {name: 'read', read_only: true}], roots: ['/tmp/project'], goal: 'ship gui'};
      else if (path === '/api/profile') data = {profile: 'hello'};
      return {ok: true, statusText: 'OK', text: async () => JSON.stringify(data)};
    },
    setInterval: (cb, ms) => { intervals.push({cb, ms, cleared: false}); return intervals.length; },
    clearInterval: id => { if (intervals[id - 1]) intervals[id - 1].cleared = true; },
    setTimeout: (cb, ms) => { timeouts.push({cb, ms}); queueMicrotask(cb); return timeouts.length; },
    confirm: () => true,
    JSON,
    CSS: {escape: s => String(s)},
  };
  ctx.window.EventSource = EventSourceMock;
  ctx.window.openNewSessionModal = undefined;
  ctx.window.openProfileModal = undefined;
  ctx.window.openSystemModal = undefined;
  return {ctx, document, intervals, timeouts, eventSourceInstances, apiLog};
}

function assertContainsAll(html, wants, label) {
  for (const want of wants) {
    assert.ok(String(html).includes(want), `${label} missing ${want}`);
  }
}

const indexHTML = fs.readFileSync('internal/gui/static/index.html', 'utf8');
const stylesCSS = fs.readFileSync('internal/gui/static/styles.css', 'utf8');

async function loadApp() {
  const env = makeContext();
  const code = fs.readFileSync('internal/gui/static/app.js', 'utf8');
  vm.createContext(env.ctx);
  vm.runInContext(code, env.ctx, {filename: 'app.js'});
  await env.ctx.refreshHealth();
  await env.ctx.refreshSessions();
  return env;
}

const env = await loadApp();
const {ctx, document, intervals, eventSourceInstances, apiLog} = env;

assert.equal(document.getElementById('daemon').textContent, 'daemon connected');
assert.equal(document.getElementById('sessions').children.length, 1, 'session rail renders fetched sessions');
ctx.updateDesktopOverview({});
assert.equal(document.getElementById('overview-surface').textContent, 'home', 'desktop overview starts on home surface');
ctx.renderFeatureWorkspace();
let homeHTML = document.getElementById('feature-workspace').innerHTML;
assertContainsAll(homeHTML, ['home-surface', 'Every Eigen desktop page, one jump away.', 'Available desktop pages', 'non-truncated directory'], 'home page');
for (const [id, title] of [['chat', 'Chat'], ['changes', 'Changes'], ['tools', 'Tools'], ['shells', 'Shells'], ['approvals', 'Approvals'], ['memory', 'Memory'], ['plugins', 'Plugins'], ['config', 'Config']]) {
  assert.ok(homeHTML.includes(`data-home-feature="${id}"`), `home missing tile target ${id}`);
  assert.ok(homeHTML.includes(`surface-tile-title">${title}</span>`), `home missing tile title ${title}`);
}

const beforeOpenSources = eventSourceInstances.length;
const beforeOpenIntervals = intervals.length;
await ctx.openSession('s1');
assert.equal(eventSourceInstances.length, beforeOpenSources + 2, 'openSession connects one EventSource after closing refresh-opened stream');
assert.equal(intervals.length, beforeOpenIntervals + 2, 'openSession schedules active state polling after clearing refresh-opened poll');
const firstSource = eventSourceInstances.at(-1);
const firstPoll = intervals.at(-1);

await ctx.openSession('s2');
assert.equal(firstSource.closed, true, 'switching session closes prior EventSource');
assert.equal(firstPoll.cleared, true, 'switching session clears prior polling interval');
assert.equal(eventSourceInstances.length, beforeOpenSources + 3, 'switching session creates one replacement EventSource');

ctx.appendEvent({kind: 'tool_start', tool: 'edit', tool_id: 't1', tool_args: '{"path":"x"}'});
ctx.appendEvent({kind: 'tool_result', tool: 'edit', tool_id: 't1', result: 'diff --git a/x b/x\n@@ -1 +1 @@\n-old\n+new'});
assert.match(document.getElementById('timeline').innerHTML + document.getElementById('timeline').children.map?.(x => x.innerHTML).join(''), /diff|Tool|edit/i);

ctx.setFeature('home');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, 'home hides chat timeline');
assert.equal(document.getElementById('desktop-overview').classList.contains('hidden'), false, 'home keeps overview visible');
homeHTML = document.getElementById('feature-workspace').innerHTML;
assertContainsAll(homeHTML, ['Available desktop pages', 'Chat', 'Changes', 'Tools', 'Shells', 'Approvals', 'Memory', 'Plugins', 'Config'], 'home directory');
assert.match(indexHTML, /id="feature-nav"[\s\S]*data-feature="home"[\s\S]*data-feature="chat"/, 'static shell includes Home before Chat');
assert.match(stylesCSS, /\.feature-nav\s*\{[\s\S]*grid-template-columns:\s*1fr;/, 'feature nav uses single full-width column to avoid truncating labels');
assert.match(stylesCSS, /\.overview-card\s*\{[\s\S]*min-height:\s*76px;[\s\S]*overflow:\s*visible;/, 'overview cards grow instead of silently clipping wrapped text');
assert.match(stylesCSS, /\.system-row strong\s*\{[^}]*overflow-wrap:\s*anywhere;/s, 'system modal long values wrap instead of truncating');
assert.match(indexHTML, /id="rail-toggle"[\s\S]*aria-label="Toggle sidebar"/, 'shell has closable sidebar toggle');
assert.match(indexHTML, /data-feature="observe"/, 'shell exposes observability surface');
assert.match(stylesCSS, /body\.rail-collapsed \.shell/, 'collapsed sidebar changes layout columns');
assert.match(stylesCSS, /\.workspace\s*\{[\s\S]*grid-template-rows:\s*var\(--topbar-height\) auto minmax\(0, 1fr\) auto;/, 'workspace gives timeline a bounded scroll row');
assert.match(stylesCSS, /\.timeline\s*\{[\s\S]*overflow-y:\s*auto;/, 'chat timeline owns vertical scrolling');
assert.match(stylesCSS, /\.sessions\s*\{[\s\S]*overflow-y:\s*auto;/, 'sessions rail owns vertical scrolling');
assert.match(stylesCSS, /\.composer\s*\{[\s\S]*position:\s*sticky;[\s\S]*bottom:\s*0;/, 'chat composer stays available at bottom while chat scrolls');
assert.match(stylesCSS, /--topbar-height:\s*56px;/, 'top header is thin');
for (const id of ['clear', 'interrupt', 'resend']) assert.ok(indexHTML.includes(`id="${id}"`), `topbar missing ${id} control hook`);
ctx.setFeature('tools');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, 'feature workspace hides chat timeline');
assert.match(document.getElementById('feature-workspace').innerHTML, /Tools|automation surface|Allow tool turn|Inspect runtime|Allow bash/, 'tools surface renders dense desktop feature controls');
assert.equal(document.getElementById('overview-surface').textContent, 'tools', 'overview follows selected feature');
assert.match(stylesCSS, /@media \(max-width: 1280px\)[\s\S]*\.actions \.optional \{ display: none; \}/, 'optional topbar controls hide only at the narrow breakpoint');
assert.doesNotMatch(stylesCSS, /\.actions \#(?:clear|interrupt|resend)[\s\S]*display:\s*none/, 'run controls are not permanently hidden by CSS');
const featureExpectations = {
  changes: ['Changes', 'Diff rendering', 'Tool history', 'Focus latest tool', 'Workspace roots'],
  tools: ['Tools', 'bash', 'mutating', 'read', 'read-only', 'Allow next turn', 'Inspect'],
  shells: ['Shells', 'sh1', 'running tick', 'Poll', 'Kill'],
  approvals: ['Approvals', 'bash', 'ap1', 'Approve', 'Deny'],
  memory: ['Memory', 'Goal', 'ship gui', 'Set from composer', 'Compact', 'Add current dir', 'Edit profile'],
  plugins: ['Plugins', 'Available tools', '2 tools exposed', 'Marketplace', 'Agent roles'],
  config: ['Config', 'Model', 'Permission', 'Search / fast', 'Apply model', 'Apply perm', 'Apply search', 'Toggle fast'],
  observe: ['Observe', 'Telemetry', '0 events', 'enabled', 'Models / tools', 'Routing', 'Runtime pressure', 'Session signals'],
};
for (const [feature, wants] of Object.entries(featureExpectations)) {
  ctx.setFeature(feature);
  assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, `${feature} hides chat timeline`);
  assertContainsAll(document.getElementById('feature-workspace').innerHTML, wants, `${feature} surface`);
}
ctx.setFeature('config');
document.getElementById('model-input').value = 'glm-5.2';
await ctx.runFeatureAction({dataset: {featureAction: 'apply-model'}});
assert(apiLog.some(x => String(x.path).includes('/model') && String(x.opts?.body || '').includes('glm-5.2')), 'config apply model calls settings API');
await ctx.runFeatureAction({dataset: {featureAction: 'apply-perm'}});
assert(apiLog.some(x => String(x.path).includes('/perm')), 'config apply perm calls settings API');
await ctx.runFeatureAction({dataset: {featureAction: 'toggle-fast'}});
assert(apiLog.some(x => String(x.path).includes('/fast')), 'config toggle fast calls settings API');
ctx.setFeature('approvals');
assert.match(document.getElementById('feature-workspace').innerHTML, /Approvals|Approve|Deny/, 'approvals surface renders controls');
await ctx.runFeatureAction({dataset: {featureAction: 'approve', approvalId: 'ap1'}});
assert(apiLog.some(x => String(x.path).includes('/approve') && String(x.opts?.body || '').includes('true')), 'approval action calls approve API');
ctx.setFeature('tools');
await ctx.runFeatureAction({dataset: {featureAction: 'allow-tool', toolName: 'bash'}});
assert(apiLog.some(x => String(x.path).includes('/input') && String(x.opts?.body || '').includes('bash')), 'tool action sends allow-tools input API');
ctx.setFeature('shells');
assertContainsAll(document.getElementById('feature-workspace').innerHTML, ['1 running · 2 total', 'exited'], 'shells metrics');
assert.match(ctx.shellSummaryHTML({id: 'sh0', status: 'exited', exit_code: 0}), /exited · 0/, 'shell summaries show successful exit code 0');
ctx.setFeature('observe');
await ctx.refreshObserve({force: true});
assertContainsAll(document.getElementById('feature-workspace').innerHTML, ['Observe', 'Telemetry', '42 events', '2 errors', '/tmp/eigen-observe/events.jsonl', 'Models / tools', 'Routing', 'Runtime pressure', 'Session signals'], 'observe surface');
await ctx.runFeatureAction({dataset: {featureAction: 'kill-shell', shellId: 'sh1'}});
assert(apiLog.some(x => String(x.path).includes('/kill-shell') && String(x.opts?.body || '').includes('sh1')), 'shell action calls kill-shell API');
ctx.setFeature('memory');
document.getElementById('input').value = 'ship gui';
await ctx.runFeatureAction({dataset: {featureAction: 'set-goal'}});
assert(apiLog.some(x => String(x.path).includes('/goal') && String(x.opts?.body || '').includes('ship gui')), 'memory action calls goal API');
await ctx.runFeatureAction({dataset: {featureAction: 'compact'}});
assert(apiLog.some(x => String(x.path).includes('/compact')), 'memory action calls compact API');
ctx.setFeature('plugins');
assert.match(document.getElementById('feature-workspace').innerHTML, /Plugins|Available tools|Marketplace|Agent roles/, 'plugins surface renders desktop capability controls');
ctx.setFeature('changes');
assert.match(document.getElementById('feature-workspace').innerHTML, /Changes|Diff rendering|Tool history|Workspace roots/, 'changes surface renders repository review controls');
ctx.ensureToolCard({tool: 'bash', tool_id: 'latest-tool', tool_args: '{}'});
ctx.setFeature('changes');
await ctx.runFeatureAction({dataset: {featureAction: 'focus-latest-tool'}});
assert.equal(document.getElementById('timeline').classList.contains('hidden'), false, 'focus latest tool returns to chat before scrolling');
assert.equal(document.body.dataset.feature, 'chat', 'focus latest tool highlights in chat, not on Changes');
ctx.setFeature('chat');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), false, 'chat feature restores timeline');
assert.equal(document.getElementById('composer').classList.contains('hidden'), false, 'chat composer remains available on chat surface');
ctx.toggleRailCollapsed(true);
assert.equal(document.body.classList.contains('rail-collapsed'), true, 'sidebar can collapse');
assert.equal(document.getElementById('rail-toggle').getAttribute('aria-expanded'), 'false', 'sidebar toggle exposes collapsed state');

ctx.ensureApprovalCard({id: 'a1', tool: 'bash', args: 'echo ok', status: 'pending'});
ctx.setApprovalStatus('a1', 'approved');

assert.match(ctx.renderToolResult('diff --git a/x b/x\n@@ -1 +1 @@\n-old\n+new'), /diff-section[\s\S]*open[\s\S]*diff-view/, 'diff results open visibly by default');
assert.doesNotMatch(ctx.toolCardHTML({tool: 'read', id: 't2', status: 'done', args: '{}', result: 'plain text'}), /<details class="tool-section" open><summary>Arguments/, 'non-diff tool arguments are collapsed to reduce noise');
ctx.openProfileModal();
await Promise.resolve();
await Promise.resolve();
assert.equal(document.getElementById('profile-modal').classList.contains('hidden'), false, 'profile modal opens');

assert(apiLog.some(x => x.path === '/api/health'), 'health API called');
assert(apiLog.some(x => x.path === '/api/sessions'), 'sessions API called');

console.log(JSON.stringify({ok: true, intervals: intervals.length, eventSources: eventSourceInstances.length, apiCalls: apiLog.length}));
