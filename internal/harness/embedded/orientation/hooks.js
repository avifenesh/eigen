#!/usr/bin/env node
// orientation hook manager. Claude Code and Eigen hooks can be installed
// directly. Codex is harness-owned until its turn-end hook contract is known.

const fs = require('fs');
const path = require('path');
const { CLAUDE, EIGEN, ENGINE_DIR, ORIENTATION_HOME } = require('./state');

const SETTINGS = path.join(CLAUDE, 'settings.json');
const EIGEN_HOOKS = path.join(EIGEN, 'hooks.json');
const EIGEN_ORIENTATION = process.env.EIGEN_ORIENTATION_DIR || path.join(EIGEN, 'orientation');
const EIGEN_ENGINE_DIR = process.env.EIGEN_ORIENTATION_ENGINE_DIR || EIGEN_ORIENTATION;
const EIGEN_ORIENTATION_HOME = process.env.EIGEN_ORIENTATION_HOME || EIGEN_ORIENTATION;
const EIGEN_ORIENTATION_BIN = process.env.EIGEN_ORIENTATION_BIN || path.join(path.dirname(EIGEN), '.local', 'bin', 'orientation');
const NODE = process.execPath;
const EVENTS = ['SessionStart', 'Stop', 'PreCompact'];
const EIGEN_EVENTS = ['turn_done', 'session_stop', 'note'];

function shellQuote(value) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

function readSettings() {
  if (!fs.existsSync(SETTINGS)) return {};
  return JSON.parse(fs.readFileSync(SETTINGS, 'utf8'));
}

function writeSettings(settings) {
  fs.mkdirSync(path.dirname(SETTINGS), { recursive: true });
  fs.writeFileSync(SETTINGS, JSON.stringify(settings, null, 2));
}

function commandText(command) {
  if (Array.isArray(command)) return command.join(' ');
  return typeof command === 'string' ? command : '';
}

function isOrientationCommand(command) {
  return /(action-graph|ORIENTATION_HOME|ORIENTATION_ENGINE_DIR|orientation.*hook|eigen orientation)/.test(commandText(command));
}

function ensureHook(hooks, event, command, extra = {}) {
  hooks[event] = hooks[event] || [];
  let found = false;
  let changed = false;
  for (const grp of hooks[event]) {
    for (const h of grp.hooks || []) {
      if (!isOrientationCommand(h.command)) continue;
      found = true;
      const next = { type: 'command', command, ...extra };
      for (const [k, v] of Object.entries(next)) {
        if (h[k] !== v) { h[k] = v; changed = true; }
      }
    }
  }
  if (found) return changed;
  hooks[event].push({ hooks: [{ type: 'command', command, ...extra }] });
  return true;
}

function commands(home = ORIENTATION_HOME, engine = ENGINE_DIR) {
  const env = `ORIENTATION_HOME=${shellQuote(home)} ORIENTATION_ENGINE_DIR=${shellQuote(engine)}`;
  return {
    consume: `${env} ${shellQuote(NODE)} ${shellQuote(path.join(engine, 'consume.js'))}`,
    hook: `${env} ${shellQuote(NODE)} ${shellQuote(path.join(engine, 'hook.js'))}`,
  };
}

function installClaude(label = 'installed') {
  const settings = readSettings();
  settings.hooks = settings.hooks || {};
  const cmd = commands();
  let changed = 0;
  if (ensureHook(settings.hooks, 'SessionStart', cmd.consume, { timeout: 5, statusMessage: 'Loading orientation...' })) changed++;
  if (ensureHook(settings.hooks, 'Stop', cmd.hook, { timeout: 20 })) changed++;
  if (ensureHook(settings.hooks, 'PreCompact', cmd.hook, { timeout: 20 })) changed++;
  if (changed) writeSettings(settings);
  console.log(`claude-code hooks ${changed ? label : 'already present'} (${changed} changed)`);
}

function removeClaude() {
  const settings = readSettings();
  const hooks = settings.hooks || {};
  let changed = 0;
  for (const event of EVENTS) {
    const groups = hooks[event] || [];
    const keptGroups = [];
    for (const grp of groups) {
      const oldHooks = grp.hooks || [];
      const keptHooks = oldHooks.filter(h => !isOrientationCommand(h.command));
      if (keptHooks.length !== oldHooks.length) changed += oldHooks.length - keptHooks.length;
      if (keptHooks.length) keptGroups.push({ ...grp, hooks: keptHooks });
    }
    hooks[event] = keptGroups;
  }
  settings.hooks = hooks;
  if (changed) writeSettings(settings);
  console.log(`claude-code hooks ${changed ? 'removed' : 'not present'} (${changed} removed)`);
}

function statusClaude() {
  let settings = {};
  try { settings = readSettings(); } catch {}
  const hooks = settings.hooks || {};
  for (const event of EVENTS) {
    const commands = (hooks[event] || []).flatMap(group => group.hooks || [])
      .map(h => h.command)
      .filter(isOrientationCommand);
    const cursor = commands.some(c => c.includes('hook.js'));
    console.log(`${event.padEnd(14)} ${commands.length ? (cursor ? 'installed (cursor)' : 'installed') : 'missing'}`);
  }
}

