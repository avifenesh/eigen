#!/usr/bin/env node
// action-graph condense — the smart part. Run in background (Stop hook or cron).
// raw.jsonl (append-only, noisy) → episodes.json (deduped, clustered, minimal prose).
// No model. Template prose. Add embedder/small-model later ONLY if this proves too thin.

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { DATA_DIR } = require('./state');

const ROOT = DATA_DIR;

function loadRaw(dir) {
  const f = path.join(dir, 'raw.jsonl');
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, 'utf8').split('\n').filter(Boolean).map(l => {
    try { return JSON.parse(l); } catch { return null; }
  }).filter(Boolean);
}

function base(p) { return p ? p.split('/').slice(-2).join('/') : p; }

// Split the flat event stream into episodes. Each `intent` event opens a new episode.
// Split the flat stream into GOALS. Each user intent opens a goal; everything
// until the next intent belongs to it. Commits do NOT split — they mark done
// inside the goal (see buildSubgoals).
function splitEpisodes(events) {
  const eps = [];
  let cur = null;
  for (const e of events) {
    if (e.kind === 'intent') {
      cur = {
        intent: e.text,
        t: e.t,
        session: e.session,
        runtime: e.runtime,
        adapter: e.adapter,
        sourceKind: e.sourceKind,
        gitBranch: e.gitBranch,
        gitBranchSource: e.gitBranchSource,
        flow: e.flow,
        events: [e],
      };
      eps.push(cur);
    } else {
      if (!cur) {
        cur = {
          intent: null,
          t: e.t,
          session: e.session,
          runtime: e.runtime,
          adapter: e.adapter,
          sourceKind: e.sourceKind,
          gitBranch: e.gitBranch,
          gitBranchSource: e.gitBranchSource,
          flow: e.flow,
          events: [],
        };
        eps.push(cur);
      }
      cur.events.push(e);
    }
  }
  return eps;
}

// Build the GOAL → SUBGOAL → ACTION tree inside one goal.
// A subgoal = a run of actions sharing the same echo'd reason, closed (done) by
// a commit. The reason string (Bash description / echo) is the subgoal label —
// the "echo sub goal" the user pointed at. Commit = "goal marked as done".
function buildSubgoals(events) {
  const subgoals = [];
  let cur = null;

  function open(label) { cur = { label: label || null, actions: [], files: [], done: false, failed: false }; subgoals.push(cur); }
  function ensure(label) { if (!cur) open(label); else if (label && !cur.label) cur.label = label; }

  for (const e of events) {
    if (e.kind === 'explore') {
      ensure(e.q ? `explore ${e.q}` : null);
      cur.actions.push({ kind: 'explore', files: (e.files || []).map(base) });
      continue;
    }
    if (e.kind === 'edit' || e.kind === 'write') {
      ensure(null);
      for (const f of (e.files || [])) cur.files.push(base(f));
      cur.actions.push({ kind: e.kind, files: (e.files || []).map(base) });
      if (e.failed) cur.failed = true;
      continue;
    }
    // a reason on any action names/refines the current subgoal
    if (e.reason) ensure(e.reason);
    else ensure(null);

    if (e.kind === 'commit' && !e.failed) {
      cur.actions.push({ kind: 'commit', cmd: e.cmd });
      cur.done = true;        // commit closes this subgoal as done
      cur = null;             // next action starts a fresh subgoal
      continue;
    }
    if (['test', 'build', 'push', 'run', 'delegate', 'skill'].includes(e.kind)) {
      cur.actions.push({ kind: e.kind, cmd: e.cmd, agent: e.agent, skill: e.skill });
      if (e.failed) cur.failed = true;
    }
  }
  // drop empty subgoals (label-only, no actions)
  return subgoals.filter(s => s.actions.length);
}

