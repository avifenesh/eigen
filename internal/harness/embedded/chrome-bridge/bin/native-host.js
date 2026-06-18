#!/usr/bin/env node
'use strict';

const childProcess = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const { socketPath } = require('../lib/socket-path');
const { version: PACKAGE_VERSION } = require('../package.json');

const ROOT = path.resolve(__dirname, '..');
const BROKER = path.join(ROOT, 'bin', 'broker.js');
const SOCKET_PATH = socketPath();
const LOG_DIR = path.join(os.homedir(), '.local', 'state', 'agent-chrome-bridge');
const LOG_PATH = path.join(LOG_DIR, 'native-host.log');

try { fs.mkdirSync(LOG_DIR, { recursive: true, mode: 0o700 }); } catch { /* ignore */ }
let broker = null;
let nativeBuffer = Buffer.alloc(0);

function log(message) {
  const line = `[${new Date().toISOString()}] ${message}\n`;
  try { fs.appendFileSync(LOG_PATH, line); } catch { /* ignore */ }
  process.stderr.write(`[agent-chrome-bridge native-host] ${message}\n`);
}

function writeNative(message) {
  const body = Buffer.from(JSON.stringify(message), 'utf8');
  const header = Buffer.alloc(4);
  header.writeUInt32LE(body.length, 0);
  process.stdout.write(Buffer.concat([header, body]));
}

function writeBroker(message) {
  if (!broker || broker.destroyed) {
    log(`drop broker-bound (not connected): ${JSON.stringify(message).slice(0, 200)}`);
    return;
  }
  broker.write(`${JSON.stringify(message)}\n`);
}

function startBroker() {
  const child = childProcess.spawn(process.execPath, [BROKER], {
    detached: true,
    stdio: 'ignore',
    env: process.env,
  });
  child.unref();
}

function connectBroker(attempt = 0) {
  const socket = net.createConnection(SOCKET_PATH);

  socket.on('connect', () => {
    broker = socket;
    writeBroker({ type: 'hello', role: 'extension-host', pid: process.pid, version: PACKAGE_VERSION });
  });

  socket.on('data', makeLineReader((message) => {
    if (message.type === 'response' || (message.id && message.method)) {
      writeNative(message);
    }
  }));

  socket.on('close', () => {
    broker = null;
  });

  socket.on('error', (error) => {
    socket.destroy();
    if ((error.code === 'ENOENT' || error.code === 'ECONNREFUSED') && attempt < 20) {
      if (attempt === 0) startBroker();
      setTimeout(() => connectBroker(attempt + 1), 100);
      return;
    }
    log(`failed to connect broker: ${error.message}`);
    process.exit(1);
  });
}

function makeLineReader(onMessage) {
  let buffer = '';
  return (chunk) => {
    buffer += chunk.toString('utf8');
    for (;;) {
      const newline = buffer.indexOf('\n');
      if (newline === -1) break;
      const line = buffer.slice(0, newline).trim();
      buffer = buffer.slice(newline + 1);
      if (!line) continue;
      try {
        onMessage(JSON.parse(line));
      } catch (error) {
        log(`invalid broker JSON: ${error.message}`);
      }
    }
  };
}

function handleNativeChunk(chunk) {
  nativeBuffer = Buffer.concat([nativeBuffer, chunk]);
  while (nativeBuffer.length >= 4) {
    const size = nativeBuffer.readUInt32LE(0);
    if (nativeBuffer.length < 4 + size) return;
    const body = nativeBuffer.subarray(4, 4 + size);
    nativeBuffer = nativeBuffer.subarray(4 + size);

    try {
      const message = JSON.parse(body.toString('utf8'));
      log(`from-ext ${message.type || '(tool-response)'} id=${message.id || '-'} method=${message.method || '-'}`);
      writeBroker({
        type: 'response',
        id: message.id,
        result: message.result,
        error: message.error,
      });
    } catch (error) {
      log(`invalid ext JSON: ${error.message}`);
      writeBroker({ type: 'response', id: null, error: `Invalid extension JSON: ${error.message}` });
    }
  }
}

process.stdin.on('data', handleNativeChunk);
process.stdin.on('end', () => process.exit(0));

connectBroker();
