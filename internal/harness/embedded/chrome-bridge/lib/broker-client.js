'use strict';

const childProcess = require('node:child_process');
const net = require('node:net');
const path = require('node:path');
const { socketPath } = require('./socket-path');

const ROOT = path.resolve(__dirname, '..');
const BROKER = path.join(ROOT, 'bin', 'broker.js');
const SOCKET_PATH = socketPath();

let counter = 0;

function nextId() {
  return `req-${process.pid}-${Date.now()}-${++counter}`;
}

function startBroker() {
  const child = childProcess.spawn(process.execPath, [BROKER], {
    detached: true,
    stdio: 'ignore',
    env: process.env,
  });
  child.unref();
}

function connect(attempt = 0) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(SOCKET_PATH);

    socket.on('connect', () => resolve(socket));
    socket.on('error', (error) => {
      socket.destroy();
      if ((error.code === 'ENOENT' || error.code === 'ECONNREFUSED') && attempt < 20) {
        if (attempt === 0) startBroker();
        setTimeout(() => {
          connect(attempt + 1).then(resolve, reject);
        }, 100);
        return;
      }
      reject(error);
    });
  });
}

function request(method, params = {}, options = {}) {
  const id = nextId();

  return new Promise(async (resolve, reject) => {
    let socket;
    let buffer = '';
    try {
      socket = await connect();
    } catch (error) {
      reject(error);
      return;
    }

    const timeout = setTimeout(() => {
      socket.destroy();
      reject(new Error(`Timed out waiting for Chrome bridge response to ${method}`));
    }, Number(options.timeoutMs || 30000));

    socket.setEncoding('utf8');
    socket.on('data', (chunk) => {
      buffer += chunk;
      for (;;) {
        const newline = buffer.indexOf('\n');
        if (newline === -1) break;
        const line = buffer.slice(0, newline).trim();
        buffer = buffer.slice(newline + 1);
        if (!line) continue;

        let message;
        try {
          message = JSON.parse(line);
        } catch (error) {
          clearTimeout(timeout);
          socket.destroy();
          reject(error);
          return;
        }

        if (message.id !== id) continue;
        clearTimeout(timeout);
        socket.end();
        if (message.error) {
          reject(new Error(message.error));
        } else {
          resolve(message.result);
        }
      }
    });

    socket.on('error', (error) => {
      clearTimeout(timeout);
      reject(error);
    });

    socket.write(`${JSON.stringify({ type: 'request', id, method, params })}\n`);
  });
}

module.exports = { SOCKET_PATH, request };
