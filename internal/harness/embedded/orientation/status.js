#!/usr/bin/env node
// Public status/project inventory for orientation state. Doctor explains the
// installed runtime; this command answers "what is indexed here?" and "which
// projects are indexed?" with the same filter vocabulary as query commands.

const fs = require('fs');
const path = require('path');
const { projectKey, projectKeyCandidates, inspectProject } = require('./project');
const { DATA_DIR, ALLOWLIST_FILE, ORIENTATION_HOME } = require('./state');
const { parseFilterArgs, hasFilters, filterEpisodes, describeFilters } = require('./filters');

function exists(p) { return fs.existsSync(p); }

function readJson(p) {
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); } catch { return null; }
}

function mtimeMs(p) {
  try { return fs.statSync(p).mtimeMs; } catch { return 0; }
}

function allowlist() {
  if (!exists(ALLOWLIST_FILE)) return [];
  return fs.readFileSync(ALLOWLIST_FILE, 'utf8')
    .split('\n')
    .map(s => s.trim())
    .filter(s => s && !s.startsWith('#'));
}

function prefixMatch(cwd, prefixes) {
  return prefixes.some(p => cwd === p || cwd.startsWith(p.endsWith('/') ? p : p + '/'));
}

function staleReason(rawPath, episodesPath, graphPath) {
  if (!exists(rawPath)) return null;
  if (!exists(episodesPath)) return 'missing episodes';
  if (!exists(graphPath)) return 'missing graph';
  const rawMtime = mtimeMs(rawPath);
  const graphMtime = mtimeMs(graphPath);
  return graphMtime && rawMtime > graphMtime + 1 ? 'raw newer than graph' : null;
}

function unique(values) {
  return [...new Set(values.filter(Boolean))].sort();
}

function latestTime(values) {
  const ts = values.map(v => Date.parse(v || '')).filter(t => !Number.isNaN(t));
  return ts.length ? new Date(Math.max(...ts)).toISOString() : null;
}

function episodeBranches(ep) {
  return [
    ep.gitBranch,
    ...(ep.branchHistory || []).map(b => b && b.branch),
  ];
}

function summarizeProject(dir, filters = {}) {
  const key = path.basename(dir);
  const manifest = readJson(path.join(dir, '.manifest.json')) || {};
  const rawPath = path.join(dir, 'raw.jsonl');
  const episodesPath = path.join(dir, 'episodes.json');
  const graphPath = path.join(dir, 'graph.json');
  const allEpisodes = readJson(episodesPath)?.episodes || [];

  if (
    filters.projectKey &&
    filters.projectKey !== key &&
    filters.projectKey !== manifest.projectKey &&
    filters.projectKey !== manifest.legacyProjectKey
  ) return null;
  if (filters.repoKey && filters.repoKey !== manifest.repoKey) return null;
  if (filters.worktree && filters.worktree !== manifest.worktreeCwd && filters.worktree !== manifest.cwd) return null;

  const filteredEpisodes = filterEpisodes(allEpisodes, filters);
  if (hasFilters(filters) && !filteredEpisodes.length) return null;
  const eps = hasFilters(filters) ? filteredEpisodes : allEpisodes;
  const stale = staleReason(rawPath, episodesPath, graphPath);
  const graph = readJson(graphPath) || {};

  return {
    projectKey: key,
    projectKeyVersion: manifest.projectKeyVersion,
    legacyProjectKey: manifest.legacyProjectKey,
    cwd: manifest.cwd,
    repoKey: manifest.repoKey,
    repoRoot: manifest.repoRoot,
    gitRemote: manifest.gitRemote,
    worktreeCwd: manifest.worktreeCwd,
    headSha: manifest.headSha,
    currentBranch: manifest.currentBranch,
    records: manifest.records,
    raw: exists(rawPath),
    rawMtime: mtimeMs(rawPath) || null,
    graphUpdated: graph.updated || null,
    graphMtime: mtimeMs(graphPath) || null,
    stale: !!stale,
    staleReason: stale,
    episodes: eps.length,
    totalEpisodes: allEpisodes.length,
    runtimes: unique(eps.map(e => e.runtime)),
    adapters: unique(eps.map(e => e.adapter)),
    branches: unique(eps.flatMap(episodeBranches)),
    lastEventTime: latestTime(eps.map(e => e.t)),
    cursorIngest: !!manifest.cursorIngest,
    lastCursorIngest: manifest.lastCursorIngest,
    lastFullRefresh: manifest.lastFullRefresh,
  };
}

function projectSummaries(filters = {}) {
  if (!exists(DATA_DIR)) return [];
  return fs.readdirSync(DATA_DIR)
    .filter(name => !name.startsWith('_'))
    .map(name => path.join(DATA_DIR, name))
    .filter(dir => {
      try { return fs.statSync(dir).isDirectory(); } catch { return false; }
    })
    .map(dir => summarizeProject(dir, filters))
    .filter(Boolean)
    .sort((a, b) => String(b.lastEventTime || b.graphUpdated || '').localeCompare(String(a.lastEventTime || a.graphUpdated || '')));
}

