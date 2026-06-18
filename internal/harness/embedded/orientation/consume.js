#!/usr/bin/env node
// action-graph consume — the payoff. Two modes:
//   node consume.js <cwd>            → SessionStart injector (prints additionalContext JSON)
//   node consume.js --query <cwd> <q> → grep episodes for a keyword, print matches
// Read-only. If episodes.json missing/stale, prints nothing (no noise into a fresh session).

const fs = require('fs');
const path = require('path');
const { parseFilterArgs, filterEpisodes, hasFilters, describeFilters } = require('./filters');
const { projectKeyCandidates, inspectProject } = require('./project');
const { DATA_DIR } = require('./state');

const ROOT = DATA_DIR;

// human age from a millisecond delta
function fmtAge(deltaMs) {
  const h = deltaMs / 3600e3;
  if (h < 1) return `${Math.max(1, Math.round(h * 60))}min ago`;
  if (h < 48) return `${Math.round(h)}h ago`;
  return `${Math.round(h / 24)}d ago`;
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

function load(cwd, filters = {}) {
  if (filters.sameRepo) {
    const repoKey = filters.repoKey || targetRepoKey(cwd);
    const projects = [];
    const episodes = [];
    for (const dir of projectDirs()) {
      const manifest = manifestForDir(dir);
      if (manifest.repoKey !== repoKey) continue;
      const data = readJson(path.join(dir, 'episodes.json'));
      if (!data || !Array.isArray(data.episodes)) continue;
      projects.push({ key: path.basename(dir), cwd: manifest.cwd, repoKey: manifest.repoKey, worktreeCwd: manifest.worktreeCwd });
      episodes.push(...data.episodes);
    }
    if (!episodes.length) return null;
    return { updated: new Date(Math.max(...episodes.map(e => Date.parse(e.t || 0) || 0))).toISOString(), repoKey, projects, episodes };
  }
  const projects = [];
  const episodes = [];
  let updated = null;
  for (const dir of exactProjectDirs(cwd)) {
    const manifest = manifestForDir(dir);
    const data = readJson(path.join(dir, 'episodes.json'));
    if (!data || !Array.isArray(data.episodes)) continue;
    projects.push({ key: path.basename(dir), cwd: manifest.cwd, repoKey: manifest.repoKey, worktreeCwd: manifest.worktreeCwd });
    episodes.push(...data.episodes);
    if (data.updated && (!updated || String(data.updated).localeCompare(String(updated)) > 0)) updated = data.updated;
  }
  if (!episodes.length) return null;
  return { updated, projects, episodes };
}

function loadGraph(cwd, filters = {}) {
  if (filters.sameRepo) return null;
  const dirs = exactProjectDirs(cwd)
    .filter(dir => fs.existsSync(path.join(dir, 'graph.json')));
  if (dirs.length !== 1) return null;
  return readJson(path.join(dirs[0], 'graph.json'));
}

function printJson(obj) {
  process.stdout.write(JSON.stringify(obj, null, 2) + '\n');
}

function episodeSummary(e) {
  return {
    t: e.t,
    session: e.session,
    runtime: e.runtime,
    adapter: e.adapter,
    sourceKind: e.sourceKind,
    id: e.id,
    gitBranch: e.gitBranch,
    gitBranchSource: e.gitBranchSource,
    branchHistory: e.branchHistory || [],
    flow: e.flow,
    projectKey: e.projectKey,
    projectKeyVersion: e.projectKeyVersion,
    legacyProjectKey: e.legacyProjectKey,
    repoKey: e.repoKey,
    repoRoot: e.repoRoot,
    gitRemote: e.gitRemote,
    worktreeCwd: e.worktreeCwd,
    headSha: e.headSha,
    headShaSource: e.headShaSource,
    intent: e.intent,
    prose: e.prose,
    filesTouched: e.filesTouched || [],
    runs: e.runs || [],
    sources: e.sources || [],
    evidence: e.evidence || [],
  };
}

// Resolve a user-supplied file path to a matcher over stored `parent/base` names.
// Prefer an EXACT parent/base hit; only fall back to bare-basename suffix match when
// no exact name exists in `known`. Without this, `rules/codex.rs` also matches
// `schemas/codex.rs` (same basename) — conflating two distinct files. `known` is the
// set of all filenames seen (episode filesTouched ∪ graph file nodes).
function makeMatcher(file, known) {
  const base = file.split('/').slice(-2).join('/'); // parent/base
  const bare = file.split('/').pop();
  const exactExists = known.has(base);
  const canonical = exactExists ? base : null;
  const match = f => exactExists ? f === base : (f === base || f.endsWith('/' + bare) || f === bare);
  return { match, canonical, base };
}

function coupledFromEpisodes(episodes, match) {
  const counts = new Map();
  for (const ep of episodes) {
    const files = ep.filesTouched || [];
    if (!files.some(match)) continue;
    for (const f of files) {
      if (match(f)) continue;
      counts.set(f, (counts.get(f) || 0) + 1);
    }
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]);
}

