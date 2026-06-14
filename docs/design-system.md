# eigen design system

The single, durable brief for eigen's visual language. Source of truth for
color, type weight, glyphs, spacing, and the rules that keep the chat TUI
(`internal/tui`) and the app shell (`internal/app`) looking like ONE product.

Status: **v2 — 2026-06-14 (luxury redesign).** Built from a from-scratch
review (see `docs/design-inventory.md` for the map + `docs/design-references.md`
for the references). The system now has: a chosen signature palette (**deep
teal**), elevation surfaces, a coherent no-emoji icon set, one selection
treatment, a document-grade transcript (framed code on surfaces, syntax
tinting, markdown tables, pretty-printed + tinted JSON tool results), composed
vertical rhythm, and motion in several
places. When in doubt, this doc wins; update it in the same commit as any
visual change.

## v2 at a glance (the luxury redesign)
- **Palette = "deep teal"** (default; user-chosen): jewel petrol-teal brand
  `#3E9E96` + warm-clay Focus `#D08C5E` on a deep base `#0B0E0F`. Rich + composed
  jewel tones, bold via presence not saturation (rejected: brass=too brown,
  neon=too electric). nord/gruvbox remain as alternate palettes.
- **Elevation surfaces** (the "construction"/depth — Warp-like): `Base` (canvas)
  → `Surface` (rail, right panel, code blocks, tables) → `Overlay` (active
  session, selection, popovers). Painted via `fillBG` (internal/tui/surface.go),
  which re-asserts the bg after every reset so tints run flush. The rail + right
  panel sit on Surface; the active session on Overlay.
- **One coherent icon set, no emoji** (internal/theme/icons.go): a Nerd-Font
  tier (user runs JetBrainsMono NF) with a pure-Unicode geometric fallback,
  one glyph per tool (◇ read · ✎ edit · ⌕ search · ≣ list · » bash · ⊕ fetch ·
  ◈ task · ▦ image · ▪ tool). `❯` is reserved for the prompt/you-are-here.
- **One selection treatment**: a Focus `▎` bar + Focus-tinted text, shared by
  the chat rail, the app-shell rows, and selected transcript blocks.
- **Transcript as a document**: fenced code in a framed block on Surface with a
  lang chip + lightweight syntax tinting (keywords/strings/comments/numbers);
  markdown tables rendered aligned + bordered on Surface; crisp prose on the
  Text color; quiet (dim) diff context so +/− pop.
- **Composed rhythm**: an extra blank line before each user turn (turns read as
  sections); tight grouping within a turn.
- **Motion in several places**: the breathing-λ loader, the rail working
  spinner, and now a synced braille spinner on in-flight tool blocks in the
  transcript; tonal flash pills.

---


## Why a design system

eigen is a terminal app the user *lives in* (the end-user is the developer
moving all his work onto it), so polish is first-class, not decoration. A
terminal can't choose fonts (the emulator owns that — the user runs
zellij-in-ghostty), so the levers we DO control must be used with restraint and
consistency:

- **color** (truecolor, adaptive light/dark),
- **text weight** (bold / faint / italic / underline),
- **glyphs** (a small, fixed vocabulary),
- **spacing + alignment** (the band/sidebar geometry),
- **wrapping** (never overflow a slot).

The enemy is **drift**: two surfaces, dozens of call sites, ad-hoc
`lipgloss.Color("#...")` literals → a patchwork that stops reading as one
product. The system's job is to make the right thing the easy thing: ask for a
ROLE, never a hue.

## Core principles

1. **Roles, not hues.** Call sites import a semantic role (`Text`, `Dim`,
   `Accent`, `Focus`, `Ok`, …) from `internal/theme`, never a raw color. A
   re-theme is then one edit in `theme.go`.
2. **One palette, two surfaces.** `internal/theme` is imported by BOTH
   `internal/tui` and `internal/app`. Neither defines its own colors. (The app
   shell aliases roles in `internal/app/style.go` as `c*`/`s*` — those MUST map
   to `theme.*`, never to literals.)
