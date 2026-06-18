#!/usr/bin/env node
// action-graph ingest — single source of truth. Normalizes agent transcripts
// (the JSONL that already records EVERYTHING: user prompts, agent text, tool calls,
// todos, commits) into append-only raw.jsonl. Derived episodes/graphs can be
// rebuilt, but raw events are preserved and de-duped by transcript evidence.
//
//   node ingest.js            → append unseen Claude records for allowlisted projects
//   node ingest.js <cwd>      → append unseen Claude records for one project
//   node ingest.js --force    → rescan all Claude sources, still append-only
//   node ingest.js --runtime claude|codex|eigen --source <jsonl> --cwd <cwd> --cursor
//                             → append only new records from one transcript
//
// Allowlist entries are cwd PREFIXES. A line `/home/you/projects` matches
// every project (and nested repo) under it — each distinct session cwd becomes its
// own project. Discovery reads the real cwd from each transcript (dir-name dashing
// is ambiguous); incremental skip via per-project mtime manifest keeps Stop fast
// even across ~1GB / 185 dirs.

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { parseRows, inferCwd, resolveAdapter } = require('./adapters');
const { projectKey, sourceKey, inspectProject, eventIdentity } = require('./project');
const { ORIENTATION_HOME, DATA_DIR, ALLOWLIST_FILE, CLAUDE_PROJECTS_DIR, CODEX_SESSIONS_DIR, EIGEN_DIR } = require('./state');

const ROOT = DATA_DIR;
const PROJECTS_DIR = CLAUDE_PROJECTS_DIR;
const FINGERPRINT_BYTES = 64 * 1024;

function allowlist() {
  const f = ALLOWLIST_FILE;
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, 'utf8').split('\n').map(s => s.trim()).filter(s => s && !s.startsWith('#'));
}

// Read the real cwd a transcript dir belongs to. Dir names dash-encode the path
// ambiguously (a literal '-' is indistinguishable from a '/' separator), so we
// trust the `cwd` field inside the records instead of decoding the name.
function dirCwd(dir) {
  let f;
  try { f = fs.readdirSync(dir).find(x => x.endsWith('.jsonl')); } catch { return null; }
  if (!f) return null;
  // scan first N lines for a cwd field (first record sometimes lacks it)
  const lines = readHead(path.join(dir, f), 200);
  for (const line of lines) {
    if (!line.trim()) continue;
    try { const o = JSON.parse(line); if (o.cwd) return o.cwd; } catch {}
  }
  return null;
}

// Read up to n lines without loading a 160MB file whole.
function readHead(file, n) {
  const fd = fs.openSync(file, 'r');
  try {
    const buf = Buffer.alloc(64 * 1024);
    let data = '', read;
    while ((data.match(/\n/g) || []).length < n && (read = fs.readSync(fd, buf, 0, buf.length, null)) > 0) {
      data += buf.toString('utf8', 0, read);
    }
    return data.split('\n').slice(0, n);
  } finally { fs.closeSync(fd); }
}

function readJsonlChunk(file, startOffset = 0, startLine = 0) {
  const stat = fs.statSync(file);
  let offset = startOffset;
  let line = startLine;
  if (offset > stat.size) { offset = 0; line = 0; }
  const fd = fs.openSync(file, 'r');
  try {
    const buf = Buffer.alloc(stat.size - offset);
    if (!buf.length) return { rows: [], nextOffset: offset, nextLine: line, size: stat.size };
    fs.readSync(fd, buf, 0, buf.length, offset);
    const lastNl = buf.lastIndexOf(10);
    if (lastNl === -1) return { rows: [], nextOffset: offset, nextLine: line, size: stat.size };

    const complete = buf.subarray(0, lastNl + 1);
    const rows = [];
    let pos = 0;
    while (pos < complete.length) {
      const nl = complete.indexOf(10, pos);
      const end = nl === -1 ? complete.length : nl;
      const lineBuf = complete.subarray(pos, end);
      line++;
      if (lineBuf.length) {
        try {
          const row = JSON.parse(lineBuf.toString('utf8'));
          row.__sourceLine = line;
          row.__sourceOffset = offset + pos;
          rows.push(row);
        } catch {}
      }
      pos = end + 1;
    }
    return { rows, nextOffset: offset + complete.length, nextLine: line, size: stat.size };
  } finally { fs.closeSync(fd); }
}