function readEigenHooks() {
  if (!fs.existsSync(EIGEN_HOOKS)) return { hooks: [] };
  const parsed = JSON.parse(fs.readFileSync(EIGEN_HOOKS, 'utf8'));
  if (Array.isArray(parsed)) return { hooks: parsed };
  if (!parsed || typeof parsed !== 'object') return { hooks: [] };
  return { ...parsed, hooks: Array.isArray(parsed.hooks) ? parsed.hooks : [] };
}

function writeEigenHooks(config) {
  fs.mkdirSync(path.dirname(EIGEN_HOOKS), { recursive: true });
  fs.writeFileSync(EIGEN_HOOKS, JSON.stringify(config, null, 2));
}

function eigenCommand() {
  return ['sh', '-c', `${shellQuote(EIGEN_ORIENTATION_BIN)} hook --runtime ${shellQuote('eigen')}`];
}

function isEigenOrientationHook(spec) {
  return spec && EIGEN_EVENTS.includes(spec.event) && isOrientationCommand(spec.command);
}

function sameCommand(a, b) {
  return JSON.stringify(a || null) === JSON.stringify(b || null);
}

function installEigen(label = 'installed') {
  const config = readEigenHooks();
  let changed = 0;
  for (const event of EIGEN_EVENTS) {
    const matching = config.hooks
      .map((hook, index) => ({ hook, index }))
      .filter(({ hook }) => hook && hook.event === event && isOrientationCommand(hook.command));
    if (!matching.length) {
      config.hooks.push({ event, command: eigenCommand() });
      changed++;
      continue;
    }
    const primary = matching[0].hook;
    const next = { ...primary, event, command: eigenCommand() };
    delete next.disabled;
    if (!sameCommand(primary.command, next.command) || primary.disabled) {
      Object.keys(primary).forEach(k => delete primary[k]);
      Object.assign(primary, next);
      changed++;
    }
    if (matching.length > 1) {
      const duplicates = new Set(matching.slice(1).map(m => m.index));
      config.hooks = config.hooks.filter((_hook, index) => !duplicates.has(index));
      changed += duplicates.size;
    }
  }
  if (changed) writeEigenHooks(config);
  console.log(`eigen hooks ${changed ? label : 'already present'} (${changed} changed)`);
  console.log(`config ${EIGEN_HOOKS}`);
}

function removeEigen() {
  const config = readEigenHooks();
  const before = config.hooks.length;
  config.hooks = config.hooks.filter(hook => !isEigenOrientationHook(hook));
  const removed = before - config.hooks.length;
  if (removed) writeEigenHooks(config);
  console.log(`eigen hooks ${removed ? 'removed' : 'not present'} (${removed} removed)`);
}

function statusEigen() {
  let config = { hooks: [] };
  try { config = readEigenHooks(); } catch {}
  console.log(`config ${EIGEN_HOOKS}`);
  for (const event of EIGEN_EVENTS) {
    const hooks = config.hooks.filter(hook => hook && hook.event === event && isOrientationCommand(hook.command));
    const disabled = hooks.some(hook => hook.disabled);
    console.log(`${event.padEnd(14)} ${hooks.length ? (disabled ? 'installed (disabled)' : 'installed (cursor)') : 'missing'}`);
  }
}

function valueAfter(args, name) {
  const prefix = `${name}=`;
  const inline = args.find(a => a.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const idx = args.indexOf(name);
  return idx === -1 ? undefined : args[idx + 1];
}

function normalizeRuntime(runtime) {
  const r = String(runtime || 'claude-code').replace(/_/g, '-');
  if (r === 'claude') return 'claude-code';
  return r;
}

function harnessContract(runtime) {
  const cmd = `${commands().hook} --runtime ${shellQuote(runtime)}`;
  console.log(`${runtime} hooks are harness-owned.

Call this command at end-of-turn and before compaction:
${cmd}

Send JSON on stdin:
{
  "runtime": "${runtime}",
  "source": "/path/to/session-or-task.jsonl",
  "cwd": "/path/to/repo",
  "event": "TurnEnd",
  "sourceKind": "session",
  "branch": "main"
}

Add "allowUnlisted": true only for harness-managed projects that should bypass projects.txt.`);
}

function handleHarnessRuntime(action, runtime) {
  if (runtime === 'eigen') {
    if (action === 'status') statusEigen();
    else if (action === 'install') installEigen('installed');
    else if (action === 'repair') installEigen('repaired');
    else if (action === 'remove') removeEigen();
    else { usage(); process.exit(1); }
    return;
  }
  if (action === 'remove') {
    console.log(`${runtime} hooks are harness-owned; remove the command from your harness config.`);
    return;
  }
  harnessContract(runtime);
}

function usage() {
  console.log(`usage:
  orientation hooks status [--runtime claude-code|eigen|codex]
  orientation hooks install --runtime claude-code
  orientation hooks install --runtime eigen
  orientation hooks repair  --runtime claude-code
  orientation hooks repair  --runtime eigen
  orientation hooks remove  --runtime claude-code
  orientation hooks remove  --runtime eigen`);
}

function main() {
  const args = process.argv.slice(2);
  const action = args[0] || 'status';
  const runtime = normalizeRuntime(valueAfter(args, '--runtime') || args.find((a, i) => i > 0 && !a.startsWith('--')));
  if (runtime !== 'claude-code') {
    handleHarnessRuntime(action, runtime);
    return;
  }

  if (action === 'status') statusClaude();
  else if (action === 'install') installClaude('installed');
  else if (action === 'repair') installClaude('repaired');
  else if (action === 'remove') removeClaude();
  else { usage(); process.exit(1); }
}

main();
