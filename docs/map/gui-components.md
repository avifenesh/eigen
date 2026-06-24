# GUI shared components

The reusable Svelte 5 building blocks of the Eigen desktop GUI (Wails v3 + Svelte 5).
Everything under `internal/gui/frontend/src/lib/components/` is presentation-layer: small,
mostly-stateless `.svelte` components consumed by the route views (`lib/views/*.svelte`) and the
app shell (`App.svelte`). They split into three loose tiers: **primitives** (Badge, Button, Card,
StatusDot, EmptyState — pure visual atoms taking props + a `children` snippet), **overlays /
chrome** (Rail, TopBar, Sheet, Popover, Tooltip, ToastHost, CommandPalette, Shortcuts — app
furniture, focus-trapped dialogs, and global keyboard surfaces), and **content renderers**
(Composer, Markdown, CodeBlock, DiffView, ToolCallCard, VirtualList — take chat input and turn
daemon/model output into typeset text, syntax-tinted code, unified diffs, collapsible tool records,
and windowed lists). All styling flows from CSS custom-property design tokens (`--brand`, `--sp-*`,
`--fs-*`, `--dur-*`, etc.) defined elsewhere in the frontend; every component honors
`prefers-reduced-motion`. The only places these components reach beyond the frontend are the Wails
bridge (`$lib/bridge`), the reactive stores (`$lib/stores/*`), the router (`$lib/router.svelte`),
the shared DTO types (`$lib/types`), and the `trapFocus` action (`$lib/actions`).

## Files

### internal/gui/frontend/src/lib/components/Badge.svelte
- **Role:** Tone-coded pill label for statuses / model ids / numeric counts.
- **Key symbols:**
  - props `tone` (`neutral|brand|success|warn|error|info`), `truncate` (clamp to 16ch + ellipsis), `title` (explicit tooltip override), `children` (Snippet) — a `<span>` pill; numeric content stays tabular (`tnum`).
  - self-tooltip: when `truncate` is on and no explicit `title` is passed, a `MutationObserver` (wired in an `$effect`, torn down on cleanup) keeps `captured` in sync with the label's rendered text so the clamped remainder is always recoverable as a native `title`; `tooltip` (`$derived`) resolves explicit `title` → captured text → none.
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
- **Role:** Syntax-tinted monospace code surface with a language label + copy button, guarded against huge blobs.
- **Key symbols:**
  - props `code`, `lang`.
  - `copy()` — writes to `navigator.clipboard`; on success flashes a one-shot `copied` state (1.4s), on reject flashes a `failed` state (✕ + "copy failed", error-tinted) rather than looking dead; both `copyTimer`/`failTimer` cleared `onDestroy`. `canCopy` (`$derived`) hides the control when there's nothing to copy.
  - huge-blob guard: `MAX_LINES`=2000 / `MAX_BYTES`=200_000; `byteLen`/`lineCount`/`tooLarge` (`$derived`) measure the source; above either cap only `headSlice(src)` is tinted+rendered behind a "show full (N lines)" expander (`expanded` state, reset whenever `code` changes). `rendered` is the slice-or-whole string that actually gets tinted.
  - `esc(s)` — HTML-escapes before injection (no raw code as HTML).
  - `html` (`$derived.by`) — tokenizes `rendered` on comment/string boundaries, then tints keywords/numbers via regex (`COMMENT`, `STRING`, `KEYWORDS`, `NUMBER`); `tintPlain(seg)` does the keyword/number pass. Cheap regex tint, no highlighter dependency.
- **Depends on:** `svelte` (`onDestroy`).
- **Used by / entrypoint:** directly by `views/Agents.svelte`; transitively the main consumer — rendered by `Markdown.svelte` (fenced code) and `ToolCallCard.svelte` (args/result/error wells).

