#!/usr/bin/env node
// action-graph graph — derives the actual graph from episodes.json.
// Flat episodes are the log; THIS is the graph. Nodes + edges that mean something.
//
// Nodes:
//   file:<path>     an artifact that was edited
//   intent:<n>      a goal (indexed; text in .label)
// Edges (directed unless noted):
//   intent --touched--> file        goal changed this file
//   file   --coupled-- file         co-edited in same episode (undirected, weighted)
//   intent --outcome--> committed|failed|explored
//
// Coupling is the edge that beats grep: "you're editing X — last 3 times X changed,
// Y changed too." Pure derivation, no model.

const fs = require('fs');
const path = require('path');
const { parseFilterArgs, filterEpisodes, hasFilters, describeFilters } = require('./filters');
const { projectKeyCandidates, inspectProject } = require('./project');
const { DATA_DIR } = require('./state');

const ROOT = DATA_DIR;

function loadEpisodes(dir) {
  const f = path.join(dir, 'episodes.json');
  if (!fs.existsSync(f)) return null;
  try { return JSON.parse(fs.readFileSync(f, 'utf8')).episodes || []; } catch { return null; }
}

function readJson(f) {
  try { return JSON.parse(fs.readFileSync(f, 'utf8')); } catch { return null; }
}

function projectDirs() {
  if (!fs.existsSync(ROOT)) return [];
  return fs.readdirSync(ROOT)
    .filter(name => !name.startsWith('_'))
    .map(name => path.join(ROOT, name))
    .filter(d => {
      try { return fs.statSync(d).isDirectory(); } catch { return false; }
    });
}

function manifestForDir(dir) {
  return readJson(path.join(dir, '.manifest.json')) || {};
}

function exactProjectDirs(cwd) {
  return projectKeyCandidates(cwd)
    .map(key => path.join(ROOT, key))
    .filter((dir, index, dirs) => dirs.indexOf(dir) === index && fs.existsSync(dir));
}

function targetRepoKey(cwd) {
  for (const dir of exactProjectDirs(cwd)) {
    const exact = manifestForDir(dir);
    if (exact.repoKey) return exact.repoKey;
  }
  return inspectProject(cwd).repoKey;
}

function loadEpisodeSet(cwd, filters = {}) {
  if (filters.sameRepo) {
    const repoKey = filters.repoKey || targetRepoKey(cwd);
    const episodes = [];
    const projects = [];
    for (const dir of projectDirs()) {
      const manifest = manifestForDir(dir);
      if (manifest.repoKey !== repoKey) continue;
      const eps = loadEpisodes(dir);
      if (!eps) continue;
      projects.push({ key: path.basename(dir), cwd: manifest.cwd, repoKey, worktreeCwd: manifest.worktreeCwd });
      episodes.push(...eps);
    }
    if (!episodes.length) return null;
    return { cwd, repoKey, projects, episodes };
  }
  const episodes = [];
  const projects = [];
  for (const dir of exactProjectDirs(cwd)) {
    const eps = loadEpisodes(dir);
    if (!eps) continue;
    const manifest = manifestForDir(dir);
    projects.push({ key: path.basename(dir), cwd: manifest.cwd || cwd, repoKey: manifest.repoKey, worktreeCwd: manifest.worktreeCwd });
    episodes.push(...eps);
  }
  return episodes.length ? { cwd, projects, episodes } : null;
}

