// Runtime transcript adapters. Each adapter turns a source-specific JSONL shape
// into the raw action-graph event schema consumed by condense.js.

const fs = require('fs');
const path = require('path');
const { classify, ok, cleanIntent, gistText, branchMarker } = require('./classify');

const ADAPTERS = {
  claude: { name: 'claude', runtime: 'claude' },
  codex: { name: 'codex', runtime: 'codex' },
  'eigen-session': { name: 'eigen-session', runtime: 'eigen', sourceKind: 'session' },
  'eigen-task': { name: 'eigen-task', runtime: 'eigen', sourceKind: 'task' },
};

function resolveAdapter(name = 'claude', source = '') {
  const raw = String(name || 'claude').replace(/_/g, '-');
  if (raw === 'eigen') return String(source || '').includes('/tasks/') ? ADAPTERS['eigen-task'] : ADAPTERS['eigen-session'];
  const adapter = ADAPTERS[raw];
  if (!adapter) throw new Error(`unsupported runtime/adapter: ${name}`);
  return adapter;
}

function parseRows(runtimeOrAdapter, rows, meta = {}) {
  const adapter = resolveAdapter(runtimeOrAdapter, meta.source);
  const nextMeta = {
    ...meta,
    runtime: adapter.runtime,
    adapter: adapter.name,
    sourceKind: meta.sourceKind || adapter.sourceKind,
  };
  if (adapter.name === 'claude') return parseClaudeRows(rows, nextMeta);
  if (adapter.name === 'codex') return parseCodexRows(rows, nextMeta);
  return parseEigenRows(rows, nextMeta);
}

function inferCwd(runtimeOrAdapter, rows, meta = {}) {
  const adapter = resolveAdapter(runtimeOrAdapter, meta.source);
  if (meta.cwd) return meta.cwd;
  if (adapter.name === 'claude') return rows.find(r => r && r.cwd)?.cwd || null;
  if (adapter.name === 'codex') {
    const sm = rows.find(r => r && r.type === 'session_meta' && r.payload);
    return sm?.payload?.cwd || null;
  }
  return eigenMeta(meta.source).dir || null;
}

function parseClaudeRows(rows, meta = {}) {
  const resultById = new Map();
  for (const d of rows) {
    if (d.type !== 'user') continue;
    const c = d.message && d.message.content;
    if (!Array.isArray(c)) continue;
    for (const b of c) if (b && b.type === 'tool_result') resultById.set(b.tool_use_id, b.content);
  }

  const out = [];
  let activeTodo = null;
  let pendingInterrupt = false;
  const state = {
    runtime: 'claude',
    adapter: meta.adapter || 'claude',
    ...branchState(meta),
  };
  const push = pusher(out, meta, state);

  for (const d of rows) {
    const t = d.timestamp || '';
    const session = (d.sessionId || meta.session || '').slice(0, 8);
    const rowMeta = { _row: d, cwd: d.cwd || meta.cwd };

    if (d.type === 'user') {
      if (isInterruption(d.message && d.message.content)) {
        push({ t, session, kind: 'interrupt', ...rowMeta });
        pendingInterrupt = true;
        continue;
      }
      const intent = userIntent(d.message && d.message.content);
      if (intent) {
        const rec = { t, session, kind: 'intent', text: intent.slice(0, 300), ...rowMeta };
        const br = branchMarker(intent);
        if (br) rec.flow = br;
        else if (pendingInterrupt) { rec.flow = 'steer'; rec.flowSrc = 'interrupt'; }
        push(rec);
        pendingInterrupt = false;
      }
      continue;
    }
    if (d.type !== 'assistant') continue;

    const blocks = (d.message && d.message.content) || [];
    const turn = (d.uuid || (d.message && d.message.id) || '').slice(-12);
    for (const b of blocks) {
      if (!b) continue;
      if (b.type === 'text') {
        const g = gistText(b.text);
        if (g) push({ t, session, turn, kind: 'say', text: g.text, cue: g.cue, ...rowMeta });
        continue;
      }
      if (b.type !== 'tool_use') continue;

      if (b.name === 'TodoWrite') {
        const todos = Array.isArray(b.input && b.input.todos) ? b.input.todos : [];
        const ip = todos.find(x => x && x.status === 'in_progress');
        activeTodo = ip ? ip.content : activeTodo;
        push({ t, session, turn, kind: 'todo', active: activeTodo, items: todos.length, ...rowMeta });
        continue;
      }

      const result = resultById.get(b.id);
      const rec = classify(b.name, b.input, result);
      rec.t = t; rec.session = session; rec.tool = b.name; rec.turn = turn;
      Object.assign(rec, rowMeta);
      if (activeTodo) rec.subgoal = activeTodo;
      if (!ok(result)) rec.failed = true;
      push(rec);
    }
  }
  return out;
}

