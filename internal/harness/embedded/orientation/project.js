const crypto = require('crypto');
const { spawnSync } = require('child_process');

function shortHash(value, n = 12) {
  return crypto.createHash('sha1').update(value || 'unknown').digest('hex').slice(0, n);
}

const PROJECT_KEY_VERSION = 'repo-worktree-v1';

function legacyProjectKey(cwd) {
  return shortHash(cwd || 'unknown');
}

function projectKeyForIdentity(identity = {}) {
  const worktreeCwd = identity.worktreeCwd || identity.cwd || 'unknown';
  const repoIdentity = identity.repoKey
    || shortHash(identity.gitRemote || identity.repoRoot || worktreeCwd || 'unknown');
  return shortHash(`${PROJECT_KEY_VERSION}\0${repoIdentity}\0${worktreeCwd}`);
}

function projectKey(cwd) {
  const worktreeCwd = cwd || 'unknown';
  const repoRoot = runGit(cwd, ['rev-parse', '--show-toplevel']);
  const gitRemote = runGit(cwd, ['config', '--get', 'remote.origin.url']);
  const repoKey = shortHash(gitRemote || repoRoot || worktreeCwd);
  return projectKeyForIdentity({ repoKey, worktreeCwd });
}

function projectKeyCandidates(cwd) {
  return [...new Set([projectKey(cwd), legacyProjectKey(cwd)])];
}

function sourceKey(runtime, source) {
  return shortHash(`${runtime || 'unknown'}:${source || 'unknown'}`, 16);
}

function runGit(cwd, args) {
  if (!cwd) return null;
  const r = spawnSync('git', ['-C', cwd, ...args], { encoding: 'utf8' });
  if (r.status !== 0) return null;
  return r.stdout.trim() || null;
}

function inspectProject(cwd) {
  const worktreeCwd = cwd || null;
  const repoRoot = runGit(cwd, ['rev-parse', '--show-toplevel']);
  const gitRemote = runGit(cwd, ['config', '--get', 'remote.origin.url']);
  const headSha = runGit(cwd, ['rev-parse', 'HEAD']);
  const currentBranch = runGit(cwd, ['branch', '--show-current']);
  const repoIdentity = gitRemote || repoRoot || cwd || 'unknown';
  const repoKey = shortHash(repoIdentity);

  return {
    projectKey: projectKeyForIdentity({ repoKey, worktreeCwd }),
    projectKeyVersion: PROJECT_KEY_VERSION,
    legacyProjectKey: legacyProjectKey(cwd),
    repoKey,
    repoRoot,
    gitRemote,
    worktreeCwd,
    headSha,
    currentBranch,
  };
}

function eventIdentity(identity = {}) {
  const out = {};
  for (const k of ['projectKey', 'projectKeyVersion', 'legacyProjectKey', 'repoKey', 'repoRoot', 'gitRemote', 'worktreeCwd', 'headSha']) {
    if (identity[k]) out[k] = identity[k];
  }
  if (out.headSha) out.headShaSource = identity.headShaSource || 'ingest';
  return out;
}

module.exports = {
  shortHash,
  PROJECT_KEY_VERSION,
  projectKey,
  projectKeyForIdentity,
  projectKeyCandidates,
  legacyProjectKey,
  sourceKey,
  inspectProject,
  eventIdentity,
};
