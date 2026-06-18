#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const { socketPath } = require('../lib/socket-path');
const { version: PACKAGE_VERSION } = require('../package.json');

const SOCKET_PATH = socketPath();
const STARTED_AT = Date.now();
const LOG_DIR = process.env.AGENT_CHROME_BRIDGE_LOG_DIR
  || path.join(os.homedir(), '.local', 'state', 'agent-chrome-bridge');
const ACTION_LOG = path.join(LOG_DIR, 'actions.jsonl');

let extensionSocket = null;
let extensionHostInfo = null;
const pending = new Map();
const connections = new Set();
const recentRequests = [];
const recentErrors = [];
const locks = new Map();

const LOCKED_METHODS = new Set([
  'tabs.select',
  'tabs.close',
  'tabs.reload',
  'tabs.back',
  'tabs.forward',
  'page.navigate',
  'page.click',
  'page.type',
  'page.scroll',
  'page.waitForSelector',
  'page.waitForText',
  'page.waitUntilIdle',
  'cdp.click',
  'cdp.key',
  'cdp.type',
]);

const BROKER_LOG_PATH = path.join(LOG_DIR, 'broker.log');
try { fs.mkdirSync(LOG_DIR, { recursive: true, mode: 0o700 }); } catch { /* ignore */ }
function log(message) {
  const line = `[${new Date().toISOString()}] ${message}\n`;
  try { fs.appendFileSync(BROKER_LOG_PATH, line); } catch { /* ignore */ }
  process.stderr.write(`[agent-chrome-bridge broker] ${message}\n`);
}

function sendLine(socket, message) {
  if (socket.destroyed) return;
  socket.write(`${JSON.stringify(message)}\n`);
}

function pushBounded(list, value, max = 50) {
  list.push(value);
  while (list.length > max) list.shift();
}

function redactParams(value) {
  if (Array.isArray(value)) return value.map(redactParams);
  if (!value || typeof value !== 'object') return value;

  const redacted = {};
  for (const [key, child] of Object.entries(value)) {
    if (/text|password|token|secret|cookie|authorization/i.test(key)) {
      if (typeof child === 'string') {
        redacted[key] = `[redacted string length=${child.length}]`;
      } else {
        redacted[key] = '[redacted]';
      }
    } else {
      redacted[key] = redactParams(child);
    }
  }
  return redacted;
}

function appendAction(entry) {
  try {
    fs.mkdirSync(LOG_DIR, { recursive: true, mode: 0o700 });
    fs.appendFile(ACTION_LOG, `${JSON.stringify(entry)}\n`, (error) => {
      if (error) log(`failed to append action log: ${error.message}`);
    });
  } catch (error) {
    log(`failed to prepare action log: ${error.message}`);
  }
}

function recordRequest(message) {
  const entry = {
    ts: new Date().toISOString(),
    id: message.id,
    method: message.method,
    params: redactParams(message.params || {}),
    extensionConnected: Boolean(extensionSocket && !extensionSocket.destroyed),
  };
  pushBounded(recentRequests, entry);
  appendAction(entry);
}

function recordError(id, method, error) {
  const entry = {
    ts: new Date().toISOString(),
    id,
    method,
    error,
  };
  pushBounded(recentErrors, entry);
  appendAction({ ...entry, type: 'error' });
}

function health() {
  cleanupLocks();
  return {
    ok: true,
    pid: process.pid,
    socketPath: SOCKET_PATH,
    logPath: ACTION_LOG,
    uptimeSeconds: Math.round((Date.now() - STARTED_AT) / 1000),
    extensionConnected: Boolean(extensionSocket && !extensionSocket.destroyed),
    broker: {
      version: PACKAGE_VERSION,
      startedAt: new Date(STARTED_AT).toISOString(),
    },
    nativeHost: extensionHostInfo,
    connections: connections.size,
    pending: pending.size,
    locks: listLocks(),
    recentRequests: recentRequests.slice(-10),
    recentErrors: recentErrors.slice(-10),
  };
}

