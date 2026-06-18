#!/usr/bin/env node
// orientation doctor — explain the installed runtime state. This is intentionally
// read-only: it does not repair hooks or mutate cursors.

const fs = require('fs');
const path = require('path');
const { projectKey, projectKeyCandidates, inspectProject } = require('./project');
const { CLAUDE, CODEX, EIGEN, ENGINE_DIR, ORIENTATION_HOME, DATA_DIR, ALLOWLIST_FILE } = require('./state');

const ROOT = DATA_DIR;
const EIGEN_HOOKS = path.join(EIGEN, 'hooks.json');
const EIGEN_ORIENTATION = process.env.EIGEN_ORIENTATION_DIR || path.join(EIGEN, 'orientation');
const EIGEN_ENGINE_DIR = process.env.EIGEN_ORIENTATION_ENGINE_DIR || EIGEN_ORIENTATION;
const EIGEN_ORIENTATION_HOME = process.env.EIGEN_ORIENTATION_HOME || EIGEN_ORIENTATION;
const EIGEN_EVENTS = ['turn_done', 'session_stop', 'note'];

function exists(p) { return fs.existsSync(p); }

function readJson(p) {
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; }
}

function mtimeMs(p) {
  try { return fs.statSync(p).mtimeMs; } catch { return 0; }
}

function allowlist() {
  const f = ALLOWLIST_FILE;
  if (!exists(f)) return [];
  return fs.readFileSync(f, 'utf8').split('\n').map(s => s.trim()).filter(s => s && !s.startsWith('#'));
}

function prefixMatch(cwd, prefixes) {
  return prefixes.some(p => cwd === p || cwd.startsWith(p.endsWith('/') ? p : p + '/'));
}

function findHooks(settings) {
  const out = {
    SessionStart: { installed: false, cursor: false },
    Stop: { installed: false, cursor: false },
    PreCompact: { installed: false, cursor: false },
  };
  const hooks = (settings && settings.hooks) || {};
  for (const event of Object.keys(out)) {
    const commands = (hooks[event] || []).flatMap(group => group.hooks || [])
      .map(h => h.command)
      .filter(c => typeof c === 'string' && /(action-graph|ORIENTATION_HOME|ORIENTATION_ENGINE_DIR)/.test(c));
    out[event].installed = commands.length > 0;
    out[event].cursor = commands.some(c => c.includes('hook.js'));
  }
  return out;
}

function commandText(command) {
  if (Array.isArray(command)) return command.join(' ');
  return typeof command === 'string' ? command : '';
}

function isOrientationCommand(command) {
  return /(action-graph|ORIENTATION_HOME|ORIENTATION_ENGINE_DIR)/.test(commandText(command));
}

function findEigenHooks(config) {
  const out = Object.fromEntries(EIGEN_EVENTS.map(event => [event, { installed: false, disabled: false }]));
  const hooks = Array.isArray(config) ? config : (config && Array.isArray(config.hooks) ? config.hooks : []);
  for (const event of EIGEN_EVENTS) {
    const matches = hooks.filter(hook => hook && hook.event === event && isOrientationCommand(hook.command));
    out[event].installed = matches.length > 0;
    out[event].disabled = matches.some(hook => hook.disabled);
  }
  return out;
}