function buildGraph(episodes) {
  const nodes = new Map(); // id -> {id, type, label, count}
  const couple = new Map(); // "a\0b" (sorted) -> weight
  const touched = new Map(); // intentId -> Set(file)
  const outcomes = []; // {intent, outcome}

  function node(id, type, label) {
    const n = nodes.get(id) || { id, type, label, count: 0 };
    n.count++;
    nodes.set(id, n);
    return n;
  }

  episodes.forEach((ep, idx) => {
    const files = ep.filesTouched || [];
    files.forEach(f => node('file:' + f, 'file', f));

    if (ep.intent) {
      const iid = 'intent:' + idx;
      node(iid, 'intent', ep.intent);
      touched.set(iid, new Set(files));

      const committed = (ep.runs || []).some(r => r.kind === 'commit');
      const failed = (ep.deadEnds || 0) > 0 && !committed;
      outcomes.push({ intent: iid, outcome: committed ? 'committed' : failed ? 'failed' : files.length ? 'edited' : 'explored' });
    }

    // coupling: every unordered pair of files edited together gains weight
    for (let a = 0; a < files.length; a++) {
      for (let b = a + 1; b < files.length; b++) {
        const key = [files[a], files[b]].sort().join('\0');
        couple.set(key, (couple.get(key) || 0) + 1);
      }
    }
  });

  const edges = [];
  for (const [iid, set] of touched) for (const f of set) edges.push({ from: iid, to: 'file:' + f, rel: 'touched' });
  for (const [key, w] of couple) { const [a, b] = key.split('\0'); edges.push({ from: 'file:' + a, to: 'file:' + b, rel: 'coupled', weight: w }); }
  for (const o of outcomes) edges.push({ from: o.intent, to: 'outcome:' + o.outcome, rel: 'outcome' });

  // RESUME edges: goal_i --resumes--> goal_j when they share files, threading the
  // same work across detours. Text can't find these (deictic prompts like "ok
  // continue"); file footprint can. IDF down-weights hub files (settings.rs touched
  // by 20 goals = weak connector) so only meaningful overlap threads.
  resumeEdges(touched, couple, edges);

  return { updated: new Date().toISOString(), nodes: [...nodes.values()], edges };
}

// Resume scoring = COSINE over IDF-weighted file vectors (hub files excluded).
// Evolution (all tested on real data):
//   Jaccard          → hub-only overlap scores 1.0 (IDF cancels in ratio). dead.
//   IDF mass (sum)    → rewards big footprints; two 40-file sweeps "resume" each
//                       other by breadth, not by being the same thread. dead.
//   IDF cosine        → length-normalized, so footprint size cancels. A focused
//                       2-file goal sharing both beats two broad goals sharing 5.
// Hub filter stays: a file in >25% of goals (settings.rs) is a stopword connector.
// cosine threshold — calibrated on TWO projects (codex-desktop-linux N=74,
// agent-workspace-linux N=27). Below 0.45 = incidental doc/hub overlap junk
// ("you chose" ↔ * via MEMORY.md); at/above = real resumes (viewer.rs thread,
// patch.js update-button cluster). Mass was rejected: it rewards footprint size,
// anti-correlating with quality (87-mass sweeps scored cos 0.58, real 2.5-mass
// viewer threads scored 0.65). Cosine alone, footprint-invariant, generalizes.
const RESUME_MIN = 0.45;
const HUB_MIN_GOALS = 4;       // absolute floor — see isHub
const PASSENGER_RATIO = 0.8;   // co-edit ratio above which a file is a "passenger"
function resumeEdges(touched, couple, edges) {
  const goals = [...touched.entries()].filter(([, s]) => s.size); // [iid, Set(files)]
  const N = goals.length;
  const df = new Map();
  for (const [, set] of goals) for (const f of set) df.set(f, (df.get(f) || 0) + 1);

  // Passenger files: almost never edited alone — dragged along with whatever else
  // changes (a CHANGELOG, a coverage/tracking doc, a generated schema). Detected by
  // max co-edit ratio = max(coEdits(f,*)) / df(f) >= PASSENGER_RATIO. They connect
  // everything → connect nothing, same as a hub but invisible to the df cutoff.
  // Validated on agnix: excluding them dropped 23→5 threads, all 20 removed were
  // junk via tool-release-baselines.json, all 5 kept were real. Source-free signal
  // (pure co-edit), so it survives deleted worktrees where an import graph can't.
  const maxCo = new Map();
  for (const [key, w] of couple) {
    const [a, b] = key.split('\0'); // couple keys are NUL-joined (filenames may contain spaces)
    const ra = w / (df.get(a) || 1), rb = w / (df.get(b) || 1);
    if (ra > (maxCo.get(a) || 0)) maxCo.set(a, ra);
    if (rb > (maxCo.get(b) || 0)) maxCo.set(b, rb);
  }
  const isPassenger = f => (df.get(f) || 0) >= HUB_MIN_GOALS && (maxCo.get(f) || 0) >= PASSENGER_RATIO;

  // Hub = stopword-tier connector file. Pure percentage (>25%) breaks at low N:
  // a file in 2 of 6 goals is NOT a hub but trips 25%. Require BOTH >25% AND an
  // absolute floor of HUB_MIN_GOALS, so small projects keep their real connectors.
  const isHub = f => { const c = df.get(f) || 0; return (c > N * 0.25 && c >= HUB_MIN_GOALS) || isPassenger(f); };
  const idf = f => Math.log((N + 1) / ((df.get(f) || 0) + 1)) + 1;

  // IDF vector per goal over non-hub files, plus its norm
  const vecs = goals.map(([, set]) => {
    const v = new Map();
    for (const f of set) if (!isHub(f)) v.set(f, idf(f));
    let norm = 0; for (const w of v.values()) norm += w * w;
    return { v, norm: Math.sqrt(norm) };
  });

  const idx = i => parseInt(goals[i][0].split(':')[1], 10);
  for (let a = 0; a < goals.length; a++) {
    const A = vecs[a]; if (!A.norm) continue;
    for (let b = a + 1; b < goals.length; b++) {
      if (Math.abs(idx(b) - idx(a)) < 3) continue; // adjacent = same continuous work
      const B = vecs[b]; if (!B.norm) continue;
      let dot = 0;
      const [small, big] = A.v.size <= B.v.size ? [A.v, B.v] : [B.v, A.v];
      for (const [f, w] of small) { const w2 = big.get(f); if (w2) dot += w * w2; }
      if (!dot) continue;
      const cos = dot / (A.norm * B.norm);
      if (cos >= RESUME_MIN) {
        const shared = [...A.v.keys()].filter(f => B.v.has(f)).sort((x, y) => idf(y) - idf(x));
        edges.push({ from: goals[a][0], to: goals[b][0], rel: 'resumes', weight: +cos.toFixed(2), via: shared.slice(0, 3) });
      }
    }
  }
}