// Condense one episode's events into a minimal record.
function condenseEpisode(ep) {
  const files = new Map();      // file -> {edits, reads}
  const runs = [];              // notable commands (test/build/commit/push)
  const delegates = [];
  const skills = [];
  const runtimes = [];
  const adapters = [];
  const sourceKinds = [];
  const gitBranches = [];
  const gitBranchSources = [];
  const branchEvents = [];
  const sources = [];
  const projectKeys = [];
  const projectKeyVersions = [];
  const legacyProjectKeys = [];
  const repoKeys = [];
  const repoRoots = [];
  const gitRemotes = [];
  const worktreeCwds = [];
  const headShas = [];
  const headShaSources = [];
  const evidence = [];
  let exploreCount = 0;
  const exploreFiles = new Set();
  let deadEnds = 0;
  const reasons = [];

  for (const e of ep.events) {
    if (e.runtime) runtimes.push(e.runtime);
    if (e.adapter) adapters.push(e.adapter);
    if (e.sourceKind) sourceKinds.push(e.sourceKind);
    if (e.gitBranch) {
      gitBranches.push(e.gitBranch);
      branchEvents.push({
        branch: e.gitBranch,
        source: e.gitBranchSource || null,
        t: e.t,
        session: e.session,
        runtime: e.runtime,
      });
    }
    if (e.gitBranchSource) gitBranchSources.push(e.gitBranchSource);
    if (e.source) sources.push(e.source);
    if (e.projectKey) projectKeys.push(e.projectKey);
    if (e.projectKeyVersion) projectKeyVersions.push(e.projectKeyVersion);
    if (e.legacyProjectKey) legacyProjectKeys.push(e.legacyProjectKey);
    if (e.repoKey) repoKeys.push(e.repoKey);
    if (e.repoRoot) repoRoots.push(e.repoRoot);
    if (e.gitRemote) gitRemotes.push(e.gitRemote);
    if (e.worktreeCwd) worktreeCwds.push(e.worktreeCwd);
    if (e.headSha) headShas.push(e.headSha);
    if (e.headShaSource) headShaSources.push(e.headShaSource);
    if (e.source || e.session || e.runtime) evidence.push({
      runtime: e.runtime,
      adapter: e.adapter,
      sourceKind: e.sourceKind,
      session: e.session,
      source: e.source,
      sourceLine: e.sourceLine,
      sourceOffset: e.sourceOffset,
      t: e.t,
      kind: e.kind,
      gitBranch: e.gitBranch,
      gitBranchSource: e.gitBranchSource,
    });
    if (e.kind === 'explore') {
      exploreCount++;
      (e.files || []).forEach(f => f && exploreFiles.add(base(f)));
      continue;
    }
    if (e.kind === 'edit' || e.kind === 'write') {
      for (const f of (e.files || [])) {
        const k = base(f);
        const rec = files.get(k) || { edits: 0 };
        rec.edits++;
        files.set(k, rec);
      }
      if (e.failed) deadEnds++;
      continue;
    }
    if (['test', 'build', 'commit', 'push', 'run'].includes(e.kind)) {
      // keep only signal-bearing runs; drop bare `run` unless it failed or has reason
      if (e.kind === 'run' && !e.failed && !e.reason) continue;
      runs.push({ kind: e.kind, cmd: e.cmd, reason: e.reason, failed: !!e.failed });
      if (e.reason) reasons.push(e.reason);
      if (e.failed) deadEnds++;
      continue;
    }
    if (e.kind === 'delegate') { delegates.push(e.agent || e.desc); continue; }
    if (e.kind === 'skill') { skills.push(e.skill); continue; }
  }

  // dedup runs by (kind, cmd) keeping the LAST occurrence — final outcome wins.
  // fail-then-pass must read as pass, else a future session distrusts shipped work.
  const lastByKey = new Map();
  for (const r of runs) lastByKey.set(r.kind + '|' + (r.cmd || ''), r);
  const uniqRuns = [...lastByKey.values()];

  const branches = dedupeBranchHistory(branchEvents);
  const lastBranch = last(branches);
  const rec = {
    intent: ep.intent,
    t: ep.t,
    session: ep.session,
    runtime: mostCommon(runtimes) || ep.runtime || 'claude',
    adapter: mostCommon(adapters) || ep.adapter,
    sourceKind: mostCommon(sourceKinds) || ep.sourceKind,
    gitBranch: lastBranch?.branch || last(gitBranches) || ep.gitBranch,
    gitBranchSource: lastBranch?.source || last(gitBranchSources) || ep.gitBranchSource,
    branchHistory: branches,
    flow: ep.flow,
    projectKey: mostCommon(projectKeys),
    projectKeyVersion: mostCommon(projectKeyVersions),
    legacyProjectKey: mostCommon(legacyProjectKeys),
    repoKey: mostCommon(repoKeys),
    repoRoot: mostCommon(repoRoots),
    gitRemote: mostCommon(gitRemotes),
    worktreeCwd: mostCommon(worktreeCwds),
    headSha: last(headShas),
    headShaSource: last(headShaSources),
    sources: [...new Set(sources)].slice(0, 8),
    evidence: dedupeEvidence(evidence).slice(0, 12),
    filesTouched: [...files.keys()],
    explored: exploreCount ? { count: exploreCount, files: [...exploreFiles].slice(0, 8) } : null,
    runs: uniqRuns,
    delegates: delegates.filter(Boolean),
    skills: [...new Set(skills)],
    deadEnds,
    prose: prose(ep.intent, files, uniqRuns, exploreCount, delegates, deadEnds, reasons, ep.continued),
  };
  rec.id = episodeId(rec);
  return rec;
}

