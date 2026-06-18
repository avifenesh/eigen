// action-graph shared classify — used by ingest.js (and consume.js for branch
// markers). Single source of truth for turning transcript events into records.

function classify(tool, input, result) {
  const i = input || {};
  switch (tool) {
    case 'Edit':
    case 'MultiEdit':
      return { kind: 'edit', files: [i.file_path].filter(Boolean) };
    case 'Write':
      return { kind: 'write', files: [i.file_path].filter(Boolean) };
    case 'NotebookEdit':
      return { kind: 'edit', files: [i.notebook_path].filter(Boolean) };
    case 'Read':
    case 'Grep':
    case 'Glob':
      return { kind: 'explore', files: [i.file_path || i.path].filter(Boolean), q: i.pattern || i.query };
    case 'Bash': {
      const cmd = (i.command || '').trim();
      const reason = i.description || extractReason(cmd);
      let kind = 'run';
      if (/\bgit\s+commit\b/.test(cmd)) kind = 'commit';
      else if (/\bgit\s+push\b/.test(cmd)) kind = 'push';
      else if (/\b(pytest|jest|vitest|cargo test|go test|npm test|npm run test)\b/.test(cmd)) kind = 'test';
      else if (/\b(npm run build|cargo build|make|tsc|go build)\b/.test(cmd)) kind = 'build';
      const out = { kind, cmd: cmd.slice(0, 200), reason };
      const branch = extractBranch(cmd, result);
      if (branch) { out.gitBranch = branch; out.gitBranchSource = 'transcript'; }
      // commit message points BACKWARD at the goal that just closed — high-value label
      if (kind === 'commit') { const m = commitMsg(cmd); if (m) out.msg = m; }
      return out;
    }
    case 'Task':
    case 'Agent':
      return { kind: 'delegate', agent: i.subagent_type, desc: i.description };
    case 'Skill':
      return { kind: 'skill', skill: i.skill };
    default:
      return { kind: 'other', tool };
  }
}

function extractBranch(cmd, result) {
  const s = result == null ? '' : (typeof result === 'string' ? result : JSON.stringify(result));
  const text = s.slice(0, 4000);
  const status = /\bOn branch ([^\n\r]+)/.exec(text);
  if (status) return status[1].trim();
  const marker = /---BRANCH---\s*[\r\n]+([^\r\n]+)/.exec(text);
  if (marker && marker[1].trim() && !marker[1].includes('---')) return marker[1].trim();
  if (/\bgit\s+branch\s+--show-current\b/.test(cmd)) {
    const line = text.split(/\r?\n/).map(x => x.trim())
      .find(x => x && !x.startsWith('---') && !x.startsWith('/') && !/\s/.test(x));
    if (line) return line;
  }
  const sw = /\bgit\s+(?:switch|checkout)\s+(?:-c\s+)?([^\s;&|]+)/.exec(cmd);
  if (sw && !sw[1].startsWith('-')) return sw[1];
  return null;
}

// Pull the subject line from a git commit. Handles the common heredoc form
// `-m "$(cat <<'EOF'\n<subject>\n..."` AND plain `-m "subject"`.
function commitMsg(cmd) {
  // heredoc: subject is the first non-empty line after the <<MARKER line
  const here = /<<['"]?\w+['"]?\s*\n([\s\S]*?)(?:\nEOF|\nMSG|$)/.exec(cmd);
  if (here) {
    const first = here[1].split('\n').map(s => s.trim()).find(Boolean);
    if (first) return first.slice(0, 100);
  }
  // plain -m, but reject the heredoc wrapper that starts with $(
  const dq = /-m\s+"([^"]+)"/.exec(cmd) || /-m\s+'([^']+)'/.exec(cmd);
  if (dq && !dq[1].includes('$(')) return dq[1].split('\n')[0].slice(0, 100);
  return null;
}

function extractReason(cmd) {
  const echo = /echo\s+["']?(?:reason:\s*)?([^"'\n]{4,120})["']?/i.exec(cmd);
  if (echo) return echo[1].trim();
  const comment = /#\s*(.{4,120})$/.exec(cmd);
  if (comment) return comment[1].trim();
  return undefined;
}

