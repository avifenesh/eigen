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

export type StreamEventDTO = { event: WireEventDTO; replay: boolean };

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
};
export type PluginsDTO = { plugins: InstalledPluginDTO[]; marketplaces: MarketplaceDTO[] };

export type ConfigFieldDTO = {
  key: string;
  desc: string;
  value: string;
  options?: string[];
  multi?: boolean;
};
export type ConfigDTO = { fields: ConfigFieldDTO[]; path: string };

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
