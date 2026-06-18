const path = require('path');
const os = require('os');

const HOME = os.homedir();
const CLAUDE = process.env.CLAUDE_CONFIG_DIR || path.join(HOME, '.claude');
const CODEX = process.env.CODEX_HOME || path.join(HOME, '.codex');
const EIGEN = process.env.EIGEN_HOME || path.join(HOME, '.eigen');

// Eigen owns orientation now. State lives under ~/.eigen/orientation by default;
// the engine may be this directory or an embedded temp copy selected by the
// harness via ORIENTATION_ENGINE_DIR.
const LEGACY_HOME = path.join(CLAUDE, 'action-graph'); // read-only compatibility label
const EIGEN_ORIENTATION_HOME = process.env.EIGEN_ORIENTATION_HOME ||
  process.env.EIGEN_ORIENTATION_DIR ||
  path.join(EIGEN, 'orientation');
const HERE = path.resolve(__dirname);
const ORIENTATION_HOME = process.env.ORIENTATION_HOME || EIGEN_ORIENTATION_HOME;
const ENGINE_DIR = process.env.ORIENTATION_ENGINE_DIR || HERE;
const DATA_DIR = path.join(ORIENTATION_HOME, 'data');
const ALLOWLIST_FILE = path.join(ORIENTATION_HOME, 'projects.txt');
const CLAUDE_PROJECTS_DIR = path.join(CLAUDE, 'projects');
const CODEX_SESSIONS_DIR = path.join(CODEX, 'sessions');
const EIGEN_DIR = EIGEN;

module.exports = {
  HOME,
  CLAUDE,
  CODEX,
  EIGEN,
  LEGACY_HOME,
  EIGEN_ORIENTATION_HOME,
  ORIENTATION_HOME,
  ENGINE_DIR,
  DATA_DIR,
  ALLOWLIST_FILE,
  CLAUDE_PROJECTS_DIR,
  CODEX_SESSIONS_DIR,
  EIGEN_DIR,
};
