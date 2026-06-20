import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

/* Minimal DOM mock sufficient to exercise the real chat-core app.js:
 * markdown rendering, incremental timeline, send/state/stream, working indicator.
 */

class ClassList {
  constructor(el) { this.el = el; this.set = new Set(); }
  add(...n) { for (const x of n) this.set.add(x); this._sync(); }
  remove(...n) { for (const x of n) this.set.delete(x); this._sync(); }
  contains(n) { return this.set.has(n); }
  toggle(n, force) { const on = force === undefined ? !this.set.has(n) : !!force; if (on) this.set.add(n); else this.set.delete(n); this._sync(); return on; }
  _sync() { this.el._className = [...this.set].join(' '); }
}

const VOID = new Set(['br','hr','img','input','meta','link']);

class Element {
  constructor(tag = 'div', id = '') {
    this.tagName = tag.toUpperCase(); this.id = id;
    this.children = []; this.parentElement = null;
    this.dataset = {}; this.style = {}; this.attributes = {};
    this.eventHandlers = {}; this.classList = new ClassList(this);
    this._className = ''; this._innerHTML = ''; this.textContent = '';
    this.value = ''; this.disabled = false;
    this.scrollTop = 0; this.scrollHeight = 1000; this.clientHeight = 500;
  }
  get className() { return this._className; }
  set className(v) { this._className = String(v || ''); this.classList.set = new Set(this._className.split(/\s+/).filter(Boolean)); }
  get innerHTML() { return this._innerHTML; }
  set innerHTML(v) {
    this._innerHTML = String(v || '');
    this.children = [];
    // Minimal parse of recognized elements so querySelector/querySelectorAll work.
    const re = /<(\w+)([^>]*)>([\s\S]*?)<\/\1>|<(\w+)([^>]*)\s*\/?>/g;
    let m;
    const stack = [this];
    const textRe = /<(\w+)((?:[^>])*?)>/g;
    // Tokenize tags.
    const tagRe = /<(\/?)(\w+)((?:[^>]*?))>/g;
    let tok;
    while ((tok = tagRe.exec(this._innerHTML)) !== null) {
      const [_, close, name, attrs] = tok;
      if (close) { if (stack.length > 1) stack.pop(); continue; }
      const el = new Element(name);
      // parse attributes incl data-* and class
      const attrRe = /(\b[\w-]+)(?:="([^"]*)")?/g;
      let am; const cls = [];
      while ((am = attrRe.exec(attrs)) !== null) {
        const [, k, val] = am;
        if (k === 'class') { if (val) el.className = val; }
        else if (k.startsWith('data-')) { el.dataset[k.slice(5)] = val === undefined ? '' : val; }
        else el.attributes[k] = val === undefined ? '' : val;
      }
      stack[stack.length - 1].appendChild(el);
      if (!/\/(?:>|$)/.test(tok[0]) && !VOID.has(name)) stack.push(el);
    }
  }
  appendChild(c) { c.parentElement = this; this.children.push(c); return c; }
  remove() { if (this.parentElement) { const i = this.parentElement.children.indexOf(this); if (i >= 0) this.parentElement.children.splice(i, 1); } this.parentElement = null; }
  get lastElementChild() { return this.children[this.children.length - 1] || null; }
  querySelector(sel) {
    return this._findBy(sel, false);
  }
  querySelectorAll(sel) { return this._findBy(sel, true); }
  _findBy(sel, all) {
    const out = [];
    const tokens = String(sel).trim().split(/\s+/); // descendant combinators
    let first = null;
    const walk = (el, depth) => {
      for (const c of el.children) {
        if (matches(c, tokens[depth])) {
          if (depth === tokens.length - 1) { if (all) out.push(c); else if (!first) first = c; }
          else { const deeper = walk(c, depth + 1); if (deeper && !all) return deeper; }
        }
        if (all || !first) { const deeper = walk(c, depth); if (deeper && !all && !first) first = deeper; }
        if (!all && first) return first;
      }
      return null;
    };
    walk(this, 0);
    return all ? out : (first || null);
  }
  addEventListener(t, cb) { (this.eventHandlers[t] ||= []).push(cb); }
  dispatchEvent(t, ev = {}) { for (const cb of this.eventHandlers[t] || []) cb(ev); }
  closest() { return this; }
  getBoundingClientRect() { return {top: 0, bottom: 100, height: 100}; }
  focus() { this.focused = true; }
  blur() { this.blurred = true; }
  requestSubmit() { if (typeof this.onsubmit === 'function') this.onsubmit({preventDefault(){}}); }
  setAttribute(n, v) { this.attributes[n] = String(v); }
  getAttribute(n) { return this.attributes[n]; }
}
function matches(el, sel) {
  if (!sel) return false;
  // Compound descendant selectors are split by the walker; match each token.
  // A token like '.message.assistant' = element with all those classes.
  const classes = [];
  let tag = '', id = '', attr = null, rest = sel;
  const idm = rest.match(/#([\w-]+)/); if (idm) { id = idm[1]; rest = rest.replace('#'+id, ''); }
  const tagm = rest.match(/^[a-zA-Z][\w-]*/); if (tagm) { tag = tagm[0]; rest = rest.slice(tag.length); }
  for (const cm of rest.matchAll(/\.([\w-]+)/g)) classes.push(cm[1]);
  const am = rest.match(/\[([\w-]+)(?:="([^"]*)")?\]/); if (am) attr = am;
  if (id && el.id !== id) return false;
  if (tag && el.tagName !== tag.toUpperCase()) return false;
  if (attr) {
    const [_, k, v] = attr;
    const key = k.startsWith('data-') ? k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase()) : k;
    if (v === undefined) { if (!(key in (el.dataset||{}))) return false; }
    else { if (el.dataset[key] !== v) return false; }
  }
  if (classes.length) { const have = (el._className || '').split(/\s+/); for (const c of classes) if (!have.includes(c)) return false; }
  return true;
}

class Document {
  constructor() { this.elements = new Map(); this.eventHandlers = {}; this.activeElement = null; this.body = new Element('body', 'body'); this.body.dataset = {}; }
  getElementById(id) { if (!this.elements.has(id)) this.elements.set(id, new Element('div', id)); return this.elements.get(id); }
  createElement(tag) { return new Element(tag); }
  addEventListener(t, cb) { (this.eventHandlers[t] ||= []).push(cb); }
}

function makeContext() {
  const document = new Document();
  const intervals = []; const timeouts = []; const eventSourceInstances = [];
  class EventSourceMock { constructor(url) { this.url = url; this.closed = false; this.handlers = {}; eventSourceInstances.push(this); } addEventListener(t, cb) { this.handlers[t] = cb; } close() { this.closed = true; } }
  const apiLog = [];
  let stateDB = {
    s1: {title: 'Session 1', provider: 'mock', model: 'm1', perm: 'gated', search: 'off', fast_ok: true, running: false, messages: [], pending: [], shells: [{id: 'sh1', command: 'sleep 10', status: 'running', last_line: 'tick'}], tools: [{name: 'bash', read_only: false}, {name: 'read', read_only: true}], roots: ['/tmp/project'], goal: 'ship gui'},
  };
  const ctx = {
    console, document,
    window: {CSS: {escape: s => String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&')}},
    queueMicrotask, EventSource: EventSourceMock,
    fetch: async (path, opts = {}) => {
      apiLog.push({path, opts});
      let data = {};
      if (path === '/api/health') data = {ok: true, socket: 'test', stats: {version: 'test', goroutines: 3}};
      else if (path === '/api/sessions') data = [{id: 's1', title: 'Session 1', status: 'idle', dir: '/tmp/project'}];
      else if (path === '/api/profile') data = {profile: 'hello'};
      else if (String(path).includes('/state')) {
        const id = String(path).split('/').slice(-2, -1)[0];
        data = JSON.parse(JSON.stringify(stateDB[id] || stateDB.s1));
      } else if (String(path).includes('/input')) {
        const id = String(path).split('/').slice(-2, -1)[0];
        const body = JSON.parse(opts.body || '{}');
        const arr = stateDB[id] || (stateDB[id] = {messages: []});
        arr.messages = arr.messages || [];
        arr.messages.push({role: 'user', text: body.text});
        arr.running = true;
        data = {steered: false};
      } else if (String(path).includes('/model') || String(path).includes('/perm') || String(path).includes('/effort') || String(path).includes('/search') || String(path).includes('/fast') || String(path).includes('/goal') || String(path).includes('/title')) {
        data = {ok: true};
      } else if (path === '/api/sessions' && opts.method === 'DELETE') {
        const body = JSON.parse(opts.body || '{}');
        delete stateDB[body.id];
        data = {ok: true};
      }
      return {ok: true, statusText: 'OK', text: async () => JSON.stringify(data)};
    },
    setInterval: (cb, ms) => { intervals.push({cb, ms, cleared: false}); return intervals.length; },
    clearInterval: id => { if (intervals[id - 1]) intervals[id - 1].cleared = true; },
    setTimeout: (cb, ms) => { timeouts.push({cb, ms}); queueMicrotask(cb); return timeouts.length; },
    clearTimeout: () => {},
    confirm: () => true,
    prompt: (msg, def) => def !== undefined ? `renamed-${def}` : 'a-new-goal',
    navigator: {clipboard: {writeText: async () => {}}},
    JSON, CSS: {escape: s => String(s)},
  };
  ctx.window.EventSource = EventSourceMock;
  ctx._stateDB = stateDB;
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
const {ctx, document, intervals, apiLog} = env;

const indexHTML = fs.readFileSync('internal/gui/static/index.html', 'utf8');
const stylesCSS = fs.readFileSync('internal/gui/static/styles.css', 'utf8');

/* ---------- Structure ---------- */
assert.match(indexHTML, /id="chat-stage"/, 'shell has a dedicated chat stage');
assert.match(indexHTML, /id="composer"/, 'shell has a composer form');
assert.match(indexHTML, /id="status-bar"/, 'shell has a status bar showing current setup');
assert.match(indexHTML, /id="status-indicator"/, 'shell has a working indicator');
assert.match(indexHTML, /id="feature-stage"/, 'shell has a separate feature stage so it never competes with chat layout');
assert.match(indexHTML, /id="rail-toggle"/, 'shell has a closable sidebar toggle');
assert.match(stylesCSS, /\.workspace\s*\{[\s\S]*grid-template-rows:\s*var\(--topbar-height\)\s+var\(--statusbar-height\)\s+minmax\(0, 1fr\);/, 'workspace uses a correct 3-row layout so composer can never be pushed off-canvas');
assert.match(stylesCSS, /\.chat-stage\s*\{[\s\S]*grid-template-rows:\s*auto\s+minmax\(0, 1fr\)\s+auto;/, 'chat stage pins the composer below the scroll area (goal bar + timeline + composer)');
assert.doesNotMatch(stylesCSS, /backdrop-filter/, 'removed GPU-heavy backdrop-filter that caused scroll lag');

/* ---------- Markdown rendering ---------- */
const md = ctx.renderMarkdown('Here is `inline` code:\n\n```go\nfunc main() {}\n```');
assert.match(md, /<code class="inline">inline<\/code>/, 'inline code renders');
assert.match(md, /<div class="code-block">[\s\S]*<pre[^>]*>func main\(\) \{\}<\/pre>/, 'fenced code blocks render inside a styled block');
assert.match(md, /data-copy-for="/, 'code blocks have a copy button');
const mdXss = ctx.renderMarkdown('<script>alert(1)</script>');
assert.doesNotMatch(mdXss, /<script>/i, 'raw HTML in markdown is escaped (no XSS)');
assert.match(mdXss, /&lt;script&gt;/, 'raw HTML is entity-escaped');

/* ---------- Open session + state sync ---------- */
await ctx.openSession('s1');
assert.equal(document.getElementById('title').textContent, 'Session 1', 'session title renders');
assert.match(document.getElementById('status-indicator').className, /idle/, 'status indicator reflects idle state');

/* ---------- Sending a message activates the session ---------- */
document.getElementById('input').value = 'please build it';
await ctx.openSession('s1'); // ensure active
const composer = document.getElementById('composer');
composer.onsubmit({preventDefault(){}});
await new Promise(r => setTimeout(r, 0));
assert(apiLog.some(x => String(x.path).includes('/input') && String(x.opts?.body || '').includes('please build it')), 'send posts to the input API');
assert(ctx._stateDB.s1.running === true, 'send flips the session to running in the shared state DB');

/* ---------- Running indicator + incremental timeline ---------- */
// Simulate the daemon reporting running + an assistant message via state poll.
ctx._stateDB.s1.running = true;
ctx._stateDB.s1.messages = [{role: 'user', text: 'hi'}, {role: 'assistant', text: 'working on it'}];
await ctx.refreshActiveState({force: true});
assert.match(document.getElementById('status-indicator').className, /running/, 'status indicator turns running when a turn is in flight');
assert.match(document.getElementById('status-text').textContent, /working/i, 'status text says the agent is working');
// Assistant message must be markdown-rendered, not raw text.
const tl = document.getElementById('timeline');
const bodies = tl.querySelectorAll('.message.assistant .body').map(b => b._innerHTML || '');
assert(bodies.some(h => h.includes('working on it')), 'assistant message body renders markdown content');

/* ---------- Incremental rendering: streaming survives a poll ---------- */
ctx.appendDelta('assistant', 'partial ');
const beforeKids = tl.children.length;
ctx.appendDelta('assistant', 'response');
await ctx.refreshActiveState({force: true}); // poll re-renders, must not wipe streaming node
assert.ok(tl.children.length >= beforeKids, 'poll does not rebuild the whole timeline (no flicker)');

/* ---------- Non-chat feature does not break chat layout ---------- */
ctx.setFeature('tools');
assert.equal(document.getElementById('workspace').dataset.feature, 'tools', 'feature switch sets workspace data attribute');
ctx.setFeature('chat');

/* ---------- Approvals ---------- */
ctx.appendEvent({kind: 'approval', result: 'ap1', tool: 'bash', text: 'echo ok'});
assert.ok(tl.children.some(c => c.dataset.approvalCard === 'ap1'), 'approval card renders in timeline');

/* ---------- Feature surfaces still render controls + call APIs ---------- */
ctx.setFeature('config');
document.getElementById('model-input').value = 'glm-5.2';
await ctx.runFeatureAction({dataset: {featureAction: 'apply-model'}});
assert(apiLog.some(x => String(x.path).includes('/model') && String(x.opts?.body || '').includes('glm-5.2')), 'config apply model calls settings API');
await ctx.runFeatureAction({dataset: {featureAction: 'toggle-fast'}});
assert(apiLog.some(x => String(x.path).includes('/fast')), 'config toggle fast calls settings API');
ctx.setFeature('approvals');
await ctx.runFeatureAction({dataset: {featureAction: 'approve', approvalId: 'ap1'}});
assert(apiLog.some(x => String(x.path).includes('/approve') && String(x.opts?.body || '').includes('true')), 'approval action calls approve API');
ctx.setFeature('shells');
await ctx.runFeatureAction({dataset: {featureAction: 'kill-shell', shellId: 'sh1'}});
assert(apiLog.some(x => String(x.path).includes('/kill-shell') && String(x.opts?.body || '').includes('sh1')), 'shell kill calls the kill-shell API');
ctx.setFeature('chat');

/* ---------- Goal bar + token usage + rename/delete (real backend wiring) ---------- */
// Goal bar appears when the session has a goal (s1 has goal 'ship gui').
assert.equal(document.getElementById('goal-bar').classList.contains('hidden'), false, 'goal bar shows when session has a goal');
assert.match(document.getElementById('goal-text').textContent, /ship gui/, 'goal bar shows the session goal');
assert.match(indexHTML, /id="goal-bar"/, 'shell has a goal bar');
assert.match(indexHTML, /id="token-usage"/, 'shell has a turn token-usage indicator');
assert.match(indexHTML, /id="rename-session"/, 'shell has a rename-session control');
assert.match(indexHTML, /id="delete-session"/, 'shell has a delete-session control');

// Token usage accumulates from streamed events.
ctx.appendEvent({kind: 'text', text: 'hi', in_toks: 100, out_toks: 50});
assert.equal(document.getElementById('token-usage').classList.contains('hidden'), false, 'token usage shows once events carry token counts');
const tuIn = document.getElementById('token-usage').querySelector('.tu-in');
assert.ok(tuIn && /100/.test(tuIn.textContent), 'token usage reflects input tokens');

// Rename calls the title API.
await document.getElementById('rename-session').onclick({preventDefault(){}});
await new Promise(r => setTimeout(r, 0));
assert(apiLog.some(x => String(x.path).includes('/title') && String(x.opts?.body || '').includes('renamed-')), 'rename posts to the session title API');

// Goal clear calls the goal API with empty value.
await document.getElementById('goal-clear').onclick({preventDefault(){}});
await new Promise(r => setTimeout(r, 0));
assert(apiLog.some(x => String(x.path).includes('/goal') && String(x.opts?.body || '').includes('"value":""')), 'goal clear posts empty goal to the goal API');

assert(apiLog.some(x => x.path === '/api/health'), 'health API called');
assert(apiLog.some(x => x.path === '/api/sessions'), 'sessions API called');

console.log(JSON.stringify({ok: true, intervals: intervals.length, apiCalls: apiLog.length}));