// Query: files coupled with a given file, ranked by co-edit weight.
function coupledWith(graph, file) {
  return graph.edges
    .filter(e => e.rel === 'coupled' && (e.from === 'file:' + file || e.to === 'file:' + file))
    .map(e => ({ file: (e.from === 'file:' + file ? e.to : e.from).replace('file:', ''), weight: e.weight }))
    .sort((a, b) => b.weight - a.weight);
}

function makeMatcher(file, known) {
  const base = file.split('/').slice(-2).join('/');
  const bare = file.split('/').pop();
  const exactExists = known.has(base);
  const match = f => exactExists ? f === base : (f === base || f.endsWith('/' + bare) || f === bare);
  return { match, base };
}

function graphForQuery(cwd, episodes, filters = {}) {
  if (!hasFilters(filters)) {
    const dirs = exactProjectDirs(cwd)
      .filter(dir => fs.existsSync(path.join(dir, 'graph.json')));
    if (dirs.length === 1) {
      const cached = readJson(path.join(dirs[0], 'graph.json'));
      if (cached) return { graph: cached, source: 'cached' };
    }
  }
  return { graph: buildGraph(episodes), source: 'temporary' };
}

function coupledWithMatcher(graph, match) {
  const matched = new Set((graph.nodes || [])
    .filter(n => n.type === 'file' && match(n.label))
    .map(n => n.id));
  if (!matched.size) return { matchedFiles: [], hits: [] };

  const byFile = new Map();
  for (const e of graph.edges || []) {
    if (e.rel !== 'coupled') continue;
    const fromMatch = matched.has(e.from);
    const toMatch = matched.has(e.to);
    if (fromMatch && toMatch) continue;
    const other = fromMatch ? e.to : toMatch ? e.from : null;
    if (!other) continue;
    const file = other.replace('file:', '');
    byFile.set(file, (byFile.get(file) || 0) + (e.weight || 0));
  }

  return {
    matchedFiles: [...matched].map(id => id.replace('file:', '')),
    hits: [...byFile.entries()]
      .map(([file, weight]) => ({ file, weight }))
      .sort((a, b) => b.weight - a.weight),
  };
}