3. **Restraint = information.** Every styled thing must MEAN something. Color
   is signal, not flair. Default to neutral; spend a bright/loud color only
   where it earns attention.
4. **Adaptive + degrading.** Every color is `lipgloss.AdaptiveColor{Dark,
   Light}` (Dark is the design target; Light stays legible on a light term).
   truecolor degrades to 256-color automatically. Never hardcode a single hex.
5. **Geometry owns layout; the system owns appearance.** Widths/rects come from
   `computeLayout`/`railRows`/`sidebarRows`; this doc governs what fills them.
   A styled line must still fit its slot (size-sweep tests enforce it; raw tabs
   are banned — expand to spaces before width math).

## The palette (current — `internal/theme/theme.go`)

Calm, desaturated, Nord-inspired. Roles:

| Role | Dark hex | Meaning / where |
|---|---|---|
| `Text` | `#D8DEE9` | primary prose, assistant answers |
| `Dim` | `#9aa5b8` | secondary: instructions, metadata, inactive rows |
| `Faint` | `#79839a` | chrome: separators, hairlines, disabled |
| **`Accent`** | `#81A1C1` | **BRAND/structural blue** — borders, rules, the input caret, nav |
| `Title` | `#88C0D0` | brand cyan — headings, the user's own voice (`❯` prompt), model name |
| `Focus` | `#D1A0B0` | **the active session THIS pane drives** — a non-blue rose (see the rule below) |
| `Sel` | `#D1A0B0` | **selected row / cursor in a list or picker** — same rose attention family as Focus, kept a separate role so call sites stay semantic |
| `Ok` | `#A3BE8C` | success / available / idle-ok |
| `Warn` | `#EBCB8B` | attention / confirm prompts |
| `Err` | `#BF616A` | failure / missing / error notes |
| `Tool` | `#B48EAD` | tool activity, counts, meta (violet) |
| `Code` | `#8FBCBB` | code + monospace spans |
| `Link` | `#88C0D0` | links (underlined at the call site) |
| `Working` | `#D08770` | the LOUD "actively thinking" loader + the working `●` status glyph — warm orange, unmistakable |
| `OnBright` | `#1b1f27` | text/glyph placed ON a bright fill (the flash pill); near-black dark / near-white light |

Animation ramps (theme-owned, not call-site literals): `BreathRamp` (the
brand-λ loader, an Accent brightness cycle using `FaintDim`/`Faint`/`Accent`/
`AccentBright`) and `WorkingRamp` (the app-shell live-session λ pulse, a Working
brightness cycle using `WorkingDim`/`Working`/`WorkingBright`).

Each has a ready `S<Role>` style (e.g. `theme.SFocus`); call sites compose
`.Bold()/.Underline()/.Italic()` on top.

### THE BRAND RULE (user-set, 2026-06-14)

> **Blue (`Accent` + `Title`) is reserved for eigen's brand + structural
> chrome.** Anything that is NOT brand — selection, the active session, state
> highlights — must use a DIFFERENT theme color, never the brand blue.

First application: the **active session** (the one this pane is attached to,
the `❯` pointer + its name, and the sidebar title row) uses **`Focus`** (rose),
not `Accent`/`Title`. With several windows open, the active one must pop
*against* the blue brand chrome, not blend into it.

Applied across both surfaces (commit after 395d158):
- **active session / "where you are"** → `Focus` (rose): rail pointer+name
  (`rail.go railEntryLabel`), sidebar title (`sidebar.go sbTitle`), the
  switcher "you are here" mark (`view.go`), project-open rail name
  (`rail.go`), the app-shell active rail page (`sRailActive`).
- **selected row / cursor** → `Sel` (rose): the app-shell selected row
  (`sRowSel`), the switcher selected line (`view.go`), the tray cursor
  (`tray.go`), the active right-panel tab (`rightpanel.go`).
- **working** → `Working` (orange): the `●` status glyph + the rail spinner
  now match the loader (were brand blue — a semantic mismatch, fixed).