function readRange(file, start, length) {
  if (length <= 0) return Buffer.alloc(0);
  const fd = fs.openSync(file, 'r');
  try {
    const buf = Buffer.alloc(length);
    const read = fs.readSync(fd, buf, 0, length, start);
    return read === length ? buf : buf.subarray(0, read);
  } finally { fs.closeSync(fd); }
}

function bufferHash(buf) {
  return crypto.createHash('sha1').update(buf).digest('hex');
}

function sourceFingerprint(file) {
  const stat = fs.statSync(file);
  const headLen = Math.min(FINGERPRINT_BYTES, stat.size);
  const tailStart = Math.max(0, stat.size - FINGERPRINT_BYTES);
  const tailLen = stat.size - tailStart;
  return {
    algorithm: 'sha1-head-tail-v1',
    bytes: FINGERPRINT_BYTES,
    size: stat.size,
    mtimeMs: stat.mtimeMs,
    headHash: bufferHash(readRange(file, 0, headLen)),
    tailHash: bufferHash(readRange(file, tailStart, tailLen)),
  };
}

function cursorResetReason(cursor, fingerprint) {
  if (!cursor) return null;
  if ((cursor.offset || 0) > fingerprint.size) return 'source shrank';
  if (cursor.fingerprint?.headHash && cursor.fingerprint.headHash !== fingerprint.headHash) {
    return 'source fingerprint changed';
  }
  return null;
}

function sourceCwd(file, runtime = 'claude') {
  const lines = readHead(file, 200);
  const rows = [];
  for (const line of lines) {
    if (!line.trim()) continue;
    try { rows.push(JSON.parse(line)); } catch {}
  }
  return inferCwd(runtime, rows, { source: file });
}

// Latest mtime across a dir's transcripts — the incremental-skip key.
function dirMtime(dir) {
  let m = 0;
  for (const f of fs.readdirSync(dir)) {
    if (!f.endsWith('.jsonl')) continue;
    const mt = fs.statSync(path.join(dir, f)).mtimeMs;
    if (mt > m) m = mt;
  }
  return m;
}

function readJsonlFile(file) {
  const rows = [];
  const data = fs.readFileSync(file, 'utf8');
  let offset = 0;
  let lineNo = 0;
  for (const line of data.split('\n')) {
    lineNo++;
    if (line.trim()) {
      try {
        const row = JSON.parse(line);
        row.__sourceLine = lineNo;
        row.__sourceOffset = offset;
        rows.push(row);
      } catch {}
    }
    offset += Buffer.byteLength(line) + 1;
  }
  return rows;
}

function parseTranscript(file, meta = {}) {
  return parseRows('claude', readJsonlFile(file), { ...meta, runtime: 'claude', source: meta.source || file });
}

function stableString(value) {
  if (Array.isArray(value)) return `[${value.map(stableString).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(k => `${JSON.stringify(k)}:${stableString(value[k])}`).join(',')}}`;
  }
  return JSON.stringify(value);
}

function recordKey(record) {
  if (record.source && (record.sourceLine != null || record.sourceOffset != null)) {
    return [
      'src',
      record.runtime || '',
      record.adapter || '',
      record.source,
      record.sourceLine ?? '',
      record.sourceOffset ?? '',
      record.kind || '',
      record.turn || '',
      record.tool || '',
      record.session || '',
    ].join('|');
  }
  if (record.source && record.turn && record.kind) {
    return ['turn', record.runtime || '', record.adapter || '', record.source, record.turn, record.kind, record.tool || '', record.session || ''].join('|');
  }
  return 'hash|' + crypto.createHash('sha1').update(stableString(record)).digest('hex');
}

function rawIndex(outDir) {
  const f = path.join(outDir, 'raw.jsonl');
  const keys = new Set();
  let count = 0;
  if (!fs.existsSync(f)) return { keys, count };
  for (const line of fs.readFileSync(f, 'utf8').split('\n')) {
    if (!line.trim()) continue;
    count++;
    try { keys.add(recordKey(JSON.parse(line))); } catch {}
  }
  return { keys, count };
}