function cleanupLocks() {
  const now = Date.now();
  for (const [tabId, lock] of locks.entries()) {
    if (lock.expiresAt <= now) locks.delete(tabId);
  }
}

function listLocks() {
  cleanupLocks();
  return [...locks.values()].map((lock) => ({
    ...lock,
    ttlMs: Math.max(0, lock.expiresAt - Date.now()),
  }));
}

function acquireLock(params = {}) {
  cleanupLocks();
  if (params.tabId === undefined || params.tabId === null) throw new Error('tabId is required');
  const tabId = Number(params.tabId);
  const existing = locks.get(tabId);
  if (existing) throw new Error(`Tab ${tabId} is already locked by ${existing.owner} until ${existing.expiresAtIso}`);

  const ttlMs = Math.max(1000, Math.min(30 * 60 * 1000, Number(params.ttlMs || 120000)));
  const now = Date.now();
  const lock = {
    lockId: `lock-${tabId}-${now}-${Math.random().toString(36).slice(2, 10)}`,
    tabId,
    owner: String(params.owner || 'agent'),
    note: params.note || undefined,
    createdAt: now,
    createdAtIso: new Date(now).toISOString(),
    expiresAt: now + ttlMs,
    expiresAtIso: new Date(now + ttlMs).toISOString(),
  };
  locks.set(tabId, lock);
  return { ...lock, ttlMs };
}

function releaseLock(params = {}) {
  if (!params.lockId) throw new Error('lockId is required');
  cleanupLocks();
  for (const [tabId, lock] of locks.entries()) {
    if (lock.lockId === params.lockId) {
      locks.delete(tabId);
      return { released: true, lock };
    }
  }
  return { released: false, lockId: params.lockId };
}

function lockViolation(message) {
  cleanupLocks();
  if (!LOCKED_METHODS.has(message.method)) return null;
  if (message.params?.tabId === undefined || message.params?.tabId === null) return null;

  const tabId = Number(message.params.tabId);
  const lock = locks.get(tabId);
  if (!lock) return null;
  if (message.params.lockId === lock.lockId) return null;

  return `Tab ${tabId} is locked by ${lock.owner}. Pass lockId ${lock.lockId} or release the lock.`;
}

function installLineReader(socket, onMessage) {
  let buffer = '';
  socket.setEncoding('utf8');
  socket.on('data', (chunk) => {
    buffer += chunk;
    for (;;) {
      const newline = buffer.indexOf('\n');
      if (newline === -1) break;
      const line = buffer.slice(0, newline).trim();
      buffer = buffer.slice(newline + 1);
      if (!line) continue;
      try {
        onMessage(JSON.parse(line));
      } catch (error) {
        sendLine(socket, { type: 'error', error: `Invalid JSON line: ${error.message}` });
      }
    }
  });
}

function rejectAllForExtension(reason) {
  for (const [id, requester] of pending.entries()) {
    sendLine(requester, { type: 'response', id, error: reason });
    pending.delete(id);
  }
}

function handleExtensionMessage(socket, message) {
  if (message.type === 'hello') {
    extensionSocket = socket;
    extensionHostInfo = {
      pid: message.pid || null,
      version: message.version || null,
      connectedAt: new Date().toISOString(),
    };
    sendLine(socket, { type: 'hello', ok: true, socket: SOCKET_PATH });
    log('extension host connected');
    return;
  }

  if (message.type === 'response') {
    const requester = pending.get(message.id);
    if (!requester) return;
    pending.delete(message.id);
    if (message.error) recordError(message.id, 'extension.response', message.error);
    sendLine(requester, message);
    return;
  }

  // Extension-originated requests (e.g. session.* from the side panel) are
  // forwarded by the native host as {type:'request'} frames on the same
  // extension-host socket. Treat them as client requests.
  if (message.type === 'request' && message.id && message.method) {
    handleClientMessage(socket, message);
    return;
  }

  sendLine(socket, { type: 'error', error: `Unexpected extension message type: ${message.type || 'missing'}` });
}