function graphCoupledNeighbors(graph, match) {
  if (!graph) return [];
  const fileNode = (graph.nodes.find(n => n.type === 'file' && match(n.label)) || {}).id;
  if (!fileNode) return [];
  const nb = [];
  for (const e of graph.edges) {
    if (e.rel !== 'coupled') continue;
    if (e.from === fileNode) nb.push([e.to.replace('file:', ''), e.weight]);
    else if (e.to === fileNode) nb.push([e.from.replace('file:', ''), e.weight]);
  }
  return nb.sort((a, b) => b[1] - a[1]);
}

function printFilterNote(filters) {
  if (hasFilters(filters)) console.log(`Filters: ${describeFilters(filters)}\n`);
}

function evidenceLine(e) {
  const ev = (e.evidence || []).find(x => x.source) || {};
  const loc = ev.source ? `${ev.source}${ev.sourceLine ? `:${ev.sourceLine}` : ''}` : null;
  const bits = [
    e.id && `goal=${e.id}`,
    e.runtime,
    e.session && `session=${e.session}`,
    e.gitBranch && `branch=${e.gitBranch}${e.gitBranchSource ? `/${e.gitBranchSource}` : ''}`,
    e.worktreeCwd && `worktree=${e.worktreeCwd}`,
    loc,
  ].filter(Boolean);
  return bits.join(' ');
}

// SessionStart hook entry: inject a thin POINTER, not the data. The graph can be
// large; dumping recent goals + areas steals context the user wants for the task.
// Instead announce availability + the trigger, and let the agent pull provenance
// on demand via the check-provenance skill. Lazy, not eager.
function injectMode(cwd) {
  const data = load(cwd);
  if (!data || !data.episodes.length) { process.stdout.write('{}'); return; }
  const goals = data.episodes.filter(e => e.intent).length;
  if (!goals) { process.stdout.write('{}'); return; }

  const ctx =
    `orientation has recorded history for this project (${goals} goals across prior sessions). ` +
    `If you find yourself judging, deleting, or building near code you didn't write this session — ` +
    `before you conclude it's a bug/dead/done, or act on it — use the get-oriented skill to look up its ` +
    `real history (the goal behind it, shipped vs in-flight, what changed with it) instead of guessing from the code. ` +
    `Loads on demand; nothing preloaded here.`;

  process.stdout.write(JSON.stringify({
    hookSpecificOutput: { hookEventName: 'SessionStart', additionalContext: ctx }
  }));
}

function queryMode(cwd, q, filters = {}, opts = {}) {
  const data = load(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'query', cwd, q, filters, status: 'no_project', episodes: [] });
    else console.log('(no orientation history for this project yet)');
    return;
  }
  if (q) filters = { ...filters, keywords: [...(filters.keywords || []), q] };
  const episodes = filterEpisodes(data.episodes, filters);
  const hits = episodes;
  if (opts.json) {
    printJson({ type: 'query', cwd, q, filters, count: hits.length, episodes: hits.map(episodeSummary) });
    return;
  }
  if (!hits.length) { console.log(`(no episodes match "${q}")`); return; }
  printFilterNote(filters);
  console.log(`${hits.length} episode(s) match "${q}":\n`);
  for (const e of hits.reverse()) {
    const meta = [e.runtime, e.gitBranch].filter(Boolean).join('/');
    console.log(`[${e.t.slice(0, 10)}${meta ? ` ${meta}` : ''}] ${e.prose}`);
    if (e.filesTouched.length) console.log(`    files: ${e.filesTouched.join(', ')}`);
    const why = evidenceLine(e);
    if (why) console.log(`    why: ${why}`);
    console.log('');
  }
}