### internal/gui/frontend/src/lib/components/CommandPalette.svelte
- **Role:** ⌘K/Ctrl+K fuzzy command palette — run a verb, jump to a view, or open a session.
- **Key symbols:**
  - `fuzzy(q, text)` — subsequence scorer rewarding contiguous runs + word-start hits (-1 = no match).
  - `actions` (static verbs) + `navItems` (from `routes`, minus `chat`) + per-render Sessions (from `sessions.list`); `items` / `grouped` (`$derived.by`) filter/sort by score (best first, group order as tiebreak) and inject non-selectable section headers. `Item` carries an optional stable `id` (sessions key on `s.id` since their labels collide).
  - actions: `newSession()` (`Bridge.NewSession` → refresh → nav chat), `pruneEmpty()` (`Bridge.PruneSessions`, toasts the pruned count), `refreshFeed()` (nav home + `feed.refresh` + toast), `go(route)`.
  - `show()`/`hide()`/`run(i)`; `onWinKey` (global ⌘K toggle / Escape) + `onOverlay` (yields the screen when another overlay — the Shortcuts sheet — dispatches `eigen:overlay`; `show()` itself dispatches `eigen:overlay` so only one keyboard surface owns the screen) both registered/torn-down in `$effect`; `onListKey` (↑/↓/Enter within input).
  - `track(el, index)` Svelte action registers each row in `rows` keyed by its `items` index and drops stale nodes on update/destroy; an `$effect` scrolls the active row into view (`block: "nearest"`) as arrowing drags past the 64vh-capped window.
- **Depends on:** `$lib/router.svelte` (router, routes, Route), `$lib/stores/sessions.svelte`, `$lib/stores/feed.svelte`, `$lib/stores/toasts.svelte`, `$lib/bridge` (Bridge), `$lib/actions` (`trapFocus`).
- **Used by / entrypoint:** mounted once in `App.svelte`; reached via the global ⌘K/Ctrl+K window listener.

