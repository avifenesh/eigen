# GUI shared components

The reusable Svelte 5 building blocks of the Eigen desktop GUI (Wails v3 + Svelte 5).
Everything under `internal/gui/frontend/src/lib/components/` is presentation-layer: small,
mostly-stateless `.svelte` components consumed by the route views (`lib/views/*.svelte`) and the
app shell (`App.svelte`). They split into three loose tiers: **primitives** (Badge, Button, Card,
StatusDot, EmptyState — pure visual atoms taking props + a `children` snippet), **overlays /
chrome** (Rail, TopBar, Sheet, Popover, Tooltip, ToastHost, CommandPalette, Shortcuts — app
furniture, focus-trapped dialogs, and global keyboard surfaces), and **content renderers**
(Markdown, CodeBlock, DiffView, ToolCallCard, VirtualList — turn daemon/model output into typeset
text, syntax-tinted code, unified diffs, collapsible tool records, and windowed lists). All styling
flows from CSS custom-property design tokens (`--brand`, `--sp-*`, `--fs-*`, `--dur-*`, etc.) defined
elsewhere in the frontend; every component honors `prefers-reduced-motion`. The only places these
components reach beyond the frontend are the Wails bridge (`$lib/bridge`), the reactive stores
(`$lib/stores/*`), the router (`$lib/router.svelte`), and the `trapFocus` action (`$lib/actions`).

## Files

### internal/gui/frontend/src/lib/components/Badge.svelte
- **Role:** Tone-coded pill label for statuses / model ids / numeric counts.
- **Key symbols:**
  - props `tone` (`neutral|brand|success|warn|error|info`), `truncate` (clamp to 16ch + ellipsis), `children` (Snippet) — a `<span>` pill; numeric content stays tabular (`tnum`).
- **Depends on:** none (svelte `Snippet` type only) — pure tokens-driven CSS.
- **Used by:** 13 views — Home, Chat, Sessions, Live, Agents, Memory, Dreaming, Skills, Plugins, Routing, Machines, Crons, Profile.

### internal/gui/frontend/src/lib/components/Button.svelte
- **Role:** The reference control primitive every other button composes from.
- **Key symbols:**
  - props `variant` (`primary|secondary|ghost|danger|icon|link`), `size` (`sm|md|lg`), `loading`, `disabled`, `type`, `title`, `full`, `icon` (leading Snippet), `onclick`, `children` — width-locked loading spinner, optical baseline trim, ellipsis-truncating label.
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** 14 views (Home, Chat, Sessions, Live, Agents, Memory, Dreaming, Skills, Plugins, Routing, Machines, Crons, Profile, Config) and composed inside `Composer.svelte` and `Sheet.svelte` (the close button).

### internal/gui/frontend/src/lib/components/Card.svelte
- **Role:** Raised hairline-edged container; the app's primary surface.
- **Key symbols:**
  - props `interactive` (role=button + Enter/Space activation), `selected` (brand wash + left rail), `live` (breathing teal top seam + glow for the one currently-relevant item), `title`, `onclick`, `children`.
  - `onkeydown(e)` — unexported; maps Enter/Space to `onclick` when interactive (native-button behavior).
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** 11 views — Agents, Config, Crons, Dreaming, Machines, Memory, Observe, Plugins, Profile, Routing, Skills.

### internal/gui/frontend/src/lib/components/CodeBlock.svelte
- **Role:** Syntax-tinted monospace code surface with a language label + copy button.
- **Key symbols:**
  - props `code`, `lang`.
  - `copy()` — writes to `navigator.clipboard`, flashes a one-shot "copied" state (1.4s, timer cleared `onDestroy`).
  - `esc(s)` — HTML-escapes before injection (no raw code as HTML).
  - `html` (`$derived.by`) — tokenizes on comment/string boundaries, then tints keywords/numbers via regex (`COMMENT`, `STRING`, `KEYWORDS`, `NUMBER`); `tintPlain(seg)` does the keyword/number pass. Cheap regex tint, no highlighter dependency.
- **Depends on:** `svelte` (`onDestroy`).
- **Used by / entrypoint:** directly by `views/Agents.svelte`; transitively the main consumer — rendered by `Markdown.svelte` (fenced code) and `ToolCallCard.svelte` (args/result/error wells).