// Provenance of a file — the "should I delete this?" guard. Answers:
//   was it deliberate (committed goals touched it)?  what was the goal?
//   what travels with it (coupled neighbors = blast radius)?  abandoned attempts?
// filesTouched is stored as parent/base; match on suffix so the agent can pass
// any path form (src/auth.js, auth.js, /abs/.../auth.js).
function provenanceMode(cwd, file, filters = {}, opts = {}) {
  const data = load(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'provenance', cwd, file, filters, status: 'no_project', verdict: 'unknown', goals: [], coupled: [] });
    else console.log('(no orientation history for this project — cannot judge; do not assume cruft)');
    return;
  }
  const g0 = loadGraph(cwd, filters);
  const episodes = filterEpisodes(data.episodes, filters);
  const known = new Set([
    ...episodes.flatMap(e => e.filesTouched || []),
    ...((g0 && g0.nodes) || []).filter(n => n.type === 'file').map(n => n.label),
  ]);
  const { match } = makeMatcher(file, known);

  const goals = episodes.filter(e => e.intent && (e.filesTouched || []).some(match));
  if (!goals.length) {
    if (opts.json) printJson({ type: 'provenance', cwd, file, filters, status: 'no_match', verdict: 'unknown', goals: [], coupled: [] });
    else console.log(`No recorded work touched "${file}". Action-graph has no provenance — either pre-dates tracking or genuinely untouched. Judge on code alone.`);
    return;
  }

  const ms = e => { const d = Date.parse(e.t || ''); return isNaN(d) ? 0 : d; };
  // "now" = newest activity anywhere in the project (clock-robust; no wall-clock dep)
  const now = Math.max(...episodes.map(ms), 0);
  const HOUR = 3600e3;
  const lastTouch = Math.max(...goals.map(ms));
  const ageH = (now - lastTouch) / HOUR;
  const committed = goals.filter(e => (e.runs || []).some(r => r.kind === 'commit'));
  const uncommitted = goals.filter(e => !((e.runs || []).some(r => r.kind === 'commit')));
  const coupled = hasFilters(filters) ? coupledFromEpisodes(episodes, match) : graphCoupledNeighbors(g0, match);
  let verdict = 'unclear';
  if (ageH <= 48) verdict = 'active';
  else if (committed.length) verdict = 'deliberate';
  else if (ageH > 14 * 24) verdict = 'stale_uncertain';

  if (opts.json) {
    printJson({
      type: 'provenance',
      cwd,
      file,
      filters,
      verdict,
      lastTouch: goals.find(e => ms(e) === lastTouch)?.t || null,
      ageMs: now - lastTouch,
      count: goals.length,
      committed: committed.length,
      uncommitted: uncommitted.length,
      goals: [...goals].sort((a, b) => ms(b) - ms(a)).map(episodeSummary),
      coupled: coupled.map(([path, weight]) => ({ path, weight })),
    });
    return;
  }

  console.log(`PROVENANCE: ${file}`);
  printFilterNote(filters);
  console.log(`${goals.length} goal(s) touched it — ${committed.length} committed, ${uncommitted.length} not committed. Last touched ${fmtAge(now - lastTouch)} (relative to latest project activity).\n`);

  // VERDICT — recency first. Uncommitted is NOT abandoned: in-flight work is
  // uncommitted by definition (you commit at the end). Recent activity => ACTIVE.
  // Abandonment requires uncommitted AND stale (no recent touch) — and even then
  // it's stated as uncertain, never asserted. This is the git-state-not-ship-status
  // rule applied to the tool itself: don't infer abandon from absence-of-commit.
  if (verdict === 'active') {
    console.log(`⚠ ACTIVELY IN FLIGHT — last touched ${fmtAge(now - lastTouch)}. ` +
      `${uncommitted.length ? 'Uncommitted edits here are work-in-progress, NOT abandoned — ' : ''}` +
      `someone is delivering this now. Do not treat as dead/stale; continue or ask, don't scrap.`);
  } else if (verdict === 'deliberate') {
    console.log('⚠ DELIBERATE WORK — committed and not recently active. Built on purpose; do not delete as cruft without cause.');
  } else if (verdict === 'stale_uncertain') {
    console.log(`POSSIBLY STALE (uncertain) — uncommitted and untouched for ${fmtAge(now - lastTouch)}. ` +
      `MIGHT be abandoned, might be paused. Verify with the user / real signals before treating as dead — uncommitted alone is not proof of abandonment.`);
  } else {
    console.log(`Uncommitted, last touched ${fmtAge(now - lastTouch)}. Status unclear — could be paused mid-work. Don't assume abandoned; check intent below.`);
  }
  console.log('');

  console.log('Goals that touched it (newest first):');
  for (const e of [...goals].sort((a, b) => ms(b) - ms(a)).slice(0, 6)) {
    const c = (e.runs || []).some(r => r.kind === 'commit') ? '✓committed' : '·uncommitted';
    console.log(`  [${c}] ${e.intent.slice(0, 76)}`);
    const why = evidenceLine(e);
    if (why) console.log(`      why: ${why}`);
  }
  console.log('');

  // coupled neighbors = blast radius
  if (coupled.length) {
    console.log(hasFilters(filters)
      ? 'Coupled neighbors in filtered history (usually edited together — check before removing):'
      : 'Coupled neighbors (usually edited together — check before removing):');
    coupled.slice(0, 6).forEach(([f, w]) => console.log(`  ${w}x  ${f}`));
  }
}