function parseCodexRows(rows, meta = {}) {
  const sessionMeta = rows.find(r => r && r.type === 'session_meta' && r.payload)?.payload || {};
  const session = String(meta.session || sessionMeta.id || path.basename(meta.source || '', '.jsonl')).slice(0, 8);
  const defaultCwd = meta.cwd || sessionMeta.cwd || null;
  const runtimeBranch = branchFromMetadata(sessionMeta);
  const out = [];
  const outputs = codexOutputs(rows);
  const state = {
    runtime: 'codex',
    adapter: meta.adapter || 'codex',
    ...branchState(meta, runtimeBranch),
  };
  const push = pusher(out, { ...meta, cwd: defaultCwd }, state);

  for (const row of rows) {
    const payload = row.payload || {};
    const t = row.timestamp || payload.timestamp || sessionMeta.timestamp || '';
    const rowMeta = { _row: row, cwd: defaultCwd };
    const ptype = payload.type || '';

    if (ptype === 'message') {
      if (payload.role === 'user') {
        const intent = cleanIntent(messageText(payload.content));
        if (intent) push({ t, session, kind: 'intent', text: intent.slice(0, 300), ...rowMeta });
      } else if (payload.role === 'assistant') {
        const g = gistText(messageText(payload.content, ['output_text', 'text']));
        if (g) push({ t, session, kind: 'say', text: g.text, cue: g.cue, ...rowMeta });
      }
      continue;
    }

    if (ptype === 'function_call' || ptype === 'custom_tool_call') {
      const result = outputs.get(payload.call_id);
      const toolArgs = payload.arguments == null ? payload.input : payload.arguments;
      for (const rec of recordsForTool(payload.name, parseArgs(toolArgs), result)) {
        rec.t = t; rec.session = session; rec.tool = payload.name; rec.turn = payload.call_id;
        Object.assign(rec, rowMeta);
        if (!ok(result)) rec.failed = true;
        push(rec);
      }
    }
  }
  return out;
}

function parseEigenRows(rows, meta = {}) {
  const em = eigenMeta(meta.source);
  const session = String(meta.session || em.id || path.basename(meta.source || '', '.jsonl')).slice(0, 8);
  const defaultCwd = meta.cwd || em.dir || null;
  const sourceKind = meta.sourceKind || (String(meta.source || '').includes('/tasks/') ? 'task' : 'session');
  const runtimeBranch = branchFromMetadata(em);
  const out = [];
  const outputs = eigenOutputs(rows);
  const state = {
    runtime: 'eigen',
    adapter: meta.adapter || (sourceKind === 'task' ? 'eigen-task' : 'eigen-session'),
    ...branchState(meta, runtimeBranch),
  };
  const push = pusher(out, { ...meta, cwd: defaultCwd }, state);

  for (const row of rows) {
    const role = row.Role || row.role;
    const t = row.Timestamp || row.timestamp || meta.timestamp || '';
    const rowMeta = { _row: row, cwd: defaultCwd, sourceKind };

    if (role === 'user') {
      const intent = cleanIntent(row.Text || row.text || '');
      if (intent) push({ t, session, kind: 'intent', text: intent.slice(0, 300), ...rowMeta });
      continue;
    }
    if (role !== 'assistant') continue;

    const g = gistText(row.Text || row.text || '');
    if (g) push({ t, session, kind: 'say', text: g.text, cue: g.cue, ...rowMeta });
    const calls = Array.isArray(row.ToolCalls) ? row.ToolCalls : [];
    for (const call of calls) {
      const result = outputs.get(call.ID);
      for (const rec of recordsForTool(call.Name, call.Arguments, result)) {
        rec.t = t; rec.session = session; rec.tool = call.Name; rec.turn = call.ID;
        Object.assign(rec, rowMeta);
        if (!ok(result)) rec.failed = true;
        push(rec);
      }
    }
  }
  return out;
}

