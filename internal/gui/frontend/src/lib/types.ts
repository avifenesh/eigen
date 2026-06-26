// Hand-authored TS mirrors of the Go DTOs (gui/dto.go). The generated bindings
// are untyped JS, so these give the frontend real types at the seam. Keep in
// sync with dto.go field names + json tags.

export type ImageDTO = { mediaType: string; data: string };

export type ToolCallDTO = { id: string; name: string; args: string };

export type MessageDTO = {
  role: string;
  text: string;
  reasoning?: string;
  toolCalls?: ToolCallDTO[];
  toolCallId?: string;
  toolName?: string;
  toolError?: boolean;
  images?: ImageDTO[];
};

export type WireEventDTO = {
  kind: string;
  step?: number;
  text?: string;
  tool?: string;
  toolId?: string;
  toolArgs?: string;
  result?: string;
  isError?: boolean;
  inTokens?: number;
  outTokens?: number;
  provider?: string;
  model?: string;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
};

export type StreamEventDTO = { event: WireEventDTO; replay: boolean; seq?: number };

export type SessionInfoDTO = {
  id: string;
  title: string;
  dir: string;
  model: string;
  status: string;
  turns: number;
  views: number;
  updated: number; // unix nano — divide by 1e6 for ms (use relTime() in lib/status.ts)
};

export type ToolInfo = { name: string; read_only: boolean };
export type ShellInfo = {
  id: string;
  command: string;
  status: string;
  exit_code: number;
  last_line?: string;
};
export type ApprovalInfo = { id: string; tool: string; args: string };

export type SessionStateDTO = {
  messages: MessageDTO[];
  tokens: number;
  title?: string;
  model: string;
  provider: string;
  maxTokens: number;
  perm: string;
  goal: string;
  effort?: string;
  search?: string;
  fast?: boolean;
  fastOk?: boolean;
  running?: boolean;
  tools?: ToolInfo[];
  roots?: string[];
  shells?: ShellInfo[];
  pending?: ApprovalInfo[];
};

export type CompactResultDTO = { before: number; after: number };

// Parity contract (GUI-096): DaemonStats is the one shape the bridge emits RAW —
// from Stats() and on the eigen:daemon:stats stream — as the Go *daemon.DaemonStats
// with its native snake_case tags, bypassing the camelCase DTO layer every other
// type uses. So these keys must mirror internal/daemon/protocol.go DaemonStats 1:1
// (esp. the identity fields version/executable/binary_sha256/vcs_revision/
// vcs_modified). There is no mapper to catch drift: a daemon field rename silently
// desyncs this block — keep them in lockstep.
export type DaemonStats = {
  uptime_sec: number;
  goroutines: number;
  heap_alloc_b: number;
  heap_sys_b: number;
  rss_b: number;
  num_gc: number;
  sessions: number;
  views: number;
  running_turns: number;
  bg_tasks: number;
  go_version?: string;
  version?: string;
  executable?: string;
  binary_sha256?: string;
  vcs_revision?: string;
  vcs_modified?: boolean;
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
};

export type HealthDTO = { ok: boolean; error?: string };

export type MemoryNoteDTO = { index: number; text: string };
export type MemoryScopeDTO = {
  scope: string;
  dir: string;
  path: string;
  summary: string;
  hasSummary: boolean;
  notes: MemoryNoteDTO[];
  noteCount: number;
  bans: string;
  profile?: string; // USER.md — the user-editable section
  profileLearned?: string; // USER.md — the eigen-auto-maintained block (read-only)
  adHoc: MemoryNoteDTO[];
  backups: number;
  bytes: number;
};
export type MemoryDTO = {
  project: MemoryScopeDTO | null;
  global: MemoryScopeDTO | null;
};
// A selectable memory scope in the picker (lightweight — no note bodies). `key`
// round-trips to MemoryForScope(scope): an abs project dir when known, else the
// on-disk store key, or "global". See gui/memory.go ListMemoryScopes.
export type MemoryScopeRefDTO = {
  key: string;
  name: string;
  dir: string;
  noteCount: number;
  current?: boolean; // the cwd project — the picker's default selection
};