function last(xs) { return xs.length ? xs[xs.length - 1] : undefined; }

function mostCommon(xs) {
  const counts = new Map();
  for (const x of xs.filter(Boolean)) counts.set(x, (counts.get(x) || 0) + 1);
  return [...counts.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
}

function dedupeEvidence(items) {
  const seen = new Set();
  const out = [];
  for (const e of items) {
    const key = [e.runtime, e.session, e.source, e.sourceLine, e.sourceOffset, e.kind].join('|');
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(e);
  }
  return out;
}

function dedupeBranchHistory(items) {
  const out = [];
  let prev = null;
  for (const item of items) {
    if (!item.branch) continue;
    const next = {
      branch: item.branch,
      source: item.source,
      t: item.t,
      session: item.session,
      runtime: item.runtime,
    };
    const key = [next.branch, next.source, next.session, next.runtime].join('|');
    if (key === prev) continue;
    prev = key;
    out.push(next);
  }
  return out.slice(-12);
}

function episodeId(ep) {
  return crypto.createHash('sha1')
    .update([ep.t, ep.session, ep.runtime, ep.intent, ep.sources && ep.sources[0], ep.filesTouched.join(',')].filter(Boolean).join('|'))
    .digest('hex')
    .slice(0, 12);
}

// Template prose — the "least but enough" line a future session reads.
function prose(intent, files, runs, exploreCount, delegates, deadEnds, reasons, continued) {
  const parts = [];
  if (intent) parts.push(`${continued ? 'Goal (cont.)' : 'Goal'}: ${intent}`);
  const fnames = [...files.keys()];
  if (fnames.length) parts.push(`Edited ${fnames.length} file(s): ${fnames.slice(0, 5).join(', ')}`);
  else if (exploreCount) parts.push(`Explored ${exploreCount} reads, no edits`);
  if (delegates.length) parts.push(`Delegated to ${delegates.length} agent(s)`);
  const committed = runs.find(r => r.kind === 'commit');
  const tested = runs.find(r => r.kind === 'test');
  if (tested) parts.push(tested.failed ? 'Tests FAILED' : 'Tests ran');
  if (committed) parts.push('Committed');
  if (deadEnds) parts.push(`${deadEnds} dead end(s)`);
  if (reasons.length) parts.push(`Why: ${[...new Set(reasons)].slice(0, 3).join('; ')}`);
  return parts.join('. ') + '.';
}

function condenseProject(dir) {
  const events = loadRaw(dir);
  if (!events.length) return null;
  const eps = splitEpisodes(events).map(condenseEpisode)
    // drop empty episodes (intent with zero meaningful actions)
    .filter(e => e.filesTouched.length || e.runs.length || e.delegates.length || e.explored);
  const out = { updated: new Date().toISOString(), episodes: eps };
  fs.writeFileSync(path.join(dir, 'episodes.json'), JSON.stringify(out, null, 2));
  return { dir, episodes: eps.length };
}

function main() {
  if (!fs.existsSync(ROOT)) return;
  const target = process.argv[2]; // optional: specific project key
  const dirs = target ? [path.join(ROOT, target)]
    : fs.readdirSync(ROOT).map(d => path.join(ROOT, d)).filter(d => fs.statSync(d).isDirectory());
  for (const d of dirs) {
    const r = condenseProject(d);
    if (r) console.log(`condensed ${r.episodes} episodes → ${path.relative(ROOT, d)}/episodes.json`);
  }
}

main();