function branchState(meta = {}, runtimeBranch = null) {
  const activeGitBranch = meta.gitBranch || runtimeBranch || null;
  const activeGitBranchSource = meta.gitBranch
    ? (meta.gitBranchSource || 'cli')
    : (runtimeBranch ? 'runtime' : null);
  return { activeGitBranch, activeGitBranchSource };
}

function branchFromMetadata(...objects) {
  for (const obj of objects) {
    const branch = nestedValue(obj, [
      'gitBranch',
      'git_branch',
      'currentBranch',
      'current_branch',
      'branch',
      'git.branch',
      'vcs.branch',
      'repository.branch',
    ]);
    if (branch) return String(branch).trim();
  }
  return null;
}

function nestedValue(obj, names) {
  if (!obj || typeof obj !== 'object') return null;
  for (const name of names) {
    const parts = name.split('.');
    let cur = obj;
    for (const part of parts) {
      if (!cur || typeof cur !== 'object' || cur[part] == null || cur[part] === '') {
        cur = null;
        break;
      }
      cur = cur[part];
    }
    if (cur != null && cur !== '') return cur;
  }
  return null;
}

function pusher(out, meta, state) {
  return function push(rec) {
    rec.runtime = meta.runtime || state.runtime;
    rec.adapter = meta.adapter || state.adapter;
    if (meta.sourceKind && rec.sourceKind == null) rec.sourceKind = meta.sourceKind;
    rec.source = meta.source;
    for (const [k, v] of Object.entries(meta.identity || {})) {
      if (v != null && rec[k] == null) rec[k] = v;
    }
    if (rec.sourceLine == null && rec._row) rec.sourceLine = rec._row.__sourceLine;
    if (rec.sourceOffset == null && rec._row) rec.sourceOffset = rec._row.__sourceOffset;
    delete rec._row;
    if (state.activeGitBranch && !rec.gitBranch) {
      rec.gitBranch = state.activeGitBranch;
      rec.gitBranchSource = state.activeGitBranchSource;
    }
    if (rec.gitBranch) {
      state.activeGitBranch = rec.gitBranch;
      state.activeGitBranchSource = rec.gitBranchSource || state.activeGitBranchSource;
    }
    if (rec.cwd == null) rec.cwd = meta.cwd;
    out.push(rec);
  };
}

function recordsForTool(name, args, result) {
  const raw = String(name || '');
  const obj = args && typeof args === 'object' ? args : {};
  if (raw === 'multi_tool_use.parallel' || raw.endsWith('.parallel')) {
    const uses = Array.isArray(obj.tool_uses) ? obj.tool_uses : [];
    return uses.flatMap(u => recordsForTool(u.recipient_name || u.name, u.parameters || u.arguments || {}, result));
  }

  const n = raw.split('.').pop();
  if (n === 'exec_command' || n === 'bash' || n === 'shell') {
    const command = obj.cmd || obj.command || '';
    const description = obj.justification || obj.description || obj.reason;
    return [classify('Bash', { command, description }, result)];
  }
  if (n === 'apply_patch') return [patchRecord(args)];
  if (n === 'Edit' || n === 'edit' || n === 'MultiEdit') return [classify('Edit', fileArgs(obj), result)];
  if (n === 'Write' || n === 'write') return [classify('Write', fileArgs(obj), result)];
  if (n === 'Read' || n === 'read' || n === 'view_image') return [classify('Read', fileArgs(obj), result)];
  if (n === 'Grep' || n === 'grep' || n === 'rg') return [classify('Grep', fileArgs(obj), result)];
  if (n === 'Glob' || n === 'glob' || n === 'tree' || n === 'ls') return [{ kind: 'explore', files: [obj.path || obj.workdir].filter(Boolean), q: obj.pattern || obj.query || n }];
  if (n === 'TodoWrite' || n === 'update_plan' || n === 'todo') return [todoRecord(obj)];
  if (n === 'Task' || n === 'task' || n === 'Agent') return [classify('Task', { subagent_type: obj.subagent_type || obj.kind, description: obj.description || obj.task }, result)];
  if (n === 'Skill' || n === 'skill') return [classify('Skill', { skill: obj.skill || obj.name }, result)];
  return [{ kind: 'other', tool: raw }];
}

