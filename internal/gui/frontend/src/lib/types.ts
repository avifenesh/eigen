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
  updated: number;
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
  profile?: string;
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

export type RolloutDTO = { index: number; text: string; outcome?: string };
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
  backgroundDone: number;
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