export type SkillDTO = {
  name: string;
  description: string;
  path: string;
  source: string; // "user" | "project" | "extra"
};
export type SkillProposalDTO = { name: string; description: string; path: string };
export type SkillsDTO = { skills: SkillDTO[]; proposals: SkillProposalDTO[] };

export type BgTaskDTO = {
  id: string;
  task: string;
  where?: string;
  kind?: string;
  difficulty?: string;
  model?: string;
  role?: string;
  attempts?: number;
  escalated?: boolean;
  status: string; // running | done | error | canceled | lost
  result?: string;
  error?: string;
  startedMs: number;
  finishedMs?: number;
  pid?: number;
  host?: string;
  steps?: number;
  lastTool?: string;
  lastNote?: string;
  inTokens?: number;
  outTokens?: number;
  updatedMs?: number;
  canceling?: boolean;
};
export type AgentsDTO = {
  tasks: BgTaskDTO[];
  running: number;
  done: number;
  errored: number;
  dir: string;
};

export type RolloutDTO = { index: number; text: string; outcome?: string; whenMs?: number };
export type ConsolidationDTO = { path: string; label: string; whenMs: number; bytes: number };
export type DreamingScopeDTO = {
  scope: string;
  rollouts: RolloutDTO[];
  consolidations: ConsolidationDTO[];
  currentBytes: number;
};
export type DreamingDTO = {
  project: DreamingScopeDTO | null;
  global: DreamingScopeDTO | null;
};

// Result of an on-demand DreamNow(scope): what consolidation did this run.
export type DreamReportDTO = {
  scope: string;
  report: string;
  consolidated: boolean;
  summaryRegened: boolean;
  changed: boolean;
};

// A recent project dir for the new-chat working-directory picker (RecentDirs).
export type RecentDirDTO = { dir: string; name: string };

// Right-panel TOOLS DTOs ───────────────────────────────────────────────────
// Working-tree diff of the current changes vs HEAD (WorkingDiff(dir)).
export type DiffFileDTO = { path: string; adds: number; dels: number };
export type WorkingDiffDTO = {
  dir: string;
  branch: string;
  patch: string; // full unified diff (capped; truncated flagged)
  files: DiffFileDTO[];
  isRepo: boolean;
  clean: boolean;
  truncated: boolean;
};
// File-explorer tree (FileTree(dir)); Path is absolute → feeds ReadFileForView.
export type FileEntryDTO = {
  name: string;
  path: string;
  isDir: boolean;
  children?: FileEntryDTO[];
};
export type FileTreeDTO = { dir: string; entries: FileEntryDTO[]; truncated: boolean };
// Pushed on the "eigen:terminal" event as the PTY produces output / exits.
export type TerminalEventDTO = {
  id: string;
  data?: string; // base64 of raw PTY output bytes
  exited?: boolean; // the shell ended / was killed
};

// Which voice capabilities are usable in this environment (capability-gates the
// composer mic + read-aloud affordances). Server-side detection — see voice.go.
export type VoiceStatusDTO = {
  stt: boolean; // a recorder + whisper + model are present
  tts: boolean; // a TTS command (Kokoro/espeak/…) is present
};

// Pushed on the "eigen:voice" event as the mic/speaker changes state. Phase:
// idle | listening | transcribing | thinking | speaking | error | off.
export type VoiceEventDTO = {
  phase: string;
  text?: string; // transcript (listen done) or error message
  mode?: boolean; // true while hands-free voice MODE is active
};

