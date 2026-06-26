// Single stable import point for the generated Wails bindings. Views/stores
// import from here, never the deep generated path, so a regen or path change
// touches one file. The generated bindings are untyped JS; we layer typed DTO
// shapes on top via $lib/types.
import * as B from "$bindings/github.com/avifenesh/eigen/internal/gui/bridge";
import type {
  SessionInfoDTO,
  SessionStateDTO,
  CompactResultDTO,
  ImageDTO,
  MemoryDTO,
  MemoryScopeDTO,
  MemoryScopeRefDTO,
  SkillsDTO,
  AgentsDTO,
  DreamingDTO,
  DreamingScopeDTO,
  DreamReportDTO,
  RoutingDTO,
  ObserveSummaryDTO,
  CronsDTO,
  PluginsDTO,
  MarketplaceDTO,
  PluginPreviewDTO,
  InstalledPluginDTO,
  ConfigDTO,
  RuleChainsDTO,
  ConnectorsDTO,
  MCPServersDTO,
  MCPServerDTO,
  GoogleStatusDTO,
  DashboardDTO,
  BoardDTO,
  KanbanDTO,
  DaemonStats,
  FeedDTO,
  FeedItemDTO,
  MachinesDTO,
  WorkflowInfoDTO,
  WorkflowResultDTO,
  CommandInfoDTO,
  VoiceStatusDTO,
  RecentDirDTO,
  WorkingDiffDTO,
  FileTreeDTO,
} from "$lib/types";

