// Shared episode filters for query commands. Filters are data-level: branch,
// runtime, time window, and keywords apply after transcripts have been
// normalized into episodes, regardless of which agent runtime produced them.

function parseFilterArgs(argv) {
  const filters = { keywords: [] };
  const positionals = [];

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--branch') filters.branch = argv[++i];
    else if (arg.startsWith('--branch=')) filters.branch = arg.slice('--branch='.length);
    else if (arg === '--branch-source') filters.branchSource = argv[++i];
    else if (arg.startsWith('--branch-source=')) filters.branchSource = arg.slice('--branch-source='.length);
    else if (arg === '--runtime') filters.runtime = argv[++i];
    else if (arg.startsWith('--runtime=')) filters.runtime = arg.slice('--runtime='.length);
    else if (arg === '--adapter') filters.adapter = argv[++i];
    else if (arg.startsWith('--adapter=')) filters.adapter = arg.slice('--adapter='.length);
    else if (arg === '--source-kind') filters.sourceKind = argv[++i];
    else if (arg.startsWith('--source-kind=')) filters.sourceKind = arg.slice('--source-kind='.length);
    else if (arg === '--same-repo') filters.sameRepo = true;
    else if (arg === '--repo-key') filters.repoKey = argv[++i];
    else if (arg.startsWith('--repo-key=')) filters.repoKey = arg.slice('--repo-key='.length);
    else if (arg === '--project-key') filters.projectKey = argv[++i];
    else if (arg.startsWith('--project-key=')) filters.projectKey = arg.slice('--project-key='.length);
    else if (arg === '--worktree') filters.worktree = argv[++i];
    else if (arg.startsWith('--worktree=')) filters.worktree = arg.slice('--worktree='.length);
    else if (arg === '--since') filters.since = argv[++i];
    else if (arg.startsWith('--since=')) filters.since = arg.slice('--since='.length);
    else if (arg === '--period') filters.since = argv[++i];
    else if (arg.startsWith('--period=')) filters.since = arg.slice('--period='.length);
    else if (arg === '--until') filters.until = argv[++i];
    else if (arg.startsWith('--until=')) filters.until = arg.slice('--until='.length);
    else if (arg === '--keyword') filters.keywords.push(argv[++i]);
    else if (arg.startsWith('--keyword=')) filters.keywords.push(arg.slice('--keyword='.length));
    else positionals.push(arg);
  }

  filters.keywords = filters.keywords.filter(Boolean);
  return { positionals, filters };
}

function hasFilters(filters) {
  return !!(filters && (
    filters.branch ||
    filters.branchSource ||
    filters.runtime ||
    filters.adapter ||
    filters.sourceKind ||
    filters.sameRepo ||
    filters.repoKey ||
    filters.projectKey ||
    filters.worktree ||
    filters.since ||
    filters.until ||
    (filters.keywords && filters.keywords.length)
  ));
}

function parseTime(value, now = Date.now()) {
  if (!value) return null;
  const rel = /^(\d+)([hdwm])$/.exec(String(value).trim());
  if (rel) {
    const n = Number(rel[1]);
    const unit = rel[2];
    const ms = unit === 'h' ? n * 3600e3
      : unit === 'd' ? n * 24 * 3600e3
      : unit === 'w' ? n * 7 * 24 * 3600e3
      : n * 30 * 24 * 3600e3;
    return now - ms;
  }
  const t = Date.parse(value);
  return Number.isNaN(t) ? null : t;
}

function episodeText(ep) {
  const runs = (ep.runs || []).flatMap(r => [r.kind, r.cmd, r.reason]);
  const branches = (ep.branchHistory || []).flatMap(b => [b.branch, b.source]);
  return [
    ep.intent,
    ep.prose,
    ep.gitBranch,
    ep.gitBranchSource,
    ...branches,
    ep.flow,
    ep.runtime,
    ep.adapter,
    ep.sourceKind,
    ep.projectKey,
    ep.projectKeyVersion,
    ep.legacyProjectKey,
    ep.repoKey,
    ep.repoRoot,
    ep.gitRemote,
    ep.worktreeCwd,
    ep.headSha,
    ...(ep.filesTouched || []),
    ...(ep.delegates || []),
    ...(ep.skills || []),
    ...runs,
  ].filter(Boolean).join(' ').toLowerCase();
}

function episodeBranches(ep) {
  const branches = new Set();
  if (ep.gitBranch) branches.add(ep.gitBranch);
  for (const b of ep.branchHistory || []) if (b && b.branch) branches.add(b.branch);
  return branches;
}

function episodeBranchSources(ep) {
  const sources = new Set();
  if (ep.gitBranchSource) sources.add(ep.gitBranchSource);
  for (const b of ep.branchHistory || []) if (b && b.source) sources.add(b.source);
  return sources;
}

function filterEpisodes(episodes, filters = {}) {
  if (!hasFilters(filters)) return episodes;
  const now = Date.now();
  const since = parseTime(filters.since, now);
  const until = parseTime(filters.until, now);
  const keywords = (filters.keywords || []).map(k => String(k).toLowerCase()).filter(Boolean);

  return episodes.filter(ep => {
    if (filters.branch && !episodeBranches(ep).has(filters.branch)) return false;
    if (filters.branchSource && !episodeBranchSources(ep).has(filters.branchSource)) return false;
    if (filters.runtime && ep.runtime !== filters.runtime) return false;
    if (filters.adapter && ep.adapter !== filters.adapter) return false;
    if (filters.sourceKind && ep.sourceKind !== filters.sourceKind) return false;
    if (filters.repoKey && ep.repoKey !== filters.repoKey) return false;
    if (filters.projectKey && ep.projectKey !== filters.projectKey && ep.legacyProjectKey !== filters.projectKey) return false;
    if (filters.worktree && ep.worktreeCwd !== filters.worktree && ep.cwd !== filters.worktree) return false;
    const t = Date.parse(ep.t || '');
    if (since != null && !Number.isNaN(t) && t < since) return false;
    if (until != null && !Number.isNaN(t) && t > until) return false;
    if (keywords.length) {
      const hay = episodeText(ep);
      if (!keywords.every(k => hay.includes(k))) return false;
    }
    return true;
  });
}

function describeFilters(filters = {}) {
  const parts = [];
  if (filters.runtime) parts.push(`runtime=${filters.runtime}`);
  if (filters.adapter) parts.push(`adapter=${filters.adapter}`);
  if (filters.sourceKind) parts.push(`source-kind=${filters.sourceKind}`);
  if (filters.branch) parts.push(`branch=${filters.branch}`);
  if (filters.branchSource) parts.push(`branch-source=${filters.branchSource}`);
  if (filters.sameRepo) parts.push('same-repo');
  if (filters.repoKey) parts.push(`repo-key=${filters.repoKey}`);
  if (filters.projectKey) parts.push(`project-key=${filters.projectKey}`);
  if (filters.worktree) parts.push(`worktree=${filters.worktree}`);
  if (filters.since) parts.push(`since=${filters.since}`);
  if (filters.until) parts.push(`until=${filters.until}`);
  for (const k of filters.keywords || []) parts.push(`keyword=${k}`);
  return parts.join(', ');
}

module.exports = {
  parseFilterArgs,
  hasFilters,
  filterEpisodes,
  describeFilters,
  parseTime,
};