export type ModelDTO = {
  id: string;
  provider: string;
  contextWindow: number;
  cache: boolean;
  context1m: boolean;
  reasoning: boolean;
  effort?: string;
  effortLevels?: string[];
  thinkingBudget?: number;
  search?: boolean;
  vision?: boolean;
  social?: boolean;
  available: boolean;
};
export type ProviderDTO = { name: string; credentialed: boolean; modelCount: number };
export type RoutingDTO = { models: ModelDTO[]; providers: ProviderDTO[] };

export type ToolStatDTO = { name: string; calls: number; errors: number; durationMs: number };
export type ModelStatDTO = {
  name: string;
  turns: number;
  inTokens: number;
  outTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  durationMs: number;
};
export type HookStatDTO = { name: string; starts: number; done: number; errors: number; durationMs: number };
export type CountDTO = { name: string; count: number };
export type RouteStatsDTO = {
  routed: number;
  skipped: number;
  assessed: number;
  orchestrator: number;
  byModel: CountDTO[];
  byKind: CountDTO[];
  byDifficulty: CountDTO[];
  skipReasons: CountDTO[];
};
export type SubagentStatsDTO = {
  taskCalls: number;
  taskErrors: number;
  groupCalls: number;
  groupErrors: number;
  mutatingCalls: number;
  mutatingErrors: number;
  statusChecks: number;
  promotes: number;
  promoteErrors: number;
  backgroundDone: number;
  backgroundNotes: number;
  routeNotes: number;
};
export type CronDTO = {
  name: string;
  kind: string; // "timer" | "crontab"
  next: string;
  last: string;
  active: boolean; // unit is loaded/running now (start/stop)
  enabled: boolean; // unit is enabled persistently (enable/disable)
  command: string;
  unit?: string;
};
export type CronsDTO = {
  crons: CronDTO[];
  timers: number;
  crontab: number;
  systemdAvail: boolean;
};

export type ScanFindingDTO = { component: string; reasons?: string[] };
export type InstalledPluginDTO = {
  name: string;
  marketplace?: string;
  version?: string;
  description?: string;
  installedMs: number;
  enabled: boolean;
  skills?: string[];
  agents?: string[];
  mcpServers?: string[];
  commands?: string[];
  hooks?: number;
  scanStatus?: string;
  scanCount?: number;
  scans?: ScanFindingDTO[];
  warnings?: string[];
};
export type MarketplaceDTO = {
  name: string;
  source: string;
  owner?: string;
  disabled: boolean;
  addedMs: number;
  // populated only by AddMarketplace (from the parsed catalog); Plugins() omits them
  description?: string;
  version?: string;
  pluginCount?: number;
};
export type PluginsDTO = { plugins: InstalledPluginDTO[]; marketplaces: MarketplaceDTO[] };
// One installable plugin from a recorded marketplace (read-only preview; counts
// always present). See gui/plugins.go MarketplacePlugins.
export type PluginPreviewDTO = {
  name: string;
  description?: string;
  marketplace: string;
  version?: string;
  skills: number;
  agents: number;
  commands: number;
  mcpServers: number;
  hooks: number;
};

export type ConfigFieldDTO = {
  key: string;
  desc: string;
  value: string;
  options?: string[];
  multi?: boolean;
};
export type ConfigDTO = { fields: ConfigFieldDTO[]; path: string };

// Per-role model fallback chain (the per-rule chain editor). Each role's chain
// is an ordered list of model names tried in turn, each falling through to the
// next on a quota/billing failure until one answers.
export type RuleChainDTO = {
  role: string; // "primary" | "explore" | "research" | "general" | "code" | "dreamer" | "judge"
  desc: string;
  chain: string[];
  custom: boolean; // true = user-configured; false = built-in default
};
export type RuleChainsDTO = {
  roles: RuleChainDTO[];
  models: string[]; // model names the picker offers (shorthands + catalog ids)
};