function appendUniqueRaw(outDir, records) {
  fs.mkdirSync(outDir, { recursive: true });
  if (!records.length) {
    const { count } = rawIndex(outDir);
    return { appended: 0, total: count };
  }
  const { keys, count } = rawIndex(outDir);
  const fresh = [];
  for (const record of records) {
    const key = recordKey(record);
    if (keys.has(key)) continue;
    keys.add(key);
    fresh.push(record);
  }
  if (fresh.length) fs.appendFileSync(path.join(outDir, 'raw.jsonl'), fresh.map(r => JSON.stringify(r)).join('\n') + '\n');
  return { appended: fresh.length, total: count + fresh.length };
}

function rebuildProject(dir, cwd) {
  const files = fs.readdirSync(dir).filter(f => f.endsWith('.jsonl'))
    .map(f => path.join(dir, f))
    .sort((a, b) => fs.statSync(a).mtimeMs - fs.statSync(b).mtimeMs); // oldest first

  const identity = inspectProject(cwd);
  let all = [];
  for (const f of files) {
    all = all.concat(parseTranscript(f, {
      runtime: 'claude',
      source: f,
      cwd,
      identity: eventIdentity(identity),
    }));
  }

  const outDir = path.join(ROOT, projectKey(cwd));
  const manifest = path.join(outDir, '.manifest.json');
  let prior = {};
  try { prior = JSON.parse(fs.readFileSync(manifest, 'utf8')); } catch {}
  const write = appendUniqueRaw(outDir, all);
  fs.writeFileSync(path.join(outDir, '.manifest.json'), JSON.stringify({
    ...prior,
    cwd,
    ...eventIdentity(identity),
    currentBranch: identity.currentBranch,
    srcMtime: dirMtime(dir),
    fullRefreshAppendOnly: true,
    records: write.total,
    lastFullRefresh: new Date().toISOString(),
  }, null, 2));
  return { cwd, key: projectKey(cwd), records: write.appended, totalRecords: write.total, sessions: files.length };
}

function prefixMatch(cwd, prefixes) {
  return prefixes.some(p => cwd === p || cwd.startsWith(p.endsWith('/') ? p : p + '/'));
}

// Already-built and source unchanged since last build?
function isFresh(cwd, dir) {
  const mf = path.join(ROOT, projectKey(cwd), '.manifest.json');
  if (!fs.existsSync(path.join(ROOT, projectKey(cwd), 'raw.jsonl'))) return false;
  if (!fs.existsSync(mf)) return false;
  try { return JSON.parse(fs.readFileSync(mf, 'utf8')).srcMtime === dirMtime(dir); } catch { return false; }
}

function cursorFile(runtime, source) {
  return path.join(ROOT, '_cursors', `${sourceKey(runtime, source)}.json`);
}

function loadCursor(runtime, source) {
  const f = cursorFile(runtime, source);
  if (!fs.existsSync(f)) return null;
  try { return JSON.parse(fs.readFileSync(f, 'utf8')); } catch { return null; }
}