### internal/gui/frontend/src/lib/components/Composer.svelte
- **Role:** Chat message composer — auto-growing textarea, Enter-sends, Stop-while-running, with image intake (paste / drop / file-pick).
- **Key symbols:**
  - props `running` (flips Send→Stop), `disabled`, `disabledReason` (surfaced inline + as title when daemon offline), `onsend` (now `(text, images: ImageDTO[]) => boolean | void | Promise<…>`; a `false` return means rejected → draft survives), `oninterrupt`.
  - `grow()` — measures `scrollHeight`, clamps to `MAX_ROWS` (8) read from the line box.
  - `send()` — `async`, guards `canSend && !sending`, awaits `onsend`; clears text + revokes the image batch + regrows ONLY when `onsend` doesn't return `false` (a failed RPC keeps the carefully-composed prompt). `sending` state disables a double-fire. `onkeydown` (Enter sends, Shift+Enter newlines, respects IME composing).
  - image attachments: `Attachment` (`{ id; image: ImageDTO; url }`) pairs the wire DTO with a locally-owned thumbnail object-URL; `intake(file)` reads an image into raw base64 (data: prefix stripped) + an object-URL, rejecting non-images; `removeAttachment(id)`/`clearAttachments()` revoke owned URLs; `onDestroy(clearAttachments)` is the leak contract. Sources: `onpaste` (image clipboard items), `onDragEnter/Over/Leave/Drop` (drag-depth tracked so nested children don't flicker `dragging`), `onPick` (hidden file input driven by the attach icon Button).
  - derived: `trimmed`, `canSend` (text-or-images, online, not running), `hint`/`status` (char count / image count / shortcut help), `offlineReason`; `statusId` (`$props.id()`) wires the textarea's `aria-describedby` to the live status line.
- **Depends on:** `./Button.svelte`, `$lib/types` (`ImageDTO`), `svelte` (`onDestroy`).
- **Used by:** `views/Chat.svelte`.

### internal/gui/frontend/src/lib/components/DiffView.svelte
- **Role:** Unified-diff renderer — the showpiece detail surface (better than a terminal diff).
- **Key symbols:**
  - props `patch`.
  - `parsed` (`$derived.by<Parsed>`) — ONE pass that both classifies every line into `Kind` (`add|del|ctx|hunk|meta`; meta now also matches `new file`/`deleted file`/`rename`/`similarity`/`\ ` lines so a raw `git diff` paste renders gracefully) AND tallies `additions`/`deletions`/`hunks` inline; `rows`/`additions`/`deletions`/`hunks`/`empty` read off it.
  - copy affordance: `copy()` writes the raw `patch` to the clipboard with a 1.4s `copied` confirm (`copyTimer` cleared `onDestroy`), mirroring CodeBlock; a silent no-op on reject.
  - diffstat eyebrow renders the counts, a proportional add/del balance `bar`, and the copy button; del rows use a U+2212 minus to optically match `+`. Changed rows carry an sr-only "added"/"removed" prefix while the +/− gutter is aria-hidden; the `<table>` is `role="presentation"`.
  - large-diff collapse: `LARGE`=400, `PREVIEW`=120, `isLarge`, `expanded`, `visibleRows`, `hiddenCount`.
  - `hunkRange(text)` / `hunkLabel(text)` — split `@@ -a,b +c,d @@` coordinates from the trailing context label.
- **Depends on:** `svelte` (`onDestroy`); otherwise self-contained types.
- **Used by / entrypoint:** directly by `views/Dreaming.svelte`; also rendered by `ToolCallCard.svelte` for synthesized mutation diffs.

### internal/gui/frontend/src/lib/components/EmptyState.svelte
- **Role:** Calm zero-data / not-yet-built placeholder with optional single action.
- **Key symbols:**
  - props `glyph`, `title`, `line`, `action` (Snippet), `headingLevel` (`1..6`, default 2 — slots the title into the surrounding document outline).
  - `hasAction` (`$derived`) — when present, the glyph warms to teal with a halo + hairline tether toward the action; reduced-motion-safe staggered mount rise (glyph leads, text/action settle behind). `titleTag` (`$derived`) renders the title via `<svelte:element>` at `headingLevel`.
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** `App.svelte` + 13 views (Chat, Sessions, Live, Agents, Memory, Dreaming, Skills, Plugins, Routing, Machines, Observe, Crons, Config).

### internal/gui/frontend/src/lib/components/Markdown.svelte
- **Role:** Assistant prose → typeset sans Markdown, rendered safely without `{@html}`.
- **Key symbols:**
  - props `source`.
  - `tokens` (`$derived.by`) — `marked.lexer` token tree (`gfm: true`, `breaks: false`, no raw-HTML trust; throws caught → `[]`).
  - snippets `inline` / `block` / `listItem` / `table` — recursive walk emitting native Svelte markup (auto-escaped); fenced code delegates to CodeBlock, task-list items render read-only checkbox glyphs, images render alt text only (never load remote model URLs), and a raw-`html` token emits its literal text verbatim on a mono `<pre>` while a pure HTML-comment token is skipped as source noise.
  - `openLink(e, href)` — opens via Wails `Browser.OpenURL` (falls back to `window.open`); `safeHref(href)` — allows only http(s)/mailto/tel/#//. schemes.
- **Depends on:** `marked`, `@wailsio/runtime` (Browser), `./CodeBlock.svelte`.
- **Used by:** views Chat, Memory, Dreaming, Skills, Profile.

### internal/gui/frontend/src/lib/components/Popover.svelte
- **Role:** Lightweight anchored floating panel (settings clusters, menus) over a transparent scrim.
- **Key symbols:**
  - props `label`, `align` (`start|end`), `width`, `open` (`$bindable`), `trigger` (Snippet receiving a toggle fn), `children`.
  - `place()` — measures the anchor rect to position the panel below-and-edge-aligned, but flips to anchor ABOVE the trigger when the measured panel height (read off the bound `panel` ref) would overflow the viewport bottom and there's more room above; `pos` holds `{ top | bottom, left, right }` (only one vertical edge set). An `$effect` re-runs `place()` once the panel renders (its real height is now known); `toggle()`/`close()`; `onkeydown` (Escape closes, stops propagation); re-places on `<svelte:window>` resize.
- **Depends on:** `$lib/actions` (`trapFocus`).
- **Used by:** `views/Chat.svelte`.

### internal/gui/frontend/src/lib/components/Rail.svelte
- **Role:** Primary left-hand navigation grouped into Work / Knowledge / System zones with live count badges, bookended by a breathing brand dot (top) and a daemon-status footer (bottom).
- **Key symbols:**
  - `zones` — static nav table mapping `Route`→label+glyph: Work (home, chat, agents, live, sessions), Knowledge (memory, dreaming, skills), System (observe, routing, machines, crons, plugins, profile, config).
  - `badge(route)` — derives the count per route: home → `feed.actOn.length`, chat → `running_turns`, agents → `bg_tasks`, live → sessions with status `working`/`approval`, sessions → `sessions.count`.
  - `liveRoutes` set (home/chat/agents/live) + per-item `live` flag — a non-zero count on those routes goes teal/breathing; a neutral tally (total sessions) stays quiet.
  - footer derives: `online`/`offline`/`footState` mirror the daemon connection; `version` prefers `daemon.daemonVersion` else `daemon.guiVersion`; `mismatch` (`daemon.versionMismatch`) warn-tints the version stamp and appends a ⚠, with `versionTitle` spelling out the daemon-vs-gui revisions.
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
  - static `rows` table of key combos + descriptions, plus a `paletteNote` pointing at the palette's verbs (so the two never drift out of sync).
  - `isTyping()` — suppresses the "?" toggle while focus is in an input/textarea/contenteditable.
  - `show()` dispatches `eigen:overlay` so the command palette yields; `onKey` toggles on "?" / closes on Escape; `onOverlay` closes when another overlay (the palette) opens. Both listeners registered + torn down in `$effect`.
- **Depends on:** `$lib/actions` (`trapFocus`).
- **Used by / entrypoint:** mounted once in `App.svelte`; reached via the global "?" key.

### internal/gui/frontend/src/lib/components/StatusDot.svelte
- **Role:** Small breathing status indicator; `working` glows warmly.
- **Key symbols:**
  - props `state` (`working|idle|ok|warn|error`), `size` (px), `pulse` (opt-in breathe), `label` (`string | boolean`).
  - `dotState`/`isWorking`/`breathing` (`$derived`) — drive the breathe + glow keyframes; still under reduced-motion (holds working halo static).
  - accessibility: decorative (`aria-hidden`) by default; `label` (a truthy string, or `true` to use `defaultLabels[state]`) promotes the dot to `role="img"` with that `aria-label` so it speaks where it is the only state carrier. `ariaLabel` (`$derived`) resolves the spoken text.
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
- **Role:** Delayed hover/focus tooltip primitive wrapping its trigger, with a directional bubble + tail.
- **Key symbols:**
  - props `text`, `placement` (`top|bottom|left|right`, default `top`), `children` (Snippet); `OPEN_DELAY`=400ms; a falsy `text` is a no-op (renders only the trigger).
  - `schedule()`/`clearTimer()`/`dismiss()` manage the intent-delay timer (cleared via `$effect` cleanup on unmount).
  - `onpointerenter`/`onpointerleave`/`onfocusin` (only on `:focus-visible`)/`onfocusout`/`onkeydown` (Escape); `tipId` (`$props.id()`) wires `aria-describedby` ↔ the `role="tooltip"` bubble.
- **Depends on:** none (svelte `Snippet` type only).
- **Used by / entrypoint:** **none — no importer anywhere in the repo** (see Dead code).

### internal/gui/frontend/src/lib/components/TopBar.svelte
- **Role:** Top chrome — current page title + live daemon health + running-turns capsule.
- **Key symbols:**
  - props `actions` (optional right-aligned Snippet).
  - `title` (`$derived.by`) — on the `chat` route with a routed `router.param`, resolves to that session's title from `sessions.list` (fallback "Chat"); every other route is its own name. `verbatimTitle` (`$derived`) flags a resolved session title so it keeps its own casing (route names get capitalized).
  - derived from the daemon store: `statusLabel`, `dotState` (ok/error/idle), `connecting`, `running` (`running_turns` → teal breathing capsule when > 0).
- **Depends on:** `$lib/router.svelte` (route + param), `$lib/stores/daemon.svelte`, `$lib/stores/sessions.svelte`, `./StatusDot.svelte`.
- **Used by / entrypoint:** mounted once in `App.svelte` (top band of the shell).

### internal/gui/frontend/src/lib/components/VirtualList.svelte
- **Role:** Generic windowed/virtualized list (`<script generics="T">`) — renders only visible+overscan rows for large lists; optional pin-to-bottom.
- **Key symbols:**
  - props `items`, `estimateHeight`, `overscan`, `gap`, `pin`, `row` (Snippet `[T, number]`), `key`.
  - `keyOf`/`heightAt`/`offsets`/`totalH` — cumulative offset geometry over measured-or-estimated heights (`measured` is a Map keyed by item identity, not array index, so a splice/eviction keeps each row's real height); `indexByKey` maps key→index for anchor compensation.
  - `firstVisible(top)` — binary search; `start`/`end`/`windowItems` derive the rendered window.
  - `onScroll` rAF-coalesces scroll reads to one per frame (`scrollRaf`); `scrollToBottom`; `measure(node, key)` Svelte action — a ResizeObserver feeds real heights back keyed by item identity AND, when a row ABOVE the scroll position remeasures, adds the height delta to `scrollTop` (`anchorDelta`/`anchorRaf`) so the viewport stays put instead of jumping. `$effect`s prune the measured map to live keys, track viewport height (ResizeObserver), and pin-to-bottom via rAF (`pinRaf`). All rAF handles (`scrollRaf`/`anchorRaf`/`pinRaf`) are cancelled on cleanup.
- **Depends on:** none (svelte `Snippet` type only).
- **Used by:** views Chat (transcript), Agents, Memory, Dreaming.

## Cross-links
- **gui-bridge** (`$lib/bridge`) — CommandPalette calls `Bridge.NewSession` / `Bridge.PruneSessions`; Markdown uses the Wails `@wailsio/runtime` Browser.OpenURL.
- **gui stores** (`$lib/stores/*`) — daemon (Rail, TopBar; including `daemonVersion`/`guiVersion`/`versionMismatch` + the `running_turns`/`bg_tasks` stats), sessions (Rail, TopBar, CommandPalette), feed (Rail `actOn`, CommandPalette `refresh`), toasts (ToastHost, CommandPalette), transcript (`ToolBlock` consumed by ToolCallCard).
- **gui types** (`$lib/types`) — Composer's `onsend` carries `ImageDTO[]` (`{ mediaType; data }`, raw base64).
- **gui router** (`$lib/router.svelte`) — Rail, TopBar, CommandPalette read `router.route`/`router.param`/`routes` and call `router.go`.
- **gui actions** (`$lib/actions.ts`) — `trapFocus` action used by CommandPalette, Popover, Sheet, Shortcuts.
- **`eigen:overlay` event** — CommandPalette and Shortcuts dispatch/listen for this window CustomEvent so only one top-level keyboard-grabbing overlay owns the screen at a time.
- **gui-views-a / gui-views-b** (`lib/views/*.svelte`) and the **app shell** (`App.svelte`) — the consumers; every component here is mounted by a view or by `App.svelte`.
- **Design tokens** — all components read the global CSS custom properties (`tokens.css`: `--brand`, `--sp-*`, `--fs-*`, `--dur-*`, `--ease-*`, etc.); these are the implicit shared dependency of the whole slice.

## Dead code
- **`Tooltip.svelte`** (whole component) — **high confidence dead.** Grepped the entire repo (`*.svelte`, `*.ts`, `*.js`, `*.go`) for `Tooltip` and for the import path `$lib/components/Tooltip`; the only hit is the file itself. No barrel/`index` file re-exports it. It is a fully-built, accessible primitive that is simply never imported by any view, the app shell, or another component. (Note: hover titles in the app are currently done via native `title=` attributes, e.g. in Badge/Button/Card/Sheet/TopBar, rather than this component.)