function projectSummaries() {
  if (!exists(ROOT)) return [];
  return fs.readdirSync(ROOT).filter(name => !name.startsWith('_')).map(name => {
    const dir = path.join(ROOT, name);
    if (!fs.statSync(dir).isDirectory()) return null;
    const manifest = readJson(path.join(dir, '.manifest.json')) || {};
    const rawPath = path.join(dir, 'raw.jsonl');
    const episodesPath = path.join(dir, 'episodes.json');
    const graphPath = path.join(dir, 'graph.json');
    const raw = exists(rawPath);
    const episodesFile = exists(episodesPath);
    const graphFile = exists(graphPath);
    const episodes = readJson(episodesPath);
    const graph = readJson(graphPath);
    const rawMtime = mtimeMs(rawPath);
    const graphMtime = mtimeMs(graphPath);
    let staleReason = null;
    if (raw && !episodesFile) staleReason = 'missing episodes';
    else if (raw && !graphFile) staleReason = 'missing graph';
    else if (raw && graphMtime && rawMtime > graphMtime + 1) staleReason = 'raw newer than graph';
    return {
      key: name,
      projectKeyVersion: manifest.projectKeyVersion,
      legacyProjectKey: manifest.legacyProjectKey,
      cwd: manifest.cwd,
      repoKey: manifest.repoKey,
      repoRoot: manifest.repoRoot,
      gitRemote: manifest.gitRemote,
      worktreeCwd: manifest.worktreeCwd,
      headSha: manifest.headSha,
      raw,
      rawMtime,
      graphMtime,
      episodes: episodes?.episodes?.length || 0,
      graphUpdated: graph?.updated || null,
      stale: !!staleReason,
      staleReason,
      records: manifest.records,
      cursorIngest: !!manifest.cursorIngest,
    };
  }).filter(Boolean);
}

function cursorSummaries() {
  const dir = path.join(ROOT, '_cursors');
  if (!exists(dir)) return [];
  return fs.readdirSync(dir).filter(f => f.endsWith('.json')).map(f => {
    const c = readJson(path.join(dir, f)) || {};
    let size = 0;
    try { size = fs.statSync(c.source).size; } catch {}
    return {
      file: f,
      runtime: c.runtime,
      session: c.session,
      cwd: c.cwd,
      repoKey: c.repoKey,
      worktreeCwd: c.worktreeCwd,
      line: c.line,
      offset: c.offset,
      size: c.size || size,
      lagBytes: Math.max(0, (size || c.size || 0) - (c.offset || 0)),
      resets: c.resets || 0,
      lastResetReason: c.lastResetReason,
      updated: c.updated,
    };
  });
}

function printKV(label, value) {
  console.log(`${label.padEnd(22)} ${value}`);
}

