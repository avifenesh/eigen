#!/usr/bin/env node
// Runtime hook runner. Reads hook JSON from stdin, cursor-ingests the current
// transcript/session source, then rebuilds the affected project's derived
// indexes. Best-effort and quiet: hook failures must not block the user's
// session or harness.

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { projectKey } = require('./project');
const { EIGEN } = require('./state');
const { eigenMeta } = require('./adapters');

const HERE = __dirname;
const NODE = process.execPath;

function run(script, args, input) {
  return spawnSync(NODE, [path.join(HERE, script), ...args], {
    input,
    encoding: 'utf8',
    stdio: input == null ? 'ignore' : ['pipe', 'ignore', 'ignore'],
    env: process.env,
  });
}

function gitBranch(cwd) {
  if (!cwd) return null;
  const r = spawnSync('git', ['-C', cwd, 'branch', '--show-current'], { encoding: 'utf8' });
  if (r.status !== 0) return null;
  return r.stdout.trim() || null;
}

function valueAfter(args, name) {
  const prefix = `${name}=`;
  const inline = args.find(a => a.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const idx = args.indexOf(name);
  return idx === -1 ? undefined : args[idx + 1];
}

function argBool(args, name) {
  return args.includes(name) || args.includes(`${name}=true`);
}

function firstValue(obj, names) {
  for (const name of names) {
    if (obj && obj[name] != null && obj[name] !== '') return obj[name];
  }
  return null;
}

function normalizeRuntime(runtime, sourceKind) {
  const raw = String(runtime || 'claude').replace(/_/g, '-');
  if (raw === 'claude-code') return 'claude';
  if (raw === 'eigen' && sourceKind === 'task') return 'eigen-task';
  if (raw === 'eigen' && sourceKind === 'session') return 'eigen-session';
  return raw;
}

function isEigenRuntime(runtime) {
  return String(runtime || '').replace(/_/g, '-').startsWith('eigen');
}

function shouldIngestHook(hook, rawRuntime) {
  if (!isEigenRuntime(rawRuntime)) return true;
  const event = String(firstValue(hook, ['event', 'hookEvent', 'hook_event']) || '').replace(/_/g, '-').toLowerCase();
  if (event !== 'note') return true;
  const text = String(firstValue(hook, ['text', 'message', 'note']) || '').trim();
  if (!text) return false;
  const lower = text.toLowerCase();
  return lower.includes('compact') || lower === 'interrupted' || lower.startsWith('error: ');
}

function listDirs(root, predicate) {
  try {
    return fs.readdirSync(root)
      .map(name => path.join(root, name))
      .filter(p => {
        try { return fs.statSync(p).isDirectory() && predicate(path.basename(p)); } catch { return false; }
      });
  } catch {
    return [];
  }
}

function walkFiles(root, predicate, out = []) {
  if (!root || !fs.existsSync(root)) return out;
  for (const name of fs.readdirSync(root)) {
    const p = path.join(root, name);
    let st;
    try { st = fs.statSync(p); } catch { continue; }
    if (st.isDirectory()) walkFiles(p, predicate, out);
    else if (predicate(p, name, st)) out.push({ path: p, stat: st });
  }
  return out;
}

function eigenSessionRoots() {
  const roots = [path.join(EIGEN, 'sessions')];
  for (const dir of listDirs(EIGEN, name => name.startsWith('daemon'))) {
    roots.push(path.join(dir, 'sessions'));
  }
  return roots;
}

function eigenTaskRoots() {
  return listDirs(EIGEN, name => name.startsWith('tasks'));
}

function eigenSourceCwd(source) {
  const meta = eigenMeta(source);
  return meta.dir || meta.cwd || null;
}

function eigenSourceId(source) {
  const meta = eigenMeta(source);
  return String(meta.id || path.basename(source || '', '.jsonl').replace(/\.transcript$/, ''));
}

function matchesSession(source, wanted) {
  if (!wanted) return false;
  const id = eigenSourceId(source);
  const base = path.basename(source || '');
  return id === wanted || base === wanted || base.startsWith(`${wanted}.`) || base.startsWith(`${wanted}-`);
}

function findEigenSource(hook, sourceKind) {
  const wanted = String(firstValue(hook, ['session', 'sessionId', 'session_id', 'id']) || '');
  const kind = String(sourceKind || firstValue(hook, ['sourceKind', 'source_kind', 'kind']) || 'session').replace(/_/g, '-');
  const roots = kind === 'task' ? eigenTaskRoots() : eigenSessionRoots();
  const predicate = kind === 'task'
    ? (_p, name) => name.endsWith('.transcript.jsonl')
    : (p, name) => name.endsWith('.jsonl') && !name.endsWith('.meta.json') && p.includes(`${path.sep}sessions${path.sep}`);
  const candidates = roots.flatMap(root => walkFiles(root, predicate));
  const scoped = wanted ? candidates.filter(c => matchesSession(c.path, wanted)) : candidates;
  const pool = scoped.length ? scoped : candidates;
  pool.sort((a, b) => b.stat.mtimeMs - a.stat.mtimeMs);
  const chosen = pool[0];
  if (!chosen) return {};
  return {
    source: chosen.path,
    cwd: eigenSourceCwd(chosen.path),
    sourceKind: kind === 'task' ? 'task' : 'session',
  };
}

function main() {
  const cliArgs = process.argv.slice(2);
  let input = '';
  process.stdin.on('data', c => { input += c; });
  process.stdin.on('end', () => {
    let hook = {};
    try { hook = input.trim() ? JSON.parse(input) : {}; } catch { hook = {}; }

    let source = valueAfter(cliArgs, '--source') || firstValue(hook, [
      'source',
      'transcript_path',
      'transcriptPath',
      'transcript',
      'transcriptFile',
      'session_path',
      'sessionPath',
      'path',
    ]);
    let sourceKind = valueAfter(cliArgs, '--source-kind') || firstValue(hook, ['sourceKind', 'source_kind', 'kind']);
    const rawRuntime = valueAfter(cliArgs, '--runtime') || firstValue(hook, ['runtime', 'adapter']) || 'claude';
    if (!shouldIngestHook(hook, rawRuntime)) return;
    let runtime = normalizeRuntime(rawRuntime, sourceKind);
    let cwd = valueAfter(cliArgs, '--cwd') || firstValue(hook, ['cwd', 'dir', 'workdir', 'worktreeCwd']);
    if ((!source || !fs.existsSync(source)) && isEigenRuntime(rawRuntime)) {
      const inferred = findEigenSource(hook, sourceKind);
      source = inferred.source || source;
      sourceKind = sourceKind || inferred.sourceKind;
      cwd = cwd || inferred.cwd;
      runtime = normalizeRuntime(rawRuntime, sourceKind);
    }
    if (!cwd && isEigenRuntime(runtime)) cwd = eigenSourceCwd(source);
    if (!cwd && !isEigenRuntime(runtime)) cwd = process.cwd();
    if (!source || !fs.existsSync(source)) return;

    const args = ['--runtime', runtime, '--source', source, '--cursor'];
    if (cwd) args.push('--cwd', cwd);

    if (argBool(cliArgs, '--allow-unlisted') || hook.allowUnlisted || hook.allow_unlisted) args.push('--allow-unlisted');

    const branch = valueAfter(cliArgs, '--branch') || firstValue(hook, ['branch', 'gitBranch', 'currentBranch']) || gitBranch(cwd);
    const branchSource = valueAfter(cliArgs, '--branch-source') || firstValue(hook, ['branchSource', 'gitBranchSource', 'branch_source']) || (branch ? 'hook' : null);
    if (branch) args.push('--branch', branch, '--branch-source', branchSource);

    const ingested = run('ingest.js', args);
    if (ingested.status !== 0) return;
    if (!cwd) return;

    const key = projectKey(cwd);
    run('condense.js', [key]);
    run('graph.js', [key]);
  });
}

main();
