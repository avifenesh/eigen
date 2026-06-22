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
