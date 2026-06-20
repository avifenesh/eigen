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
  constructor() { this.elements = new Map(); this.eventHandlers = {}; this.activeElement = null; }
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
      else if (String(path).includes('/state')) data = {title: 'Session 1', provider: 'mock', model: 'm1', perm: 'gated', messages: [], pending: [], shells: [], tools: [], roots: []};
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
assert.equal(document.getElementById('overview-surface').textContent, 'chat', 'desktop overview starts on chat surface');

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

ctx.setFeature('tools');
assert.equal(document.getElementById('timeline').classList.contains('hidden'), true, 'feature workspace hides chat timeline');
assert.match(document.getElementById('feature-workspace').innerHTML, /Tools|automation surface|read-only|mutating/, 'tools surface renders desktop feature content');
assert.equal(document.getElementById('overview-surface').textContent, 'tools', 'overview follows selected feature');
ctx.setFeature('config');
assert.match(document.getElementById('feature-workspace').innerHTML, /Config|Permission|Search \/ fast/, 'config surface renders controls summary');
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
