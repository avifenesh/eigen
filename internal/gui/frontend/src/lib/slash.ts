// Shared GUI slash-command metadata and parsing. The command set mirrors the
// TUI's slash surface (internal/tui/completion.go + commands.go) while the Chat
// view decides which entries are executable in the GUI and which open a view or
// explain a terminal-only feature.
export type SlashCommandEntry = {
  name: string;
  description: string;
  usage?: string;
  argHint?: string;
  source?: "builtin" | "custom";
};

export type ParsedSlashCommand = { name: string; rawName: string; args: string };

export const builtinSlashCommands: SlashCommandEntry[] = [
  { name: "help", usage: "/help", description: "show slash commands and GUI shortcuts" },
  { name: "home", usage: "/home", description: "return to the home dashboard" },
  { name: "background", usage: "/background", description: "leave this chat while the daemon keeps the turn running" },
  { name: "bg", usage: "/bg", description: "alias for /background" },
  { name: "sessions", usage: "/sessions", description: "open the session switcher" },
  { name: "rail", usage: "/rail", description: "toggle the left navigation rail" },
  { name: "changes", usage: "/changes", description: "toggle the right tools dock" },
  { name: "term", usage: "/term", description: "open the terminal tool panel" },
  { name: "tasks", usage: "/tasks", description: "open background tasks" },
  { name: "shells", usage: "/shells", description: "show background shells in the session info dock" },
  { name: "tray", usage: "/tray", description: "open live sessions that need attention" },
  { name: "workflow", usage: "/workflow <name> [k=v …]", description: "run an authored workflow" },
  { name: "resume", usage: "/resume", description: "open the session list (GUI resume surface)" },
  { name: "save", usage: "/save", description: "export this session transcript" },
  { name: "clear", usage: "/clear", description: "clear the conversation" },
  { name: "rename", usage: "/rename <title>", description: "rename this session" },
  { name: "compact", usage: "/compact", description: "compact older context" },
  { name: "model", usage: "/model [id]", description: "show or switch the model" },
  { name: "effort", usage: "/effort [level]", description: "show or set reasoning effort" },
  { name: "search", usage: "/search [off|auto|on]", description: "show or set live search" },
  { name: "fast", usage: "/fast [on|off]", description: "toggle fast/priority service tier" },
  { name: "perm", usage: "/perm [gated|auto]", description: "show or set permission posture" },
  { name: "goal", usage: "/goal <text|clear>", description: "show, set, or clear the persistent goal" },
  { name: "loop", usage: "/loop", description: "explain loop support in the GUI" },
  { name: "config", usage: "/config [key value]", description: "open settings or set a config value" },
  { name: "route", usage: "/route [on|off]", description: "show or set model-assessed routing" },
  { name: "review", usage: "/review [target]", description: "ask eigen to run a cross-vendor review" },
  { name: "voice", usage: "/voice [setup]", description: "toggle hands-free voice mode or show setup" },
  { name: "mute", usage: "/mute", description: "stop hands-free voice mode" },
  { name: "dictate", usage: "/dictate", description: "dictate one message" },
  { name: "talk", usage: "/talk", description: "alias for /dictate" },
  { name: "speak", usage: "/speak", description: "read the last assistant answer aloud once" },
  { name: "skills", usage: "/skills [name]", description: "list skills or preview one" },
  { name: "tools", usage: "/tools", description: "list tools available to this session" },
  { name: "plugins", usage: "/plugins", description: "open plugins" },
  { name: "hooks", usage: "/hooks", description: "open hook/plugin wiring surfaces" },
  { name: "plugin", usage: "/plugin", description: "open plugins" },
  { name: "marketplace", usage: "/marketplace", description: "open plugin marketplaces" },
  { name: "add-dir", usage: "/add-dir [path]", description: "list or grant an additional working directory" },
  { name: "find", usage: "/find <text>", description: "find text in this page with browser search" },
  { name: "copy", usage: "/copy", description: "copy the last assistant answer" },
  { name: "mouse", usage: "/mouse", description: "terminal-only mouse-capture toggle" },
  { name: "ban", usage: "/ban <title>: <rule>", description: "record a hard prohibition in project memory" },
  { name: "unban", usage: "/unban <title>", description: "remove a project-memory ban" },
  { name: "steer", usage: "/steer", description: "make Enter steer running turns" },
  { name: "queue", usage: "/queue", description: "make Enter queue while a turn runs" },
  { name: "export", usage: "/export", description: "export this session transcript" },
  { name: "read", usage: "/read", description: "GUI uses /speak for one-shot read-aloud" },
  { name: "observe", usage: "/observe", description: "open telemetry" },
  { name: "obs", usage: "/obs", description: "alias for /observe" },
  { name: "rebuild", usage: "/rebuild", description: "terminal-only rebuild command" },
  { name: "quit", usage: "/quit", description: "close the window from the desktop shell" },
  { name: "exit", usage: "/exit", description: "alias for /quit" },
].map((c) => ({ source: "builtin", ...c }));

export function slashLabel(c: Pick<SlashCommandEntry, "name">): string {
  return c.name.startsWith("/") ? c.name : `/${c.name}`;
}

export function parseSlashCommand(text: string): ParsedSlashCommand | null {
  const m = text.match(/^\/([A-Za-z0-9][\w-]*)(?:\s+([\s\S]*))?$/);
  if (!m) return null;
  const rawName = m[1];
  return { rawName, name: rawName.toLowerCase(), args: (m[2] ?? "").trim() };
}

export function slashHelpText(commands: SlashCommandEntry[] = builtinSlashCommands): string {
  const lines = commands
    .filter((c) => c.source !== "custom")
    .map((c) => `  ${c.usage ?? slashLabel(c)} — ${c.description}`);
  return [
    "Slash commands (GUI)",
    "Type / to open the command menu. Built-ins run in the GUI; custom commands from .eigen/commands and ~/.eigen/commands run as authored prompts.",
    "",
    ...lines,
  ].join("\n");
}