function saveCursor(runtime, source, cursor) {
  const f = cursorFile(runtime, source);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  const tmp = `${f}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(cursor, null, 2));
  fs.renameSync(tmp, f);
}

function appendRaw(cwd, records, identity = inspectProject(cwd)) {
  const outDir = path.join(ROOT, projectKey(cwd));
  const write = appendUniqueRaw(outDir, records);
  const manifest = path.join(outDir, '.manifest.json');
  let prior = {};
  try { prior = JSON.parse(fs.readFileSync(manifest, 'utf8')); } catch {}
  fs.writeFileSync(manifest, JSON.stringify({
    ...prior,
    cwd,
    ...eventIdentity(identity),
    currentBranch: identity.currentBranch,
    cursorIngest: true,
    records: write.total,
    lastCursorIngest: new Date().toISOString(),
  }, null, 2));
  return { outDir, appended: write.appended, totalRecords: write.total };
}

function ingestSourceWithCursor(opts) {
  const adapter = resolveAdapter(opts.runtime, opts.source);
  if (!opts.source) throw new Error('--source is required');
  if (!fs.existsSync(opts.source)) throw new Error(`source does not exist: ${opts.source}`);

  let cursor = opts.cursor ? loadCursor(adapter.name, opts.source) : null;
  const fingerprint = opts.cursor ? sourceFingerprint(opts.source) : null;
  const resetReason = opts.cursor ? cursorResetReason(cursor, fingerprint) : null;
  if (resetReason) cursor = { ...cursor, offset: 0, line: 0 };
  const chunk = readJsonlChunk(opts.source, cursor?.offset || 0, cursor?.line || 0);
  const cwd = opts.cwd || cursor?.cwd
    || inferCwd(adapter.name, chunk.rows, { source: opts.source })
    || sourceCwd(opts.source, adapter.name);
  if (!cwd) throw new Error(`could not determine cwd for ${opts.source}; pass --cwd`);
  const identity = inspectProject(cwd);
  const gitBranch = opts.gitBranch || identity.currentBranch || null;
  const gitBranchSource = opts.gitBranch ? (opts.gitBranchSource || 'cli') : (gitBranch ? 'inferred' : null);

  const prefixes = allowlist();
  if (!opts.allowUnlisted && prefixes.length && !prefixMatch(cwd, prefixes)) {
    return { skipped: true, reason: 'outside allowlist', cwd, records: 0, rows: chunk.rows.length };
  }

  const records = parseRows(adapter.name, chunk.rows, {
    runtime: adapter.runtime,
    adapter: adapter.name,
    sourceKind: adapter.sourceKind,
    source: opts.source,
    cwd,
    session: cursor?.session || path.basename(opts.source, '.jsonl'),
    gitBranch,
    gitBranchSource,
    identity: eventIdentity(identity),
  });
  const write = appendRaw(cwd, records, identity);

  if (opts.cursor) {
    const nextCursor = {
      runtime: adapter.runtime,
      adapter: adapter.name,
      source: opts.source,
      sourceKey: sourceKey(adapter.name, opts.source),
      session: records.find(r => r.session)?.session || cursor?.session || path.basename(opts.source, '.jsonl'),
      cwd,
      ...eventIdentity(identity),
      currentBranch: identity.currentBranch,
      offset: chunk.nextOffset,
      line: chunk.nextLine,
      size: chunk.size,
      fingerprint,
      records: (cursor?.records || 0) + write.appended,
      lastEventTime: records.map(r => r.t).filter(Boolean).pop() || cursor?.lastEventTime || null,
      resets: (cursor?.resets || 0) + (resetReason ? 1 : 0),
      lastResetReason: resetReason || cursor?.lastResetReason || undefined,
      lastResetAt: resetReason ? new Date().toISOString() : cursor?.lastResetAt || undefined,
      updated: new Date().toISOString(),
    };
    saveCursor(adapter.name, opts.source, nextCursor);
  }

  return {
    skipped: false,
    runtime: adapter.runtime,
    adapter: adapter.name,
    cwd,
    key: projectKey(cwd),
    outDir: write.outDir,
    rows: chunk.rows.length,
    records: write.appended,
    parsedRecords: records.length,
    totalRecords: write.totalRecords,
    offset: chunk.nextOffset,
    line: chunk.nextLine,
    cursorReset: resetReason,
  };
}

function walkFiles(root, predicate, out = []) {
  if (!fs.existsSync(root)) return out;
  for (const name of fs.readdirSync(root)) {
    const p = path.join(root, name);
    let st;
    try { st = fs.statSync(p); } catch { continue; }
    if (st.isDirectory()) walkFiles(p, predicate, out);
    else if (predicate(p, name)) out.push(p);
  }
  return out;
}

function discoverSources(runtimeOrAdapter) {
  const raw = String(runtimeOrAdapter || 'claude').replace(/_/g, '-');
  if (raw === 'eigen') return discoverSources('eigen-session').concat(discoverSources('eigen-task'));
  const adapter = resolveAdapter(runtimeOrAdapter);
  if (adapter.name === 'claude') {
    if (!fs.existsSync(PROJECTS_DIR)) return [];
    return fs.readdirSync(PROJECTS_DIR).flatMap(name => {
      const dir = path.join(PROJECTS_DIR, name);
      try { if (!fs.statSync(dir).isDirectory()) return []; } catch { return []; }
      return fs.readdirSync(dir)
        .filter(f => f.endsWith('.jsonl'))
        .map(f => ({ adapter: 'claude', runtime: 'claude', source: path.join(dir, f), cwd: dirCwd(dir) }));
    }).filter(s => s.cwd);
  }
  if (adapter.name === 'codex') {
    return walkFiles(CODEX_SESSIONS_DIR, (_p, name) => /^rollout-.*\.jsonl$/.test(name))
      .map(source => ({ adapter: 'codex', runtime: 'codex', source }));
  }
  if (adapter.name === 'eigen-session') {
    return walkFiles(EIGEN_DIR, (p, name) => name.endsWith('.jsonl') && p.includes(`${path.sep}sessions${path.sep}`) && !name.endsWith('.meta.json'))
      .map(source => ({ adapter: 'eigen-session', runtime: 'eigen', sourceKind: 'session', source }));
  }
  if (adapter.name === 'eigen-task') {
    return walkFiles(path.join(EIGEN_DIR, 'tasks'), (_p, name) => name.endsWith('.transcript.jsonl'))
      .map(source => ({ adapter: 'eigen-task', runtime: 'eigen', sourceKind: 'task', source }));
  }
  return [];
}

function valueAfter(args, name) {
  const prefix = `${name}=`;
  const inline = args.find(a => a.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const idx = args.indexOf(name);
  return idx === -1 ? undefined : args[idx + 1];
}

function main() {
  const args = process.argv.slice(2);
  const force = args.includes('--force');
  const source = valueAfter(args, '--source');
  const runtime = valueAfter(args, '--runtime') || 'claude';
  const discover = args.includes('--discover');
  const cwd = valueAfter(args, '--cwd');
  const gitBranch = valueAfter(args, '--branch');
  const gitBranchSource = valueAfter(args, '--branch-source');
  if (source || discover) {
    const sources = source ? [{ source, cwd }] : discoverSources(runtime);
    if (discover && !sources.length) {
      console.log(`no sources discovered for ${runtime}`);
      return;
    }
    let built = 0, skipped = 0, errored = 0, records = 0;
    for (const item of sources) {
      try {
        const r = ingestSourceWithCursor({
          runtime: item.adapter || runtime,
          source: item.source,
          cwd: item.cwd || cwd,
          gitBranch,
          gitBranchSource,
          cursor: args.includes('--cursor'),
          allowUnlisted: args.includes('--allow-unlisted'),
        });
        if (r.skipped) { skipped++; if (!discover) console.log(`skipped ${item.source}: ${r.reason} (${r.cwd})`); }
        else {
          built++;
          records += r.records;
          console.log(`cursor-ingested ${r.records} records from ${r.rows} row(s) via ${r.adapter} → ${r.key} (${r.cwd}) offset=${r.offset}`);
        }
      } catch (e) {
        errored++;
        console.error(`cursor ingest failed for ${item.source}: ${e.message}`);
        if (!discover) process.exit(1);
      }
    }
    if (discover) console.log(`discover ingest done: ${built} ingested, ${skipped} skipped, ${errored} errored, ${records} records`);
    if (errored && !built && !skipped) process.exit(1);
    if (!discover && errored) process.exit(1);
    if (!discover && skipped) return;
    if (!discover) {
      // single-source success already printed above
    }
    return;
  }
  const target = args.find(a => !a.startsWith('--'));

  // Explicit single-cwd mode: map cwd→dir by re-dashing (force rebuild).
  if (target) {
    const dir = path.join(PROJECTS_DIR, target.replace(/\//g, '-'));
    if (!fs.existsSync(dir)) { console.log(`no transcript dir for ${target}`); return; }
    const r = rebuildProject(dir, target);
    console.log(`ingested ${r.records} records (${r.sessions} sessions) → ${r.key} (${target})`);
    return;
  }

  const prefixes = allowlist();
  if (!prefixes.length) { console.log('no projects in allowlist'); return; }

  let built = 0, skipped = 0, nomatch = 0, errored = 0;
  for (const name of fs.readdirSync(PROJECTS_DIR)) {
    const dir = path.join(PROJECTS_DIR, name);
    try {
      if (!fs.statSync(dir).isDirectory()) continue;
      const cwd = dirCwd(dir);
      if (!cwd || !prefixMatch(cwd, prefixes)) { nomatch++; continue; }
      if (!force && isFresh(cwd, dir)) { skipped++; continue; }
      const r = rebuildProject(dir, cwd);
      console.log(`ingested ${r.records} records (${r.sessions} sessions) → ${r.key} (${cwd})`);
      built++;
    } catch (e) {
      // one malformed transcript must not abort the whole batch
      errored++;
      console.log(`error ${name}: ${e.message}`);
    }
  }
  console.log(`done: ${built} built, ${skipped} unchanged, ${nomatch} outside allowlist, ${errored} errored (${ORIENTATION_HOME})`);
}

main();