export const Bridge = {
  // health
  Ping: (): Promise<void> => B.Ping(),
  Stats: (): Promise<DaemonStats | null> => B.Stats(),
  // version of THIS gui binary (compared against daemon version for mismatch)
  GUIVersion: (): Promise<string> => B.GUIVersion(),
  // sessions
  Sessions: (): Promise<SessionInfoDTO[]> => B.Sessions(),
  NewSession: (dir: string, model: string, perm: string): Promise<string> =>
    B.NewSession(dir, model, perm),
  RemoveSession: (id: string): Promise<void> => B.RemoveSession(id),
  PruneSessions: (): Promise<string[]> => B.PruneSessions(),
  State: (id: string): Promise<SessionStateDTO | null> => B.State(id),
  // turn I/O
  SendInput: (id: string, text: string, images: ImageDTO[], allowTools: string[]): Promise<void> =>
    B.SendInput(id, text, images, allowTools),
  SteerInput: (id: string, text: string, images: ImageDTO[]): Promise<boolean> =>
    B.SteerInput(id, text, images),
  Interrupt: (id: string): Promise<void> => B.Interrupt(id),
  Resend: (id: string): Promise<void> => B.Resend(id),
  Approve: (id: string, approvalID: string, allow: boolean): Promise<void> =>
    B.Approve(id, approvalID, allow),
  // maintenance
  Compact: (id: string, target: number): Promise<CompactResultDTO> => B.Compact(id, target),
  Clear: (id: string): Promise<void> => B.Clear(id),
  // settings (return fresh state)
  SetModel: (id: string, model: string): Promise<SessionStateDTO | null> => B.SetModel(id, model),
  SetPerm: (id: string, perm: string): Promise<SessionStateDTO | null> => B.SetPerm(id, perm),
  SetGoal: (id: string, goal: string): Promise<SessionStateDTO | null> => B.SetGoal(id, goal),
  SetTitle: (id: string, title: string): Promise<SessionStateDTO | null> => B.SetTitle(id, title),
  SetEffort: (id: string, level: string): Promise<SessionStateDTO | null> => B.SetEffort(id, level),
  SetSearch: (id: string, mode: string): Promise<SessionStateDTO | null> => B.SetSearch(id, mode),
  SetFast: (id: string, on: boolean): Promise<SessionStateDTO | null> => B.SetFast(id, on),
  // streaming
  Subscribe: (id: string): Promise<void> => B.Subscribe(id),
  Unsubscribe: (id: string): Promise<void> => B.Unsubscribe(id),
  // sandbox
  AddDir: (id: string, path: string): Promise<string> => B.AddDir(id, path),
  KillShell: (id: string, shellID: string): Promise<boolean> => B.KillShell(id, shellID),
  DetachBash: (id: string): Promise<boolean> => B.DetachBash(id),
  // memory
  Memory: (): Promise<MemoryDTO | null> => B.Memory(),
  AppendMemory: (scope: string, note: string): Promise<void> => B.AppendMemory(scope, note),
  WriteUserProfile: (content: string): Promise<void> => B.WriteUserProfile(content),
  MemoryBackups: (scope: string): Promise<string[]> => B.MemoryBackups(scope),
  // bans (banthis hard-prohibition layer) — returns whether an existing ban was replaced/removed
  AddBan: (scope: string, title: string, rule: string): Promise<boolean> => B.AddBan(scope, title, rule),
  RemoveBan: (scope: string, title: string): Promise<boolean> => B.RemoveBan(scope, title),
  // skills
  Skills: (): Promise<SkillsDTO | null> => B.Skills(),
  SkillBody: (name: string): Promise<string> => B.SkillBody(name),
  AcceptSkill: (name: string): Promise<string> => B.AcceptSkill(name),
  RejectSkill: (name: string): Promise<void> => B.RejectSkill(name),
  // skill install (local path or owner/repo GitHub ref) — scanned before write
  InstallSkillFromPath: (path: string): Promise<{ name: string; path: string } | null> => B.InstallSkillFromPath(path),
  InstallSkillFromGitHub: (ownerRepo: string): Promise<{ name: string; path: string } | null> => B.InstallSkillFromGitHub(ownerRepo),
  // agents
  Agents: (): Promise<AgentsDTO | null> => B.Agents(),
  CancelAgent: (id: string): Promise<void> => B.CancelAgent(id),
  AgentTranscript: (id: string): Promise<string> => B.AgentTranscript(id),
  // per-project memory: enumerate selectable scopes + open any one
  ListMemoryScopes: (): Promise<MemoryScopeRefDTO[]> => B.ListMemoryScopes(),
  MemoryForScope: (scope: string): Promise<MemoryScopeDTO | null> => B.MemoryForScope(scope),
  // dreaming
  Dreaming: (): Promise<DreamingDTO | null> => B.Dreaming(),
  DreamingForScope: (scope: string): Promise<DreamingScopeDTO | null> => B.DreamingForScope(scope),
  ConsolidationContent: (path: string): Promise<string> => B.ConsolidationContent(path),
  CurrentMemory: (scope: string): Promise<string> => B.CurrentMemory(scope),
  // run consolidation + summary on demand for a scope ("project" | "global")
  DreamNow: (scope: string): Promise<DreamReportDTO | null> => B.DreamNow(scope),
  // routing
  Routing: (): Promise<RoutingDTO | null> => B.Routing(),
  // observe (historical log summary; live KPIs come from the daemon stats stream)
  ObserveSummary: (limit: number): Promise<ObserveSummaryDTO | null> => B.ObserveSummary(limit),
  // crons
  Crons: (): Promise<CronsDTO | null> => B.Crons(),
  SetTimer: (unit: string, verb: string): Promise<void> => B.SetTimer(unit, verb),
  AddCrontab: (spec: string, command: string): Promise<void> => B.AddCrontab(spec, command),
  RemoveCrontab: (spec: string, command: string): Promise<void> => B.RemoveCrontab(spec, command),
  // plugins
  Plugins: (): Promise<PluginsDTO | null> => B.Plugins(),
  SetMarketEnabled: (name: string, enabled: boolean): Promise<boolean> => B.SetMarketEnabled(name, enabled),
  RemoveMarketplace: (name: string): Promise<boolean> => B.RemoveMarketplace(name),
  RemovePlugin: (name: string): Promise<boolean> => B.RemovePlugin(name),
  // add-a-plugin (mirrors skill-add): record a catalog, list its plugins, install
  // one (scanned before write; rejects on RISKY or no scanner). All three are slow
  // (network fetch / LLM scan) — call with a spinner.
  AddMarketplace: (source: string): Promise<MarketplaceDTO | null> => B.AddMarketplace(source),
  MarketplacePlugins: (mktName: string): Promise<PluginPreviewDTO[]> => B.MarketplacePlugins(mktName),
  InstallPlugin: (pluginName: string, mktName: string): Promise<InstalledPluginDTO | null> =>
    B.InstallPlugin(pluginName, mktName),
  // authored workflows + custom slash commands (run on the active session)
  Workflows: (): Promise<WorkflowInfoDTO[]> => B.Workflows(),
  RunWorkflow: (sessionID: string, name: string, vars: Record<string, string>): Promise<WorkflowResultDTO | null> =>
    B.RunWorkflow(sessionID, name, vars),
  Commands: (): Promise<CommandInfoDTO[]> => B.Commands(),
  RunCommand: (sessionID: string, name: string, args: string): Promise<string> =>
    B.RunCommand(sessionID, name, args),
  // config
  Config: (): Promise<ConfigDTO | null> => B.Config(),
  SetConfig: (key: string, value: string): Promise<string> => B.SetConfig(key, value),
  // per-role model fallback chains (the per-rule chain editor)
  RuleChains: (): Promise<RuleChainsDTO | null> => B.RuleChains(),
  SetRuleChain: (role: string, chain: string[]): Promise<string[]> => B.SetRuleChain(role, chain),
  // connectors — remote MCP servers authorized over OAuth
  Connectors: (): Promise<ConnectorsDTO | null> => B.Connectors(),
  AddConnector: (name: string, url: string, description: string): Promise<void> =>
    B.AddConnector(name, url, description),
  AddCatalogConnector: (name: string): Promise<void> => B.AddCatalogConnector(name),
  ConnectConnector: (name: string): Promise<void> => B.ConnectConnector(name),
  DisconnectConnector: (name: string): Promise<void> => B.DisconnectConnector(name),
  RemoveConnector: (name: string): Promise<boolean> => B.RemoveConnector(name),
  SetConnectorDisabled: (name: string, disabled: boolean): Promise<boolean> =>
    B.SetConnectorDisabled(name, disabled),
  // MCP server wiring editor (stdio + remote)
  MCPServers: (): Promise<MCPServersDTO | null> => B.MCPServers(),
  // The generated binding model marks optional fields required; our $lib/types
  // shape is the contract callers use, so cast at the boundary.
  SaveMCPServer: (d: MCPServerDTO): Promise<void> => B.SaveMCPServer(d as never),
  RemoveMCPServer: (name: string): Promise<boolean> => B.RemoveMCPServer(name),
  SetMCPServerDisabled: (name: string, disabled: boolean): Promise<boolean> =>
    B.SetMCPServerDisabled(name, disabled),
  MCPSecretsAvailable: (): Promise<boolean> => B.MCPSecretsAvailable(),
  // working-station command center (calendar + mail + machine health)
  Dashboard: (): Promise<DashboardDTO | null> => B.Dashboard(),
  // cross-project work board
  Board: (): Promise<BoardDTO | null> => B.Board(),
  Kanban: (): Promise<KanbanDTO | null> => B.Kanban(),
  PinLane: (key: string): Promise<void> => B.PinLane(key),
  UnpinLane: (key: string): Promise<void> => B.UnpinLane(key),
  ReviewPR: (url: string): Promise<string> => B.ReviewPR(url),
  WorkIssue: (url: string): Promise<string> => B.WorkIssue(url),
  // native Google integration (Calendar + Gmail)
  GoogleStatus: (): Promise<GoogleStatusDTO | null> => B.GoogleStatus(),
  ConnectGoogle: (): Promise<void> => B.ConnectGoogle(),
  DisconnectGoogle: (): Promise<void> => B.DisconnectGoogle(),
  ImportGoogleClient: (): Promise<boolean> => B.ImportGoogleClient(),
  // proactive feed
  Feed: (): Promise<FeedDTO | null> => B.Feed(),
  // per-project feed accessor — reserved for a future project drill-in view (not dead code)
  FeedFor: (dir: string): Promise<FeedItemDTO[]> => B.FeedFor(dir),
  StartFromFeed: (dir: string, task: string): Promise<string> => B.StartFromFeed(dir, task),
  StartWorkingFromFeed: (dir: string, task: string): Promise<string> => B.StartWorkingFromFeed(dir, task),
  DismissFeed: (key: string): Promise<void> => B.DismissFeed(key),
  RescanFeed: (): Promise<void> => B.RescanFeed(),
  // machines / remote
  Machines: (): Promise<MachinesDTO | null> => B.Machines(),
  RemoteSessions: (target: string): Promise<SessionInfoDTO[]> => B.RemoteSessions(target),
  // sessions
  ExportSession: (id: string): Promise<string> => B.ExportSession(id),
  // new-chat working-directory picker: recent dirs + native OS folder dialog
  RecentDirs: (): Promise<RecentDirDTO[]> => B.RecentDirs(),
  PickDirectory: (): Promise<string> => B.PickDirectory(), // "" when cancelled
  // right-panel TOOLS ───────────────────────────────────────────────────────
  // working-tree diff of the current changes vs HEAD
  WorkingDiff: (dir: string): Promise<WorkingDiffDTO | null> => B.WorkingDiff(dir),
  // file-explorer tree + click-to-view file content
  FileTree: (dir: string): Promise<FileTreeDTO | null> => B.FileTree(dir),
  ReadFileForView: (path: string): Promise<string> => B.ReadFileForView(path),
  // PTY terminal — output streams on the "eigen:terminal" event
  TerminalStart: (cols: number, rows: number): Promise<string> => B.TerminalStart(cols, rows),
  TerminalWrite: (id: string, data: string): Promise<void> => B.TerminalWrite(id, data),
  TerminalResize: (id: string, cols: number, rows: number): Promise<void> => B.TerminalResize(id, cols, rows),
  TerminalKill: (id: string): Promise<void> => B.TerminalKill(id),
  // voice — server-side STT/TTS (the GUI runs on the host, so the same stack the
  // TUI uses, not webview getUserMedia). State streams on the "eigen:voice" event.
  VoiceStatus: (): Promise<VoiceStatusDTO | null> => B.VoiceStatus(),
  VoiceListen: (): Promise<string> => B.VoiceListen(), // record one utterance → transcript
  VoiceCancelListen: (): Promise<void> => B.VoiceCancelListen(),
  VoiceSpeak: (text: string): Promise<void> => B.VoiceSpeak(text), // read aloud, cancelable
  VoiceStopSpeak: (): Promise<void> => B.VoiceStopSpeak(),
  VoiceModeStart: (sessionID: string): Promise<void> => B.VoiceModeStart(sessionID), // hands-free loop
  VoiceModeStop: (): Promise<void> => B.VoiceModeStop(),
};