// Connectors: remote MCP servers authorized over OAuth (Google Workspace, Slack,
// Notion, …). Mirrors internal/gui/connectors.go.
export type ConnectorDTO = {
  name: string;
  display: string;
  glyph: string;
  url: string;
  type: string;
  description: string;
  disabled: boolean;
  connected: boolean;
  requiresAuth: boolean;
  expiry?: string; // RFC3339 token expiry
};
// One curated directory connector (the browse-and-connect grid).
export type CatalogEntryDTO = {
  name: string;
  display: string;
  glyph: string;
  url: string;
  description: string;
  category: string;
  added: boolean; // already configured in mcp.json
};
export type ConnectorsDTO = { connectors: ConnectorDTO[]; directory: CatalogEntryDTO[] };
// Emitted on "eigen:connector" when a background OAuth flow finishes.
export type ConnectorEventDTO = { name: string; ok: boolean; error?: string };

// MCP server wiring editor (stdio + remote). Mirrors internal/gui/wiring.go.
export type MCPServerDTO = {
  name: string;
  command?: string[];
  url?: string;
  type?: string;
  description?: string;
  tools?: string[];
  excludeTools?: string[];
  disabled: boolean;
  remote: boolean;
  envPairs?: string[]; // KEY=VALUE lines
  secretEnvKeys?: string[]; // env vars stored in the OS keychain (names only)
  secretEnvPairs?: string[]; // write-only: KEY=VALUE secrets → keychain on save
};
export type MCPServersDTO = { servers: MCPServerDTO[] };

// Native Google integration (Calendar + Gmail) status. Mirrors internal/gui/google.go.
export type GoogleStatusDTO = {
  configured: boolean; // a BYO Google Cloud client is present
  connected: boolean; // an account is linked
  setupHint: string; // how to add a client when not configured
  setupUrl: string; // Google Cloud Console credentials page
  clientPath: string; // where the imported client JSON lands
};

// Working-station command-center snapshot. Mirrors internal/gui/dashboard.go.
export type CalEventDTO = { summary: string; start: string; allDay: boolean; location: string };
export type MailMsgDTO = { from: string; subject: string };
export type SysHealthDTO = {
  loadPerCpu: number;
  cpus: number;
  memUsedPct: number;
  memUsedGb: number;
  memTotalGb: number;
  diskUsedPct: number;
  diskUsedGb: number;
  diskTotalGb: number;
  uptimeHours: number;
};
export type DashboardDTO = {
  googleConnected: boolean;
  events: CalEventDTO[];
  unreadCount: number;
  unread: MailMsgDTO[];
  health: SysHealthDTO;
};

export type FeedItemDTO = {
  key: string;
  kind: string; // git | github | memory | suggest
  title: string;
  detail?: string;
  dir?: string;
  dirName?: string;
  task: string;
  url?: string;
};
export type FeedDTO = { items: FeedItemDTO[]; scannedMs: number; fresh: boolean };

export type MachineDTO = {
  name: string;
  ssh: string;
  addr?: string;
  dir?: string;
  model?: string;
  perm?: string;
  saved: boolean;
  detected: boolean;
};
export type MachinesDTO = { machines: MachineDTO[] };

// Authored workflows (~/.eigen/workflows) + custom slash commands
// (~/.eigen/commands) — run on the active session via the bridge.
export type WorkflowInfoDTO = { name: string; description: string; steps: number };
export type WorkflowResultDTO = {
  completed: string[];
  failedAt?: string;
  outputs: Record<string, string | undefined>;
};
export type CommandInfoDTO = {
  name: string;
  description: string;
  argHint?: string;
  model?: string;
  allowedTools?: string[];
};

export type ObserveSummaryDTO = {
  records: number;
  byKind: CountDTO[];
  tools: ToolStatDTO[];
  models: ModelStatDTO[];
  hooks: HookStatDTO[];
  errors: CountDTO[];
  routes: RouteStatsDTO;
  subagents: SubagentStatsDTO;
  available: boolean;
};