function couplingSupport(episodes, match, coupledFile, limit = 3) {
  return episodes
    .filter(ep => {
      const files = ep.filesTouched || [];
      return files.includes(coupledFile) && files.some(match);
    })
    .sort((a, b) => String(b.t || '').localeCompare(String(a.t || '')))
    .slice(0, limit)
    .map(episodeSummary);
}

function episodeForIntent(episodes, id) {
  const idx = Number(String(id || '').split(':')[1]);
  return Number.isInteger(idx) ? episodes[idx] : null;
}

function episodeSummary(ep) {
  if (!ep) return null;
  return {
    t: ep.t,
    id: ep.id,
    session: ep.session,
    runtime: ep.runtime,
    adapter: ep.adapter,
    sourceKind: ep.sourceKind,
    gitBranch: ep.gitBranch,
    gitBranchSource: ep.gitBranchSource,
    branchHistory: ep.branchHistory || [],
    projectKey: ep.projectKey,
    projectKeyVersion: ep.projectKeyVersion,
    legacyProjectKey: ep.legacyProjectKey,
    repoKey: ep.repoKey,
    worktreeCwd: ep.worktreeCwd,
    intent: ep.intent,
    filesTouched: ep.filesTouched || [],
    sources: ep.sources || [],
    evidence: ep.evidence || [],
  };
}

function evidenceLine(ep) {
  if (!ep) return '';
  const ev = (ep.evidence || []).find(x => x.source) || {};
  const loc = ev.source ? `${ev.source}${ev.sourceLine ? `:${ev.sourceLine}` : ''}` : null;
  return [
    ep.id && `goal=${ep.id}`,
    ep.runtime,
    ep.session && `session=${ep.session}`,
    ep.gitBranch && `branch=${ep.gitBranch}${ep.gitBranchSource ? `/${ep.gitBranchSource}` : ''}`,
    ep.worktreeCwd && `worktree=${ep.worktreeCwd}`,
    loc,
  ].filter(Boolean).join(' ');
}

function threadResults(graph, episodes) {
  return (graph.edges || [])
    .filter(e => e.rel === 'resumes')
    .sort((a, b) => b.weight - a.weight)
    .map(e => ({
      weight: e.weight,
      via: e.via || [],
      from: episodeSummary(episodeForIntent(episodes, e.from)),
      to: episodeSummary(episodeForIntent(episodes, e.to)),
    }));
}

function printJson(obj) {
  process.stdout.write(JSON.stringify(obj, null, 2) + '\n');
}

function splitRenderArgs(argv) {
  return { json: argv.includes('--json'), argv: argv.filter(a => a !== '--json') };
}

function printFilterNote(filters) {
  if (hasFilters(filters)) console.log(`Filters: ${describeFilters(filters)}\n`);
}

function coupledMode(cwd, file, filters = {}, opts = {}) {
  const data = loadEpisodeSet(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'coupled', cwd, file, filters, status: 'no_project', count: 0, coupled: [] });
    else console.log('(no graph yet)');
    return;
  }
  const episodes = filterEpisodes(data.episodes, filters);
  if (!episodes.length) {
    if (opts.json) printJson({ type: 'coupled', cwd, file, filters, status: 'no_match', count: 0, coupled: [] });
    else console.log(`(no filtered history for ${file})`);
    return;
  }
  const { graph, source } = graphForQuery(cwd, episodes, filters);
  const known = new Set([
    ...episodes.flatMap(e => e.filesTouched || []),
    ...((graph.nodes || []).filter(n => n.type === 'file').map(n => n.label)),
  ]);
  const { match } = makeMatcher(file, known);
  const result = coupledWithMatcher(graph, match);
  const coupled = result.hits.map(hit => ({
    ...hit,
    supportingGoals: couplingSupport(episodes, match, hit.file),
  }));

  if (opts.json) {
    printJson({
      type: 'coupled',
      cwd,
      file,
      filters,
      graphSource: source,
      status: result.hits.length ? 'ok' : 'no_match',
      matchedFiles: result.matchedFiles,
      count: coupled.length,
      coupled,
    });
    return;
  }

  printFilterNote(filters);
  if (!coupled.length) { console.log(`(no files coupled with ${file})`); return; }
  console.log(`Files co-edited with ${file}${source === 'temporary' ? ' in filtered history' : ''}:`);
  for (const hit of coupled.slice(0, 12)) {
    console.log(`  ${hit.file}  (${hit.weight}x)`);
    const support = hit.supportingGoals[0];
    const why = evidenceLine(support);
    if (why) console.log(`      why: ${why}`);
    if (support?.intent) console.log(`      goal: ${support.intent.slice(0, 76)}`);
  }
}