### internal/gui/frontend/src/lib/components/CommandPalette.svelte
- **Role:** ⌘K/Ctrl+K fuzzy command palette — run a verb, jump to a view, or open a session.
- **Key symbols:**
  - `fuzzy(q, text)` — subsequence scorer rewarding contiguous runs + word-start hits (-1 = no match).
  - `items` / `grouped` (`$derived.by`) — builds Actions + Views (from `routes`) + Sessions (from `sessions.list`), filters/sorts by score with group order as tiebreak, injects section headers.
  - actions: `newSession()` (`Bridge.NewSession`), `pruneEmpty()` (`Bridge.PruneSessions`), `refreshFeed()` (`feed.refresh` + nav home), `go(route)`.
  - `show()`/`hide()`/`run(i)`; `onWinKey` (global ⌘K toggle / Escape) registered/torn-down in `$effect`; `onListKey` (↑/↓/Enter within input).
- **Depends on:** `$lib/router.svelte` (router, routes, Route), `$lib/stores/sessions.svelte`, `$lib/stores/feed.svelte`, `$lib/stores/toasts.svelte`, `$lib/bridge` (Bridge).
- **Used by / entrypoint:** mounted once in `App.svelte`; reached via the global ⌘K/Ctrl+K window listener.

### internal/gui/frontend/src/lib/components/Composer.svelte
- **Role:** Chat message composer — auto-growing textarea, Enter-sends, Stop-while-running.
- **Key symbols:**
  - props `running` (flips Send→Stop), `disabled`, `disabledReason` (surfaced inline + as title when daemon offline), `onsend`, `oninterrupt`.
  - `grow()` — measures `scrollHeight`, clamps to `MAX_ROWS` (8) read from the line box.
  - `send()` — guards `canSend`, emits trimmed text, clears + regrows; `onkeydown` (Enter sends, Shift+Enter newlines, respects IME composing).
- **Depends on:** `./Button.svelte`.
- **Used by:** `views/Chat.svelte`.

### internal/gui/frontend/src/lib/components/DiffView.svelte
- **Role:** Unified-diff renderer — the showpiece detail surface (better than a terminal diff).
- **Key symbols:**
  - props `patch`.
  - `rows` (`$derived.by`) — one-pass classifier into `Kind` (`add|del|ctx|hunk|meta`); `additions`/`deletions`/`hunks`/`empty` counts.
  - large-diff collapse: `LARGE`=400, `PREVIEW`=120, `isLarge`, `expanded`, `visibleRows`, `hiddenCount`.
  - `hunkRange(text)` / `hunkLabel(text)` — split `@@ -a,b +c,d @@` coordinates from the trailing context label.
- **Depends on:** none (self-contained types).
- **Used by / entrypoint:** directly by `views/Dreaming.svelte`; also rendered by `ToolCallCard.svelte` for synthesized mutation diffs.

### internal/gui/frontend/src/lib/components/EmptyState.svelte
- **Role:** Calm zero-data / not-yet-built placeholder with optional single action.
- **Key symbols:**
  - props `glyph`, `title`, `line`, `action` (Snippet).
  - `hasAction` (`$derived`) — when present, the glyph warms to teal with a halo + hairline tether toward the action; reduced-motion-safe mount rise.
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** `App.svelte` + 13 views (Chat, Sessions, Live, Agents, Memory, Dreaming, Skills, Plugins, Routing, Machines, Observe, Crons, Config).

### internal/gui/frontend/src/lib/components/Markdown.svelte
- **Role:** Assistant prose → typeset sans Markdown, rendered safely without `{@html}`.
- **Key symbols:**
  - props `source`.
  - `tokens` (`$derived.by`) — `marked.lexer` token tree (gfm, no raw-HTML trust).
  - snippets `inline` / `block` / `listItem` / `table` — recursive walk emitting native Svelte markup (auto-escaped); fenced code delegates to CodeBlock.
  - `openLink(e, href)` — opens via Wails `Browser.OpenURL` (falls back to `window.open`); `safeHref(href)` — allows only http(s)/mailto/tel/#//. schemes.
- **Depends on:** `marked`, `@wailsio/runtime` (Browser), `./CodeBlock.svelte`.
- **Used by:** views Chat, Memory, Dreaming, Skills, Profile.

### internal/gui/frontend/src/lib/components/Popover.svelte
- **Role:** Lightweight anchored floating panel (settings clusters, menus) over a transparent scrim.
- **Key symbols:**
  - props `label`, `align` (`start|end`), `width`, `open` (`$bindable`), `trigger` (Snippet receiving a toggle fn), `children`.
  - `place()` — measures the anchor rect to position below it; `toggle()`/`close()`; `onkeydown` (Escape closes, stops propagation); re-places on window resize.