function fileArgs(args) {
  const f = args.file_path || args.path || args.file || args.notebook_path;
  return { ...args, file_path: f, path: args.path || f, pattern: args.pattern || args.query };
}

function patchRecord(args) {
  const patch = typeof args === 'string' ? args : (args.patch || args.input || args.text || '');
  return { kind: 'edit', files: filesFromPatch(String(patch)) };
}

function filesFromPatch(patch) {
  const files = new Set();
  for (const line of patch.split(/\r?\n/)) {
    let m = /^\*\*\* (?:Update|Add|Delete) File:\s+(.+)$/.exec(line);
    if (m) { files.add(m[1].trim()); continue; }
    m = /^diff --git a\/(.+?) b\/(.+)$/.exec(line);
    if (m) { files.add(m[2].trim()); continue; }
    m = /^\+\+\+ b\/(.+)$/.exec(line);
    if (m) files.add(m[1].trim());
  }
  return [...files];
}

function todoRecord(args) {
  const todos = Array.isArray(args.todos) ? args.todos : [];
  const active = todos.find(t => t && t.status === 'in_progress')?.content
    || args.explanation
    || args.task;
  return { kind: 'todo', active, items: todos.length || undefined };
}

function codexOutputs(rows) {
  const m = new Map();
  for (const row of rows) {
    const p = row.payload || {};
    if (p.type === 'function_call_output' || p.type === 'custom_tool_call_output') {
      m.set(p.call_id, p.output);
    }
  }
  return m;
}

function eigenOutputs(rows) {
  const m = new Map();
  for (const row of rows) {
    if ((row.Role || row.role) === 'tool' && row.ToolCallID) m.set(row.ToolCallID, row.Text || '');
  }
  return m;
}

function parseArgs(value) {
  if (value == null) return {};
  if (typeof value === 'object') return value;
  const s = String(value);
  try { return JSON.parse(s); } catch { return s; }
}

function messageText(content, wanted = ['input_text', 'output_text', 'text']) {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  return content
    .filter(b => b && wanted.includes(b.type))
    .map(b => b.text || '')
    .filter(Boolean)
    .join('\n')
    .trim();
}

function eigenMeta(source) {
  if (!source) return {};
  const candidates = [];
  if (source.endsWith('.transcript.jsonl')) candidates.push(source.replace(/\.transcript\.jsonl$/, '.jsonl'));
  if (source.endsWith('.jsonl')) {
    candidates.push(`${source}.meta.json`);
    candidates.push(source.replace(/\.jsonl$/, '.meta.json'));
  }
  for (const f of [...new Set(candidates)]) {
    try {
      const o = JSON.parse(fs.readFileSync(f, 'utf8').split('\n').find(Boolean) || '{}');
      if (f.endsWith('.meta.json')) return o;
      if (o.id || o.started || o.dir || o.cwd) return { id: o.id, timestamp: o.started, dir: o.dir || o.cwd };
    } catch {}
  }
  return {};
}

function isInterruption(content) {
  if (Array.isArray(content)) {
    return content.some(b => b && b.type === 'text' && /^\[Request interrupted/.test(b.text || ''));
  }
  return typeof content === 'string' && /^\[Request interrupted/.test(content);
}

function userIntent(content) {
  if (typeof content === 'string') return cleanIntent(content);
  if (Array.isArray(content)) {
    if (content.some(b => b && b.type === 'tool_result')) return null;
    const textBlk = content.find(b => b && b.type === 'text');
    return textBlk ? cleanIntent(textBlk.text) : null;
  }
  return null;
}

module.exports = { ADAPTERS, resolveAdapter, parseRows, inferCwd, filesFromPatch, eigenMeta };