// Related prior work for a file you're ABOUT TO ADD code to. The reinvention /
// duplication / contradiction guard: before writing, see what already built this
// area and where sibling logic lives (coupled cluster), so you extend or reconcile
// instead of duplicating or contradicting an earlier decision. File-level: surfaces
// the AREA to read, not literal duplicate lines (no code-content index).
function relatedMode(cwd, file, filters = {}, opts = {}) {
  const data = load(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'related', cwd, file, filters, status: 'no_project', goals: [], siblings: [] });
    else console.log('(no orientation history for this project — no prior-work signal; proceed, but search the code yourself)');
    return;
  }
  const g = loadGraph(cwd, filters);
  const episodes = filterEpisodes(data.episodes, filters);
  const known = new Set([
    ...episodes.flatMap(e => e.filesTouched || []),
    ...((g && g.nodes) || []).filter(n => n.type === 'file').map(n => n.label),
  ]);
  const { match } = makeMatcher(file, known);

  const goals = episodes.filter(e => e.intent && (e.filesTouched || []).some(match));
  const committed = e => (e.runs || []).some(r => r.kind === 'commit');

  let nb = [];
  if (hasFilters(filters)) {
    nb = coupledFromEpisodes(episodes, match);
  } else if (g) {
    // passenger files (always co-edited — CHANGELOG/tracking docs) are weak siblings;
    // compute df + max co-edit ratio from coupled edges and drop them, mirroring graph.js
    const df = new Map();
    for (const e of g.edges) if (e.rel === 'touched') df.set(e.to, (df.get(e.to) || 0) + 1);
    const maxCo = new Map();
    for (const e of g.edges) if (e.rel === 'coupled') {
      const ra = e.weight / (df.get(e.from) || 1), rb = e.weight / (df.get(e.to) || 1);
      if (ra > (maxCo.get(e.from) || 0)) maxCo.set(e.from, ra);
      if (rb > (maxCo.get(e.to) || 0)) maxCo.set(e.to, rb);
    }
    const isPassenger = id => (df.get(id) || 0) >= 4 && (maxCo.get(id) || 0) >= 0.8;

    const self = g.nodes.find(n => n.type === 'file' && match(n.label));
    const node = self && self.id;
    if (node) for (const e of g.edges) {
      if (e.rel !== 'coupled') continue;
      const other = e.from === node ? e.to : e.to === node ? e.from : null;
      if (!other || other === node) continue;        // skip self-loops
      if (isPassenger(other)) continue;              // skip passenger files
      nb.push([other.replace('file:', ''), e.weight]);
    }
    nb.sort((a, b) => b[1] - a[1]);
  }

  if (!goals.length && !nb.length) {
    if (opts.json) printJson({ type: 'related', cwd, file, filters, status: 'no_match', goals: [], siblings: [] });
    else console.log(`No prior work recorded on "${file}" or files coupled to it. Likely new ground — but still grep the codebase for similar logic before adding.`);
    return;
  }

  if (opts.json) {
    const sortedGoals = [...goals].sort((a, b) => (committed(b) - committed(a)));
    printJson({
      type: 'related',
      cwd,
      file,
      filters,
      status: 'ok',
      goals: sortedGoals.map(episodeSummary),
      siblings: nb.map(([path, weight]) => ({ path, weight })),
    });
    return;
  }

  console.log(`RELATED PRIOR WORK: ${file}\n`);
  printFilterNote(filters);
  if (goals.length) {
    console.log(`${goals.length} goal(s) already built in this file — read before adding, to extend rather than duplicate or contradict:`);
    // committed first (decisions that stuck), then most recent
    goals.sort((a, b) => (committed(b) - committed(a)));
    for (const e of goals.slice(0, 6)) {
      console.log(`  ${committed(e) ? '✓' : '·'} ${e.intent.slice(0, 78)}`);
      const why = evidenceLine(e);
      if (why) console.log(`      why: ${why}`);
    }
    console.log('');
  }
  if (nb.length) {
    console.log('Sibling files (edited alongside this one — similar/related logic likely lives here):');
    nb.slice(0, 6).forEach(([f, w]) => console.log(`  ${w}x  ${f}`));
    console.log('\nGrep these + this file for the function/behavior you intend to add before writing it.');
  }
}