function handleClientMessage(socket, message) {
  log(`client-msg type=${message.type || '?'} method=${message.method || '-'} id=${message.id || '-'}`);
  if (message.type === 'hello') {
    sendLine(socket, { type: 'hello', ok: true, socket: SOCKET_PATH, extensionConnected: Boolean(extensionSocket) });
    return;
  }

  if (message.type !== 'request' || !message.id || !message.method) {
    sendLine(socket, { type: 'response', id: message.id || null, error: 'Expected request with id and method' });
    return;
  }

  if (message.method === 'bridge.health') {
    sendLine(socket, { type: 'response', id: message.id, result: health() });
    return;
  }

  if (message.method === 'bridge.log') {
    const limit = Math.max(1, Math.min(100, Number(message.params?.limit || 20)));
    sendLine(socket, {
      type: 'response',
      id: message.id,
      result: {
        logPath: ACTION_LOG,
        recentRequests: recentRequests.slice(-limit),
        recentErrors: recentErrors.slice(-limit),
      },
    });
    return;
  }

  if (message.method === 'locks.acquire') {
    try {
      sendLine(socket, { type: 'response', id: message.id, result: acquireLock(message.params || {}) });
    } catch (error) {
      sendLine(socket, { type: 'response', id: message.id, error: error.message });
    }
    return;
  }

  if (message.method === 'locks.release') {
    try {
      sendLine(socket, { type: 'response', id: message.id, result: releaseLock(message.params || {}) });
    } catch (error) {
      sendLine(socket, { type: 'response', id: message.id, error: error.message });
    }
    return;
  }

  if (message.method === 'locks.list') {
    sendLine(socket, { type: 'response', id: message.id, result: { locks: listLocks() } });
    return;
  }


  if (!extensionSocket || extensionSocket.destroyed) {
    recordError(message.id, message.method, 'Chrome bridge extension is not connected.');
    sendLine(socket, {
      type: 'response',
      id: message.id,
      error: 'Chrome bridge extension is not connected. Load the extension in Chrome and make sure the native host is installed.',
    });
    return;
  }

  const violation = lockViolation(message);
  if (violation) {
    recordError(message.id, message.method, violation);
    sendLine(socket, { type: 'response', id: message.id, error: violation });
    return;
  }

  recordRequest(message);
  pending.set(message.id, socket);
  sendLine(extensionSocket, {
    id: message.id,
    method: message.method,
    params: message.params || {},
  });
}

function onConnection(socket) {
  let role = 'client';
  connections.add(socket);

  installLineReader(socket, (message) => {
    if (message.type === 'hello' && message.role === 'extension-host') {
      role = 'extension-host';
      handleExtensionMessage(socket, message);
      return;
    }

    if (role === 'extension-host') {
      handleExtensionMessage(socket, message);
    } else {
      handleClientMessage(socket, message);
    }
  });

  socket.on('close', () => {
    connections.delete(socket);
    if (socket === extensionSocket) {
      extensionSocket = null;
      extensionHostInfo = null;
      rejectAllForExtension('Chrome bridge extension disconnected.');
      log('extension host disconnected');
    }
    for (const [id, requester] of pending.entries()) {
      if (requester === socket) pending.delete(id);
    }
  });
}

function start() {
  try {
    if (fs.existsSync(SOCKET_PATH)) fs.unlinkSync(SOCKET_PATH);
  } catch (error) {
    log(`failed to remove stale socket: ${error.message}`);
    process.exit(1);
  }

  const server = net.createServer(onConnection);
  server.listen(SOCKET_PATH, () => {
    fs.chmodSync(SOCKET_PATH, 0o600);
    log(`listening on ${SOCKET_PATH}`);
  });

  server.on('error', (error) => {
    log(error.stack || error.message);
    process.exit(1);
  });

  process.on('SIGTERM', () => {
    server.close(() => process.exit(0));
  });
}

start();
