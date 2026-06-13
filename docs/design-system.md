# eigen design system

The single, durable brief for eigen's visual language. Source of truth for
color, type weight, glyphs, spacing, and the rules that keep the chat TUI
(`internal/tui`) and the app shell (`internal/app`) looking like ONE product.

Status: **v0 — bootstrapped 2026-06-14.** Color roles + the brand rule are
implemented in `internal/theme`; this doc captures the system and the work
still to do. When in doubt, this doc wins; update it in the same commit as any
visual change.

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
| `Ok` | `#A3BE8C` | success / available / idle-ok |
| `Warn` | `#EBCB8B` | attention / confirm prompts |
| `Err` | `#BF616A` | failure / missing / error notes |
| `Tool` | `#B48EAD` | tool activity, counts, meta (violet) |
| `Code` | `#8FBCBB` | code + monospace spans |
| `Link` | `#88C0D0` | links (underlined at the call site) |
| `Working` | `#D08770` | the LOUD "actively thinking" loader — warm orange, unmistakable |

Each has a ready `S<Role>` style (e.g. `theme.SFocus`); call sites compose
`.Bold()/.Underline()/.Italic()` on top.

### THE BRAND RULE (user-set, 2026-06-14)

> **Blue (`Accent` + `Title`) is reserved for eigen's brand + structural
> chrome.** Anything that is NOT brand — selection, the active session, state
> highlights — must use a DIFFERENT theme color, never the brand blue.

First application: the **active session** (the one this pane is attached to,
the `❯` pointer + its name, and the sidebar title row) uses **`Focus`** (rose),
not `Accent`/`Title`. With several windows open, the active one must pop
*against* the blue brand chrome, not blend into it. (Shipped: rail.go
`railEntryLabel`, sidebar.go `sbTitle`.)

Open follow-through (audit): other "selection/active" highlights that still use
brand blue should move to `Focus` (or another non-brand role) for consistency —
e.g. app-shell selected rows, picker cursors, the in-window switcher's current
mark. List them as they're found.

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

## Drift surface (what the system must converge — audited 2026-06-14)

Raw color literals OUTSIDE `theme.go` (each is a candidate to fold into a role):

- `internal/tui/brand.go` — the breathing-λ brightness `breathRamp` (a ramp,
  arguably a brand primitive; could live in theme as a documented ramp).
- `internal/tui/view.go` — the code-block background `#1b1f27` (should be a
  `theme.CodeBg` role).
- `internal/app/app.go` — the live-glyph breathing ramp (same family as
  brand.go's; share one ramp).

Everything else routes through `theme.*` today — keep it that way; a PR adding
a `lipgloss.Color("#...")` outside theme.go should be rejected in review.

## The plan (the "design system" effort)

Bootstrapped here; the full effort (post-compaction) should:

1. **Lock the role vocabulary** — confirm/extend the role list above
   (candidates: `Focus` ✓ shipped, `CodeBg`, a documented brand `Ramp`,
   maybe `Sel` for app-shell selection if it should differ from `Focus`).
2. **Apply the brand rule everywhere** — sweep all "selection/active/current"
   highlights off `Accent`/`Title` onto `Focus`/`Sel`; sweep the 3 raw literals
   into roles. One PR per surface, screenshot-verified.
3. **App-shell parity** — make `internal/app/style.go`'s `c*`/`s*` aliases a
   thin, audited mapping to `theme.*` (no literals), so the dashboard and chat
   are provably one palette.
4. **A living swatch** — a tiny `eigen theme` (or a hidden debug page) that
   renders every role + glyph + weight, so changes are eyeballed in one place
   and screenshots are reproducible.
5. **Re-theme proof** — demonstrate a one-edit alternate palette (e.g. a warmer
   or higher-contrast variant) to prove the roles-not-hues discipline holds.
   Possibly a config `theme:` key selecting a named palette.
6. **Tests** — assert no `lipgloss.Color(`/`AdaptiveColor{` outside theme.go
   (a grep test), and keep the size-sweep + band-alignment nets green.

Constraints carried from the chrome work: geometry-owned-first, one action
layer, keyboard/click parity, restrained design, degrade under 80 cols, and the
ANSI-width/tab-expansion rules (no row exceeds its slot).
