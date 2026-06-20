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
    this.scrollHeight = 1000;
    this.scrollHeight = 1000;
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
  querySelectorAll(sel) { return []; }
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
      if (path === '/api/health') data = {ok: true, stats: {version: 'test'}};
      else if (path === '/api/sessions') data = [{id: 's1', title: 'Session 1', status: 'idle', dir: '/tmp/project'}];
      else if (String(path).includes('/state')) data = {title: 'Session 1', provider: 'mock', model: 'm1', perm: 'gated', search: 'off', fast_ok: true, messages: [], pending: [{id: 'ap1', tool: 'bash', args: 'echo ok'}], shells: [{id: 'sh1', command: 'sleep 10', status: 'running', last_line: 'tick'}], tools: [{name: 'bash', read_only: false}, {name: 'read', read_only: true}], roots: ['/tmp/project'], goal: 'ship gui'};
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
assert.match(document.getElementById('feature-workspace').innerHTML, /Every Eigen desktop page|Chat|Changes|Tools|Shells|Approvals|Memory|Plugins|Config/, 'home page lists every available desktop page without relying on truncated rail labels');

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
assert.match(document.getElementById('feature-workspace').innerHTML, /Available desktop pages|Chat|Changes|Tools|Shells|Approvals|Memory|Plugins|Config/, 'home renders the full page directory');
ctx.setFeature('tools');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, 'feature workspace hides chat timeline');
assert.match(document.getElementById('feature-workspace').innerHTML, /Tools|automation surface|Allow tool turn|Inspect runtime|Allow bash/, 'tools surface renders dense desktop feature controls');
assert.equal(document.getElementById('overview-surface').textContent, 'tools', 'overview follows selected feature');
for (const [feature, pattern] of [
  ['changes', /Changes|Diff rendering|Tool history|Workspace roots/],
  ['tools', /Tools|automation surface|Allow tool turn|Inspect runtime|Allow bash/],
  ['shells', /Shells|SHELLS|Foreground bash|Process safety/],
  ['approvals', /Approvals|No pending approvals|Decision path|Permission posture/],
  ['memory', /Memory|Goal|Roots|Profile|Set from composer|Compact|Add current dir/],
  ['plugins', /Plugins|Available tools|Marketplace|Agent roles/],
  ['config', /Config|Permission|Search \/ fast|Apply model|Toggle fast/],
]) {
  ctx.setFeature(feature);
  assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, `${feature} hides chat timeline`);
  assert.match(document.getElementById('feature-workspace').innerHTML, pattern, `${feature} surface renders shipped controls`);
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
ctx.setFeature('chat');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), false, 'chat feature restores timeline');

ctx.ensureApprovalCard({id: 'a1', tool: 'bash', args: 'echo ok', status: 'pending'});
ctx.setApprovalStatus('a1', 'approved');

ctx.openProfileModal();
await Promise.resolve();
await Promise.resolve();
assert.equal(document.getElementById('profile-modal').classList.contains('hidden'), false, 'profile modal opens');

assert(apiLog.some(x => x.path === '/api/health'), 'health API called');
assert(apiLog.some(x => x.path === '/api/sessions'), 'sessions API called');

console.log(JSON.stringify({ok: true, intervals: intervals.length, eventSources: eventSourceInstances.length, apiCalls: apiLog.length}));