- Brand blue (`Accent`/`Title`) KEEPS: the λ mark + caret, the "eigen"
  wordmark, the app title bar/breadcrumb, all borders/rules/hairlines, section
  headers ("navigate"/"session"/"sessions"), nav buttons, input cursors, the
  model-name segment, lit toggle state.

## Glyph vocabulary (fixed — don't invent per-site)

- **Brand:** `λ` (the eigenvalue mark; the working loader is a *breathing* λ +
  synced orange dot — `internal/tui/brand.go`). NOT a sparkle/sunburst
  (deliberately unlike Claude/Codex).
- **Session/agent status:** `●` working · `○` idle · `◆` approval-wait ·
  `✗` error — the shared status language (`statusGlyph`). Working animates a
  braille spinner in the rail.
- **Navigation/structure:** `❯` you-are-here / prompt caret · `▸`/`▾`
  collapsed/expanded · `‹ back`.
- **Composer/voice:** `⏺` speak · `▶` read · `◉` voice · `◌` transcribing ·
  `▷` speaking · `⊘` muted.
- **Tasks badge:** `⚒`.

A new glyph needs a reason; reuse the vocabulary first.

## Type weight

- **Bold** = the one thing to look at in a region (active session name, a
  section's own title, a confirm). Don't bold everything.
- **Faint/Dim** = recede (inactive rows, hints, separators).
- **Italic** = rare; reserved (e.g. quoted/secondary).
- **Underline** = links only.

## Drift surface — CONVERGED (2026-06-14)

All raw color literals are now inside `internal/theme/theme.go`. The former
offenders were folded into roles/ramps:

- `internal/tui/brand.go` breathing-λ ramp → `theme.BreathRamp`.
- `internal/tui/view.go` flash-pill text `#1b1f27` → `theme.OnBright`.
- `internal/app/app.go` live-session ramp → `theme.WorkingRamp`.

A drift-guard test (`internal/theme/drift_test.go`,
`TestNoRawColorLiteralsOutsideTheme`) walks the whole tree and FAILS the build
if any `lipgloss.Color("…")` / `AdaptiveColor{…}` appears outside
`internal/theme` (tests excepted). Adding a color now MUST add a role.

## The plan (the "design system" effort)

Bootstrapped here; the full effort:

1. **Lock the role vocabulary** — DONE. Added `Focus` (active session), `Sel`
   (selection/cursor), `OnBright` (text on bright fill), and the theme-owned
   ramps `BreathRamp`/`WorkingRamp` (+ their `*Dim`/`*Bright` stops).
2. **Apply the brand rule everywhere** — DONE for tui + app: all
   selection/active/current highlights moved off `Accent`/`Title` onto
   `Focus`/`Sel`; the working glyph moved to `Working`; the 3 raw literals
   folded into roles. (Screenshot-verified in both surfaces.)
3. **App-shell parity** — DONE: `internal/app/style.go` aliases all map to
   `theme.*` (no literals); the drift-guard test enforces it tree-wide.
4. **A living swatch** — DONE: `eigen theme` renders every role (with a color
   chip), the animation ramps, the weight scale, and the glyph vocabulary +
   the brand rule (internal/theme/swatch.go). Run it to eyeball the whole
   system / verify a re-theme.
5. **Re-theme proof** — DONE: `theme.Palette` is the data a theme IS; named
   palettes `nord` (default) + `gruvbox` (warm, higher-contrast) live in
   theme.go. Select via the config `theme` key or `EIGEN_THEME=<name>`; the
   role vars + every S* style + both ramps derive from the chosen palette at
   init, so it re-themes the WHOLE product with zero call-site changes. main.go
   re-execs once with EIGEN_THEME set when the config picks a non-default theme
   (init happens before main). Brand rule holds in every palette (Focus/Sel ≠
   Accent/Title — enforced by TestReThemeSwapsAllRoles).
6. **Tests** — DONE (drift guard); keep the size-sweep + band-alignment nets
   green.

Constraints carried from the chrome work: geometry-owned-first, one action
layer, keyboard/click parity, restrained design, degrade under 80 cols, and the
ANSI-width/tab-expansion rules (no row exceeds its slot).
