'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

function runtimeDir() {
  if (process.env.XDG_RUNTIME_DIR) return process.env.XDG_RUNTIME_DIR;
  if (typeof process.getuid === 'function') {
    const candidate = `/run/user/${process.getuid()}`;
    if (fs.existsSync(candidate)) return candidate;
  }
  return os.tmpdir();
}

function socketPath() {
  return process.env.AGENT_CHROME_BRIDGE_SOCKET
    || path.join(runtimeDir(), 'agent-chrome-bridge.sock');
}

module.exports = { socketPath };