function threadsMode(cwd, filters = {}, opts = {}) {
  const data = loadEpisodeSet(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'threads', cwd, filters, status: 'no_project', count: 0, threads: [] });
    else console.log('(no graph yet)');
    return;
  }
  const episodes = filterEpisodes(data.episodes, filters);
  if (!episodes.length) {
    if (opts.json) printJson({ type: 'threads', cwd, filters, status: 'no_match', count: 0, threads: [] });
    else console.log('(no filtered history)');
    return;
  }
  const { graph, source } = graphForQuery(cwd, episodes, filters);
  const res = threadResults(graph, episodes);

  if (opts.json) {
    printJson({
      type: 'threads',
      cwd,
      filters,
      graphSource: source,
      status: res.length ? 'ok' : 'no_match',
      count: res.length,
      threads: res,
    });
    return;
  }

  printFilterNote(filters);
  if (!res.length) { console.log('(no resume threads found)'); return; }
  console.log(`${res.length} resume thread(s) — goals rejoined across detours${source === 'temporary' ? ' in filtered history' : ''}:\n`);
  for (const e of res.slice(0, 12)) {
    const fromWhy = evidenceLine(e.from);
    const toWhy = evidenceLine(e.to);
    console.log(`  ${e.weight}  via ${e.via.join(', ')}`);
    console.log(`     ↩ ${(e.from?.intent || '').slice(0, 70)}`);
    if (fromWhy) console.log(`       why: ${fromWhy}`);
    console.log(`     ↪ ${(e.to?.intent || '').slice(0, 70)}`);
    if (toWhy) console.log(`       why: ${toWhy}`);
    console.log('');
  }
}

function buildProject(dir) {
  const eps = loadEpisodes(dir);
  if (!eps) return null;
  const g = buildGraph(eps);
  fs.writeFileSync(path.join(dir, 'graph.json'), JSON.stringify(g, null, 2));
  return { nodes: g.nodes.length, edges: g.edges.length };
}

function main() {
  const args = process.argv.slice(2);
  if (args[0] === '--coupled') {
    const render = splitRenderArgs(args.slice(1));
    const { positionals, filters } = parseFilterArgs(render.argv);
    coupledMode(positionals[0] || process.cwd(), positionals.slice(1).join(' '), filters, { json: render.json });
    return;
  }
  if (args[0] === '--threads') {
    const render = splitRenderArgs(args.slice(1));
    const { positionals, filters } = parseFilterArgs(render.argv);
    threadsMode(positionals[0] || process.cwd(), filters, { json: render.json });
    return;
  }
  if (!fs.existsSync(ROOT)) return;
  const target = args[0];
  const dirs = target ? [path.join(ROOT, target)]
    : fs.readdirSync(ROOT).map(d => path.join(ROOT, d)).filter(d => fs.statSync(d).isDirectory());
  for (const d of dirs) {
    const r = buildProject(d);
    if (r) console.log(`graph: ${r.nodes} nodes, ${r.edges} edges → ${path.relative(ROOT, d)}/graph.json`);
  }
}

module.exports = { buildGraph, coupledWith, coupledWithMatcher, threadResults };
main();