- **Depends on:** `$lib/actions` (`trapFocus`).
- **Used by:** `views/Chat.svelte`.

### internal/gui/frontend/src/lib/components/Rail.svelte
- **Role:** Primary left-hand navigation grouped into Work / Knowledge / System zones with live count badges.
- **Key symbols:**
  - `zones` — static nav table mapping `Route`→label+glyph.
  - `badge(route)` — derives the count per route (feed act-on, running turns, bg tasks, live sessions, session total).
  - `liveRoutes` set + `live` flag — counts meaning active work go teal/breathing.
  - footer derives: `online`/`offline`/`footState`/`version` mirror the daemon connection.
- **Depends on:** `$lib/router.svelte`, `$lib/stores/sessions.svelte`, `$lib/stores/daemon.svelte`, `$lib/stores/feed.svelte`.
- **Used by / entrypoint:** mounted once in `App.svelte` (left column of the shell).

### internal/gui/frontend/src/lib/components/Sheet.svelte
- **Role:** Right-anchored slide-over modal dialog (dimmed scrim, focus-trapped).
- **Key symbols:**
  - props `open`, `label`, `width` (default 600), `onclose`, `title` (Snippet), `children`.
  - `onkeydown` — Escape closes; scrim click + header close button (a `Button variant="icon"`) also close.
- **Depends on:** `$lib/actions` (`trapFocus`), `./Button.svelte`.
- **Used by:** `views/Machines.svelte`.

### internal/gui/frontend/src/lib/components/Shortcuts.svelte
- **Role:** Global "?" keyboard cheatsheet overlay listing app shortcuts.
- **Key symbols:**
  - static `rows` table of key combos + descriptions.
  - `isTyping()` — suppresses the "?" toggle while focus is in an input/textarea/contenteditable.
  - `onKey` — toggles on "?" / closes on Escape; registered + torn down in `$effect`.
- **Depends on:** `$lib/actions` (`trapFocus`).
- **Used by / entrypoint:** mounted once in `App.svelte`; reached via the global "?" key.

### internal/gui/frontend/src/lib/components/StatusDot.svelte
- **Role:** Small breathing status indicator; `working` glows warmly.
- **Key symbols:**
  - props `state` (`working|idle|ok|warn|error`), `size` (px), `pulse` (opt-in breathe).
  - derived `dotState`/`isWorking`/`breathing` — drives the breathe + glow keyframes; stills under reduced-motion (holds working halo static).
- **Depends on:** none.
- **Used by:** 9 views (Home, Chat, Sessions, Live, Agents, Plugins, Routing, Machines, Crons), `TopBar.svelte`, and `ToolCallCard.svelte`.

### internal/gui/frontend/src/lib/components/ToastHost.svelte
- **Role:** Pointer-transparent transient-notification host (bottom-right stack).
- **Key symbols:**
  - reads `toasts.items` store; `toasts.dismiss(id)` on close.
  - constants `TTL_MS`/`ENTER_MS`/`EXIT_MS` (hand-aligned to motion tokens); `reduceMotion` gates transition durations to 0; `GLYPH` per `ToastKind`; auto-dismiss progress hairline pauses on hover.
- **Depends on:** `$lib/stores/toasts.svelte` (toasts, ToastKind), `svelte/transition` (fly, scale).
- **Used by / entrypoint:** mounted once in `App.svelte`; content is push-driven by the toasts store.

### internal/gui/frontend/src/lib/components/ToolCallCard.svelte
- **Role:** Richest transcript element — a collapsible record of one tool invocation, routed to a renderer by tool family.
- **Key symbols:**
  - props `block` (`ToolBlock`).
  - derived status: `dotState`/`tone` (error / done+result=ok / done-no-result=warn / running); `noResultAfterDone`/`hasResult`.
  - family classification: `MUTATION`/`OUTPUT` sets → `family` (`mutation|output|generic`); `key` (normalized tool name).
  - args helpers: `argsObj` (`$derived.by` JSON parse), `str(v)`, `pick(obj,...keys)`, `argPath`, `prettyArgs`, `argsLine`/`showArgsLine`.
  - presentation: `glyph` (per-tool sigil), `summary`+`firstLine(s)` (tool-aware one-liner), `resultLang`+`EXT_LANG` (language inference).
  - diff synthesis: `diffPatch` (`$derived.by`) builds a unified diff from edit/multi_edit/write/patch args via `header(path)`, `editHunk(old,new)`, `writeDiff(path,content)`, `splitLines(s)`; `hasDiff` gates it.