function main() {
  const args = process.argv.slice(2);
  const cwd = args.find(a => !a.startsWith('--')) || process.cwd();
  const hooksOnly = args.includes('--hooks');

  const settingsPath = path.join(CLAUDE, 'settings.json');
  const settings = readJson(settingsPath);
  const hooks = findHooks(settings);
  const eigenHooks = findEigenHooks(readJson(EIGEN_HOOKS));

  if (!hooksOnly) {
    console.log('orientation doctor\n');
    printKV('engine', exists(ENGINE_DIR) ? ENGINE_DIR : `missing (${ENGINE_DIR})`);
    printKV('state home', exists(ORIENTATION_HOME) ? ORIENTATION_HOME : `missing (${ORIENTATION_HOME})`);
    printKV('eigen engine', exists(EIGEN_ENGINE_DIR) ? EIGEN_ENGINE_DIR : `missing (${EIGEN_ENGINE_DIR})`);
    printKV('eigen state home', exists(EIGEN_ORIENTATION_HOME) ? EIGEN_ORIENTATION_HOME : `missing (${EIGEN_ORIENTATION_HOME})`);
    printKV('claude skill', exists(path.join(CLAUDE, 'skills', 'get-oriented', 'SKILL.md')) ? 'installed' : 'missing');
    printKV('codex skill', exists(path.join(CODEX, 'skills', 'get-oriented', 'SKILL.md')) ? 'installed' : 'missing');
    printKV('eigen skill', exists(path.join(EIGEN, 'skills', 'get-oriented', 'SKILL.md')) ? 'installed' : 'missing');
    printKV('settings', settings ? settingsPath : `missing/unreadable (${settingsPath})`);
    printKV('eigen hooks', exists(EIGEN_HOOKS) ? EIGEN_HOOKS : `missing (${EIGEN_HOOKS})`);
    printKV('projects.txt', exists(ALLOWLIST_FILE) ? ALLOWLIST_FILE : 'missing');

    const prefixes = allowlist();
    const identity = inspectProject(cwd);
    printKV('allowlist entries', prefixes.length);
    printKV('cwd allowlisted', prefixes.length ? (prefixMatch(cwd, prefixes) ? 'yes' : 'no') : 'no allowlist');
    printKV('cwd project key', projectKey(cwd));
    if (identity.legacyProjectKey && identity.legacyProjectKey !== identity.projectKey) printKV('cwd legacy key', identity.legacyProjectKey);
    printKV('cwd repo key', identity.repoKey);
    if (identity.repoRoot) printKV('cwd repo root', identity.repoRoot);
    if (identity.gitRemote) printKV('cwd git remote', identity.gitRemote);
    if (identity.currentBranch) printKV('cwd branch now', `${identity.currentBranch} (snapshot only)`);
    console.log('');
  }

  console.log('hooks');
  for (const event of ['SessionStart', 'Stop', 'PreCompact']) {
    const h = hooks[event];
    printKV(event, h.installed ? (h.cursor ? 'installed (cursor)' : 'installed') : 'missing');
  }
  for (const event of EIGEN_EVENTS) {
    const h = eigenHooks[event];
    printKV(`Eigen ${event}`, h.installed ? (h.disabled ? 'installed (disabled)' : 'installed (cursor)') : 'missing');
  }
  if (hooksOnly) return;

  const projects = projectSummaries();
  const cursors = cursorSummaries();
  console.log('\nindexes');
  printKV('indexed projects', projects.length);
  printKV('cursor files', cursors.length);
  const lastByRuntime = new Map();
  for (const c of cursors) {
    if (!c.runtime) continue;
    const prev = lastByRuntime.get(c.runtime);
    if (!prev || String(c.updated || '').localeCompare(String(prev.updated || '')) > 0) lastByRuntime.set(c.runtime, c);
  }
  for (const [runtime, cursor] of [...lastByRuntime.entries()].sort()) {
    printKV(`last ingest ${runtime}`, cursor.updated || 'unknown');
  }
  const lagging = cursors.filter(c => c.lagBytes > 0).length;
  const staleProjects = projects.filter(p => p.stale);
  printKV('lagging cursors', lagging);
  printKV('stale graphs', staleProjects.length);

  const currentKeys = new Set(projectKeyCandidates(cwd));
  const current = projects.find(p => currentKeys.has(p.key));
  if (current) {
    console.log('\ncurrent project');
    printKV('cwd', current.cwd || cwd);
    if (current.repoKey) printKV('repo key', current.repoKey);
    if (current.projectKeyVersion) printKV('key version', current.projectKeyVersion);
    if (current.legacyProjectKey) printKV('legacy key', current.legacyProjectKey);
    if (current.repoRoot) printKV('repo root', current.repoRoot);
    if (current.worktreeCwd) printKV('worktree cwd', current.worktreeCwd);
    if (current.gitRemote) printKV('git remote', current.gitRemote);
    if (current.headSha) printKV('head sha', current.headSha.slice(0, 12));
    printKV('raw', current.raw ? 'yes' : 'no');
    printKV('episodes', current.episodes);
    printKV('graph updated', current.graphUpdated || 'missing');
    printKV('graph stale', current.stale ? `yes (${current.staleReason})` : 'no');
    printKV('records', current.records ?? 'unknown');
  } else {
    console.log('\ncurrent project');
    printKV('indexed', 'no');
  }

  if (cursors.length) {
    console.log('\nrecent cursors');
    cursors
      .sort((a, b) => String(b.updated || '').localeCompare(String(a.updated || '')))
      .slice(0, 8)
      .forEach(c => {
        const lag = c.lagBytes ? ` lag=${c.lagBytes}B` : '';
        const reset = c.resets ? ` resets=${c.resets}${c.lastResetReason ? ` (${c.lastResetReason})` : ''}` : '';
        console.log(`  ${c.runtime || '?'} ${c.session || '?'} line=${c.line || 0} offset=${c.offset || 0}${lag}${reset} ${c.cwd || ''}`);
      });
  }

  if (staleProjects.length) {
    console.log('\nstale projects');
    staleProjects
      .slice(0, 8)
      .forEach(p => console.log(`  ${p.key} ${p.staleReason || 'stale'} ${p.cwd || ''}`));
  }
}

main();