function ok(result) {
  if (result == null) return true;
  const s = typeof result === 'string' ? result : JSON.stringify(result);
  if (/\b(error|failed|exception|traceback|fatal|cannot|no such file)\b/i.test(s.slice(0, 400))) return false;
  return true;
}

// Patterns that look like user text but are system-injected noise, not real intent.
const INTENT_NOISE = [
  /^<local-command/, /^<command-/, /^<system-remind/, /^<command-name/,
  /^<environment_context>/, /^<permissions instructions>/, /^<apps_instructions>/,
  /^<skills_instructions>/, /^<plugins_instructions>/,
  /^\[Context compacted/i,
  /^This session is being continued/i,
  /^Base directory for this skill/i,
  /^\[Image:/, /^\[Request interrupted/, /^Caveat:/,
  /^Your task is to create a detailed summary/i,
  /^<task-notification/,   // background task completion ping, not user intent
  /^belt-ping\b/i,         // scheduled keep-alive ping, not user intent
  /^(ping|heartbeat|keepalive)\b/i,
  /^You are an expert code reviewer\. Follow these steps/i, // slash-cmd template echo
  /^The user wants to (ban|persist)/i,  // banthis skill template injection
  /^Persist it into the project/i,
  /^Caveat: The messages below were generated/i,
  /^#\s*\//,                       // slash-command help header ("# /gate-and-ship ...")
  /^#+\s*Skill being applied/i,    // skill-spec injection
  /^#+\s*Prior conversation/i,     // prior-conversation dump
  /^#+\s*Skill spec/i,
  /^Parse the input/i,             // /loop command template body
];

// Returns trimmed intent text, or null if empty/too-short/noise.
function cleanIntent(s) {
  if (!s) return null;
  s = s.trim();
  if (s.length <= 8) return null;
  if (INTENT_NOISE.some(re => re.test(s))) return null;
  return s;
}

// Extract the phase-signalling gist of an assistant text block. Agent prose is
// the richest subgoal signal ("Phase 1", "big realization", "that failed, trying X")
// but verbose — keep only the first sentence, and flag if it carries a phase cue.
const PHASE_CUE = /\b(let me|now I|next,? I|first,? I|Phase \d|realization|turns out|the (?:gap|issue|bug|problem) is|that (?:failed|didn't work)|instead|switching to|starting|done|fixed|works now)\b/i;
function gistText(text) {
  if (!text) return null;
  const s = text.trim();
  if (!s || s.length < 12) return null;
  // strip markdown headers/code fences noise; take first sentence-ish chunk
  const firstLine = s.split('\n').find(l => l.trim() && !l.trim().startsWith('```') && !l.trim().startsWith('#')) || s;
  const sentence = (firstLine.match(/^.{0,200}?[.!?](?:\s|$)/) || [firstLine.slice(0, 160)])[0].trim();
  return { text: sentence, cue: PHASE_CUE.test(sentence) };
}

// Branch markers — explicit lexical cues that a prompt steers the work.
// Covers the easy ~16% (the marked ones); silent branches are caught structurally
// (interruption events + file-cosine resume edges), NOT here. No model: text-only
// classification inherits the deictic blindness already proven on this data.
const BRANCH = {
  steer: /\b(anyway|instead|forget (it|that|about)|new thing|next thing|lets? switch|before that|hold on|scratch that|different (thing|approach)|stop,? (lets|now))\b/i,
  resume: /\b(back to|resume|return to|carry on|as (i )?said|continue (with|the|on)|now back|go back to)\b/i,
  defer: /\b(later|ill show you|not now|park (it|that)|leave (it|that) (for|as)|come back to|deal with .* after)\b/i,
};
function branchMarker(text) {
  if (!text) return null;
  for (const [kind, rx] of Object.entries(BRANCH)) if (rx.test(text)) return kind;
  return null;
}

module.exports = { classify, extractReason, extractBranch, ok, cleanIntent, commitMsg, gistText, branchMarker };