function sourcesMode(cwd, goalId, filters = {}, opts = {}) {
  const data = load(cwd, filters);
  if (!data) {
    if (opts.json) printJson({ type: 'sources', cwd, goalId, filters, status: 'no_project', matches: [] });
    else console.log('(no orientation history for this project — no source evidence available)');
    return;
  }

  const episodes = filterEpisodes(data.episodes, filters);
  const matches = episodes.filter(e => e.id === goalId || (goalId && e.id && e.id.startsWith(goalId)));
  if (!goalId) {
    if (opts.json) printJson({ type: 'sources', cwd, goalId, filters, status: 'missing_goal_id', matches: [] });
    else console.log('usage: orientation sources <cwd> <goal-id>');
    return;
  }
  if (!matches.length) {
    if (opts.json) printJson({ type: 'sources', cwd, goalId, filters, status: 'no_match', matches: [] });
    else console.log(`No goal matches "${goalId}" in this history slice.`);
    return;
  }
  if (matches.length > 1) {
    if (opts.json) printJson({ type: 'sources', cwd, goalId, filters, status: 'ambiguous', matches: matches.map(episodeSummary) });
    else {
      console.log(`Goal id "${goalId}" is ambiguous; matches:`);
      matches.slice(0, 12).forEach(e => console.log(`  ${e.id}  ${e.intent || e.prose || ''}`));
    }
    return;
  }

  const e = matches[0];
  const payload = {
    type: 'sources',
    cwd,
    goalId,
    filters,
    status: 'ok',
    goal: episodeSummary(e),
    evidence: e.evidence || [],
    sources: e.sources || [],
  };
  if (opts.json) {
    printJson(payload);
    return;
  }

  console.log(`SOURCES: ${e.id}`);
  printFilterNote(filters);
  console.log(`${e.intent || e.prose || '(no intent text)'}\n`);
  const why = evidenceLine(e);
  if (why) console.log(`why: ${why}\n`);
  if (e.filesTouched && e.filesTouched.length) console.log(`files: ${e.filesTouched.join(', ')}\n`);
  const evs = e.evidence || [];
  if (evs.length) {
    console.log('Evidence:');
    for (const ev of evs) {
      const loc = ev.source ? `${ev.source}${ev.sourceLine ? `:${ev.sourceLine}` : ''}` : '(no source path)';
      const bits = [
        ev.t && ev.t.slice(0, 19),
        ev.runtime,
        ev.session && `session=${ev.session}`,
        ev.kind,
        ev.branch && `branch=${ev.branch}`,
        ev.sourceOffset != null && `offset=${ev.sourceOffset}`,
      ].filter(Boolean).join(' ');
      console.log(`  ${loc}${bits ? `  ${bits}` : ''}`);
    }
  } else {
    console.log('No source evidence recorded for this goal.');
  }
}

const args = process.argv.slice(2);
function splitRenderArgs(argv) {
  return { json: argv.includes('--json'), argv: argv.filter(a => a !== '--json') };
}

if (args[0] === '--related') {
  const render = splitRenderArgs(args.slice(1));
  const { positionals, filters } = parseFilterArgs(render.argv);
  relatedMode(positionals[0] || process.cwd(), positionals.slice(1).join(' '), filters, { json: render.json });
} else if (args[0] === '--provenance') {
  const render = splitRenderArgs(args.slice(1));
  const { positionals, filters } = parseFilterArgs(render.argv);
  provenanceMode(positionals[0] || process.cwd(), positionals.slice(1).join(' '), filters, { json: render.json });
} else if (args[0] === '--query') {
  const render = splitRenderArgs(args.slice(1));
  const { positionals, filters } = parseFilterArgs(render.argv);
  queryMode(positionals[0] || process.cwd(), positionals.slice(1).join(' '), filters, { json: render.json });
} else if (args[0] === '--sources') {
  const render = splitRenderArgs(args.slice(1));
  const { positionals, filters } = parseFilterArgs(render.argv);
  sourcesMode(positionals[0] || process.cwd(), positionals[1], filters, { json: render.json });
} else if (args[0]) {
  // explicit cwd passed (manual/test use)
  injectMode(args[0]);
} else {
  // hook use: cwd arrives on stdin JSON. Read it, then inject.
  let input = '';
  process.stdin.on('data', c => { input += c; });
  process.stdin.on('end', () => {
    let cwd = process.cwd();
    try { cwd = JSON.parse(input).cwd || cwd; } catch {}
    injectMode(cwd);
  });
}