- **Depends on:** `$lib/stores/transcript.svelte` (ToolBlock), `./StatusDot.svelte`, `./CodeBlock.svelte`, `./DiffView.svelte`.
- **Used by:** `views/Chat.svelte`.

### internal/gui/frontend/src/lib/components/Tooltip.svelte
- **Role:** Delayed hover/focus tooltip primitive wrapping its trigger.
- **Key symbols:**
  - props `text`, `children` (Snippet); `OPEN_DELAY`=400ms.
  - `schedule()`/`clearTimer()`/`dismiss()` manage the intent-delay timer (cleared `onDestroy` + `$effect` cleanup).
  - `onpointerenter`/`onpointerleave`/`onfocusin` (only on `:focus-visible`)/`onfocusout`/`onkeydown` (Escape); `tipId` random for `aria-describedby`.
- **Depends on:** `svelte` (`onDestroy`).
- **Used by / entrypoint:** **none — no importer anywhere in the repo** (see Dead code).

### internal/gui/frontend/src/lib/components/TopBar.svelte
- **Role:** Top chrome — current page title + live daemon health + running-turns capsule.
- **Key symbols:**
  - props `actions` (optional right-aligned Snippet).
  - derived from the daemon store: `statusLabel`, `dotState` (ok/error/idle), `connecting`, `running` (running turns → teal breathing capsule).
- **Depends on:** `$lib/router.svelte` (route title), `$lib/stores/daemon.svelte`, `./StatusDot.svelte`.
- **Used by / entrypoint:** mounted once in `App.svelte` (top band of the shell).

### internal/gui/frontend/src/lib/components/VirtualList.svelte
- **Role:** Generic windowed/virtualized list (`<script generics="T">`) — renders only visible+overscan rows for large lists; optional pin-to-bottom.
- **Key symbols:**
  - props `items`, `estimateHeight`, `overscan`, `gap`, `pin`, `row` (Snippet `[T, number]`), `key`.
  - `keyOf`/`heightAt`/`offsets`/`totalH` — cumulative offset geometry over measured-or-estimated heights.
  - `firstVisible(top)` — binary search; `start`/`end`/`windowItems` derive the rendered window.
  - `onScroll`/`scrollToBottom`; `measure(node, key)` Svelte action — ResizeObserver feeds real heights back keyed by item identity; `$effect`s prune the measured map, track viewport height, and pin-to-bottom via rAF (cancelled on cleanup).
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** views Chat (transcript), Agents, Memory, Dreaming.

## Cross-links
- **gui-bridge** (`$lib/bridge`) — CommandPalette calls `Bridge.NewSession` / `Bridge.PruneSessions`; Markdown uses the Wails `@wailsio/runtime` Browser.OpenURL.
- **gui stores** (`$lib/stores/*`) — daemon (Rail, TopBar), sessions (Rail, CommandPalette), feed (Rail, CommandPalette), toasts (ToastHost, CommandPalette), transcript (`ToolBlock` consumed by ToolCallCard).
- **gui router** (`$lib/router.svelte`) — Rail, TopBar, CommandPalette read `router.route`/`routes` and call `router.go`.
- **gui actions** (`$lib/actions.ts`) — `trapFocus` action used by Popover, Sheet, Shortcuts.
- **gui-views-a / gui-views-b** (`lib/views/*.svelte`) and the **app shell** (`App.svelte`) — the consumers; every component here is mounted by a view or by `App.svelte`.
- **Design tokens** — all components read the global CSS custom properties (`tokens.css`: `--brand`, `--sp-*`, `--fs-*`, `--dur-*`, `--ease-*`, etc.); these are the implicit shared dependency of the whole slice.

## Dead code
- **`Tooltip.svelte`** (whole component) — **high confidence dead.** Grepped the entire repo (`*.svelte`, `*.ts`, `*.js`, `*.go`) for `Tooltip` and for the import path `$lib/components/Tooltip`; the only hit is the file itself. No barrel/`index` file re-exports it. It is a fully-built, accessible primitive that is simply never imported by any view, the app shell, or another component. (Note: hover titles in the app are currently done via native `title=` attributes, e.g. in Badge/Button/Card/Sheet/TopBar, rather than this component.)
