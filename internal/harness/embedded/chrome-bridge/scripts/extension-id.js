#!/usr/bin/env node
'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const manifestPath = path.resolve(__dirname, '..', 'extension', 'manifest.json');
const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
const der = Buffer.from(manifest.key, 'base64');
const hash = crypto.createHash('sha256').update(der).digest().subarray(0, 16);
const alphabet = 'abcdefghijklmnop';
const id = [...hash].map((byte) => alphabet[byte >> 4] + alphabet[byte & 0x0f]).join('');

console.log(id);