function cursorSummaries() {
  const dir = path.join(DATA_DIR, '_cursors');
  if (!exists(dir)) return [];
  return fs.readdirSync(dir)
    .filter(f => f.endsWith('.json'))
    .map(f => {
      const c = readJson(path.join(dir, f)) || {};
      let size = 0;
      try { size = fs.statSync(c.source).size; } catch {}
      return {
        file: f,
        runtime: c.runtime,
        adapter: c.adapter,
        session: c.session,
        source: c.source,
        cwd: c.cwd,
        repoKey: c.repoKey,
        worktreeCwd: c.worktreeCwd,
        line: c.line,
        offset: c.offset,
        size: c.size || size,
        lagBytes: Math.max(0, (size || c.size || 0) - (c.offset || 0)),
        updated: c.updated,
      };
    })
    .sort((a, b) => String(b.updated || '').localeCompare(String(a.updated || '')));
}

function lastIngestByRuntime(cursors) {
  const out = {};
  for (const c of cursors) {
    if (!c.runtime) continue;
    if (!out[c.runtime] || String(c.updated || '').localeCompare(String(out[c.runtime].updated || '')) > 0) {
      out[c.runtime] = { updated: c.updated, cwd: c.cwd, source: c.source, lagBytes: c.lagBytes };
    }
  }
  return out;
}

function printKV(label, value) {
  console.log(`${label.padEnd(18)} ${value}`);
}

function printStatus(status) {
  console.log('orientation status\n');
  printKV('cwd', status.cwd);
  printKV('state home', status.stateHome);
  printKV('allowlisted', status.allowlisted ? 'yes' : (status.allowlistEntries ? 'no' : 'no allowlist'));
  printKV('project key', status.identity.projectKey);
  if (status.identity.legacyProjectKey && status.identity.legacyProjectKey !== status.identity.projectKey) printKV('legacy key', status.identity.legacyProjectKey);
  printKV('repo key', status.identity.repoKey);
  if (status.identity.repoRoot) printKV('repo root', status.identity.repoRoot);
  if (status.identity.currentBranch) printKV('branch now', `${status.identity.currentBranch} (snapshot only)`);
  printKV('indexed', status.indexed ? 'yes' : 'no');
  if (status.project) {
    printKV('records', status.project.records ?? 'unknown');
    printKV('episodes', `${status.project.episodes}${status.project.episodes !== status.project.totalEpisodes ? `/${status.project.totalEpisodes}` : ''}`);
    printKV('runtimes', status.project.runtimes.join(', ') || '-');
    printKV('branches', status.project.branches.join(', ') || '-');
    printKV('graph updated', status.project.graphUpdated || 'missing');
    printKV('graph stale', status.project.stale ? `yes (${status.project.staleReason})` : 'no');
  }
  printKV('cursor files', status.cursors.length);
  printKV('lagging cursors', status.cursors.filter(c => c.lagBytes > 0).length);
  const filters = describeFilters(status.filters);
  if (filters) printKV('filters', filters);
}

function printProjects(projects, filters) {
  console.log('orientation projects\n');
  printKV('state home', ORIENTATION_HOME);
  printKV('projects', projects.length);
  const described = describeFilters(filters);
  if (described) printKV('filters', described);
  if (!projects.length) return;
  console.log('');
  for (const p of projects) {
    const stale = p.stale ? ` stale=${p.staleReason}` : '';
    const branches = p.branches.length ? ` branches=${p.branches.join(',')}` : '';
    const runtimes = p.runtimes.length ? ` runtimes=${p.runtimes.join(',')}` : '';
    console.log(`${p.projectKey} episodes=${p.episodes}/${p.totalEpisodes} records=${p.records ?? '?'}${stale}${runtimes}${branches} cwd=${p.cwd || '?'}`);
  }
}

function main() {
  const rawArgs = process.argv.slice(2);
  const json = rawArgs.includes('--json');
  const projectsMode = rawArgs.includes('--projects');
  const args = rawArgs.filter(a => a !== '--json' && a !== '--projects');
  const { positionals, filters } = parseFilterArgs(args);
  const cwd = positionals[0] || process.cwd();

  if (filters.sameRepo && !filters.repoKey) filters.repoKey = inspectProject(cwd).repoKey;

  if (projectsMode) {
    const projects = projectSummaries(filters);
    const payload = { stateHome: ORIENTATION_HOME, filters, count: projects.length, projects };
    if (json) console.log(JSON.stringify(payload, null, 2));
    else printProjects(projects, filters);
    return;
  }

  const prefixes = allowlist();
  const identity = inspectProject(cwd);
  const currentKeys = new Set(projectKeyCandidates(cwd));
  const projects = projectSummaries(filters);
  const current = projects.find(p => currentKeys.has(p.projectKey)) || null;
  const cursors = cursorSummaries().filter(c => c.cwd === cwd || c.worktreeCwd === cwd);
  const payload = {
    stateHome: ORIENTATION_HOME,
    cwd,
    filters,
    allowlistEntries: prefixes.length,
    allowlisted: prefixes.length ? prefixMatch(cwd, prefixes) : false,
    identity,
    indexed: !!current,
    project: current,
    cursors,
    lastIngestByRuntime: lastIngestByRuntime(cursorSummaries()),
  };

  if (json) console.log(JSON.stringify(payload, null, 2));
  else printStatus(payload);
}

main();
