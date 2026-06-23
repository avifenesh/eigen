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
  SkillsDTO,
  AgentsDTO,
  DreamingDTO,
  RoutingDTO,
  ObserveSummaryDTO,
  CronsDTO,
  PluginsDTO,
  ConfigDTO,
  FeedDTO,
  FeedItemDTO,
  MachinesDTO,
} from "$lib/types";

export const Bridge = {
  // health
  Ping: (): Promise<void> => B.Ping(),
  Stats: (): Promise<unknown> => B.Stats(),
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
  // skills
  Skills: (): Promise<SkillsDTO | null> => B.Skills(),
  SkillBody: (name: string): Promise<string> => B.SkillBody(name),
  AcceptSkill: (name: string): Promise<string> => B.AcceptSkill(name),
  RejectSkill: (name: string): Promise<void> => B.RejectSkill(name),
  // agents
  Agents: (): Promise<AgentsDTO | null> => B.Agents(),
  CancelAgent: (id: string): Promise<void> => B.CancelAgent(id),
  AgentTranscript: (id: string): Promise<string> => B.AgentTranscript(id),
  // dreaming
  Dreaming: (): Promise<DreamingDTO | null> => B.Dreaming(),
  ConsolidationContent: (path: string): Promise<string> => B.ConsolidationContent(path),
  CurrentMemory: (scope: string): Promise<string> => B.CurrentMemory(scope),
  // routing
  Routing: (): Promise<RoutingDTO | null> => B.Routing(),
  // observe (historical log summary; live KPIs come from the daemon stats stream)
  ObserveSummary: (limit: number): Promise<ObserveSummaryDTO | null> => B.ObserveSummary(limit),
  // crons
  Crons: (): Promise<CronsDTO | null> => B.Crons(),
  SetTimer: (unit: string, verb: string): Promise<void> => B.SetTimer(unit, verb),
  // plugins
  Plugins: (): Promise<PluginsDTO | null> => B.Plugins(),
  SetMarketEnabled: (name: string, enabled: boolean): Promise<boolean> => B.SetMarketEnabled(name, enabled),
  RemoveMarketplace: (name: string): Promise<boolean> => B.RemoveMarketplace(name),
  RemovePlugin: (name: string): Promise<boolean> => B.RemovePlugin(name),
  // config
  Config: (): Promise<ConfigDTO | null> => B.Config(),
  SetConfig: (key: string, value: string): Promise<string> => B.SetConfig(key, value),
  // proactive feed
  Feed: (): Promise<FeedDTO | null> => B.Feed(),
  FeedFor: (dir: string): Promise<FeedItemDTO[]> => B.FeedFor(dir),
  StartFromFeed: (dir: string, task: string): Promise<string> => B.StartFromFeed(dir, task),
  DismissFeed: (key: string): Promise<void> => B.DismissFeed(key),
  // machines / remote
  Machines: (): Promise<MachinesDTO | null> => B.Machines(),
  RemoteSessions: (target: string): Promise<SessionInfoDTO[]> => B.RemoteSessions(target),
  // sessions
  ExportSession: (id: string): Promise<string> => B.ExportSession(id),
};
