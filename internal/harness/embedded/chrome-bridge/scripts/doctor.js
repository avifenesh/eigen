#!/usr/bin/env node
'use strict';

const { request } = require('../lib/broker-client');

async function main() {
  const health = await request('bridge.health');
  process.stdout.write(`${JSON.stringify(health, null, 2)}\n`);
}

main().catch((error) => {
  process.stderr.write(`${error.stack || error.message}\n`);
  process.exit(1);
});
