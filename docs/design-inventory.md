# eigen TUI — current-state visual inventory (the map)

A complete census of every visual atom in eigen's terminal UI, taken before a
**from-scratch** design-system redesign. The goal of the redesign (user's words):
*"high value, luxury, a joy to look at and to use — show this is a super app."*
This doc is the MAP we reason over; it is descriptive (what exists today) +
opinionated (what undermines the luxury feel). It is NOT the new system — that
we build next, from zero.

Method: direct source census (grep + read) of `internal/tui` and `internal/app`
(the model gateway was down for the agent fleet, so this was done deterministically
— which is actually more exhaustive). File:line refs throughout so the redesign
is actionable.

---

## 1. Glyph census

### Brand
- `λ` — the eigenvalue mark (brand.go). Loader = a *breathing* λ + a synced
  orange dot. Good, distinctive, NOT a sparkle/sunburst. Keep.

### Status (the shared status language)
- `●` working · `○` idle · `◆` approval-wait · `✗` error — `statusGlyph`
  (view.go:315), app `liveGlyph` (app.go:632), plan, taskspanel, crons.
- Working `●` is orange (theme.Working) after the recent fix; idle `○` dim.

### Navigation / structure
- `❯` — you-are-here pointer (rail.go:263), the user prompt caret (blocks.go:293,
  tui.go:1793), AND the bash tool icon (blocks.go:107). **Reused for 3 meanings.**
- `▸`/`▾` — collapsed/expanded (blocks.go tool headers, plan.go, rail groups).
- `‹` `›` — back / breadcrumb separators (configpanel, header, app title bar).
- `❭` — tool-result expand marker (blocks.go:328) — a 4th caret variant alongside ❯/▸/›.
- `▎` `▏` — selection bar (tray.go:104) and the tool/thinking left-lane gutter
  rule (blocks.go:365). Two different vertical-bar weights for two ideas.
- `│ ╭ ╮ ╰ ╯ ─ ═` — box drawing: header frame, code fences, panel borders,
  heading underlines (═ h1, ─ h2), section hairlines.
- `◧` `◨` — header panel-toggle buttons (left rail / right panel).
- `→ ↑ ↓` — token-rate arrows in the status bar (↑in ↓out), feed/misc.

### Content markers
- `•` (U+2022) — list bullets (blocks.go:472, violet).
- `·` (U+00B7) — separator in hint lines, the switcher "you are here" mark,
  rail current mark historically. **Overloaded: separator AND a marker.**
- `−` (U+2212) / `+` — diff del/add; `±` — git feed item (home.go).
- `⋯` — truncation/ellipsis in apply_patch sections (23 uses).
- `…` (U+2026) — text truncation elsewhere. **Two ellipsis conventions.**
- `✓` — done/saved (tool headers, plan, configpanel "✓ saved", flash, taskspanel).
- `▤` — list tool icon.

### Tool icons — EMOJI mixed with line-art (the biggest tell)
`toolIcon` (blocks.go:96): read `📖`, grep/glob `🔍`, fetch `🌐` are **full-color
emoji**, while write/edit `✎`, bash `❯`, task `⚙`, generic `▸` are monochrome
line-art. Emoji are double-width, render differently per-terminal/font, and
break the restrained monochrome palette — they read "dev tool / Slack", not
"luxury instrument." **This is the #1 visual inconsistency.**

### Voice / composer
- `⏺` speak · `▶` read · `◉` voice · `◌` transcribing · `▷` speaking · `⊘` muted.
  A coherent little set, but `◉`/`◌`/`▷` are visually close.

### App-shell page rail + feed
- Page glyphs: `⌂` home, `⇆` sessions, `+` new, `⚙` config, etc.
- Feed kind glyphs (home.go): git `±`, github `◉`, memory `↺`, suggest `✧`,
  default `·`. `◉` here = github, but `◉` in the chat composer = voice. **Reused.**

### Inconsistencies flagged
1. **Emoji (📖🔍🌐) mixed with line-art** — kills monochrome restraint.
2. **`❯` means 3 things** (prompt, you-are-here, bash) and there are **4 caret
   shapes** (❯ ▸ › ❭) doing related "pointer/expand" jobs.
3. **`◉` reused** (voice on / github feed) and `·` reused (separator / marker).
4. **Two ellipses** (`⋯` vs `…`), and a stray en-dash `[–]` (sidebar.go:47)
   among em-dashes everywhere else.
5. **Two vertical bars** (`▎` selection, `▏` gutter) — subtle, intentional-ish.

---

## 2. Color system (post Tier-22)

Roles live in `internal/theme/theme.go` (nord default + gruvbox): Text, Dim,
Faint, Accent (brand blue), Title (brand cyan), Focus/Sel (rose, non-brand),
Ok/Warn/Err, Tool (violet), Code (teal), Link, Working (orange), OnBright.
The **brand rule** (blue = brand/structure only) is applied and drift-guarded.

What works: the brand rule gives a clear blue-vs-rose split (chrome vs "where
you are"); semantic Ok/Warn/Err are conventional; Working orange is unmistakable.

Where color is still thin / ambiguous (designer view):
- **The palette is desaturated-calm but FLAT** — there's little tonal depth (no
  surface/elevation layers, no subtle backgrounds). Everything is fg-on-default;
  the only bg fills are the flash pill + code-block (none, actually — code has no
  bg). A luxury feel usually comes from *surfaces* (subtle panel tints, a hair of
  elevation), which we have zero of.
- **No hierarchy within "dim"** — Dim and Faint do a lot of work; there's no
  "tertiary" or "ghost" tier, so secondary content all reads at one weight.
- **Tool violet + Code teal + Link cyan + Title cyan** crowd the cool end; with
  Accent blue too, the cool hues are doing 5 jobs and can blur.
- **Code spans/blocks have NO background and NO syntax tint** — code looks like
  prose in teal. A premium transcript tints code on a subtle surface.
- **Semantic colors only as foreground** — an error is red *text*, never a
  surface; confirms/approvals don't get a calm highlighted region.

---

## 3. Layout, spacing, geometry

Composition: `[ left sidebar | transcript | right panel ]` with a composer +
(narrow-only) header/status. Constants: rail `railWidthCols=22`, right panel
`rightPanelWidthCols=38` (24–100), `minTranscriptCols=40`, sidebar shows ≥
`railMinTerminalWidth=80`, header de-borders below `headerBorderMinRows=14`.
App shell breakpoints: narrow ≤72, normal, wide ≥130 (adds inspector).

Designer view:
- **Rhythm is functional, not composed.** Sections are separated by single blank
  lines + hairline rules; spacing is uniform-1, not a deliberate scale (no
  4/8-style spacing system, no generous "air" around key moments).
- **1-col gutters everywhere** — tight. A luxury layout breathes more between the
  rail and transcript, and around the composer.
- **Borders are uniform rounded boxes** (header, panels, input). Fine, but every
  region framed the same way reads "form", not "composed surface." Premium UIs
  vary framing (some surfaces float on tint, some on a hairline, few on a full box).
- **No sense of elevation/z-order in the visuals** — the rail, transcript, and
  panel are coplanar; nothing recedes or lifts.
- Widths are mostly named constants (good), a few magic numbers (heading
  underline cap 48, rule width 24, preview 70 chars).

---

## 4. Components & states

- **Sidebar** (sidebar.go): brand row, nav items (idle dim / lit accent /
  toggle-state), status rows (model/perm/effort/ctx/tok/route/vision — each a
  status segment with its own color+click), plan/todo rows, session rail
  (idle ○ / working ● spinner / approval ◆ / error ✗ / **active-this-pane** rose
  ❯+name), grouped/collapsed projects (▸/▾ + rollup glyph).
- **Header** (header.go): bordered 3-row (≥14 rows) vs single-line; buttons
  `[home][sessions][+new][config]` + `[◧][◨]` toggles; breadcrumb; title-click=rename.
- **Right panel** (rightpanel.go + changes/git/term/taskspanel): tabs
  `[changes][git][term][tasks]`, active tab rose bold / inactive dim, per-tab
  empty states, the real PTY terminal.
- **Composer** (composer.go/input.go): empty / typing / multiline; the voice bar
  `⏺ speak · ▶ read · ◉ voice` right-aligned under the input; queued-while-running.
- **Overlays** (overlay.go): confirm y/n, text prompt — a single bottom line.
- **Palette** (palette.go), **pickers** (model/effort), **tray** (tray.go),
  **switcher** (view.go) — all "list with a selected row," but each renders
  "selected" slightly differently (palette `›`+style, switcher `›`+styleSel,
  tray `▎`, pickers their own).

Consistency problems:
- **"Selected" has ~4 renderings** (palette/switcher/tray/pickers) — no single
  selection component.
- **Tabs vs nav vs rail "active"** each signal differently (tab=rose bold,
  nav=lit accent, rail=rose ❯). Three "this is active" languages.
- **Empty states are terse dev-strings** ("no file changes this turn",
  "nothing waiting — all sessions idle") — functional, not delightful.

---

## 5. Typography & microcopy

Terminal = no font control, so "typography" = case / weight / spacing / wording.
- **Case:** section labels are lowercase ("navigate", "session", "sessions",
  "plan (2/3)", "choose a model", "command", "changes"). Mostly consistent
  lowercase — a deliberate, calm choice. Good. A few Title-Case leaks in app
  pages.
- **Weight:** bold for the one thing to look at; faint/dim recede. Reasonable.
- **Separators:** hint lines use `·` ("enter send · ctrl+i newline · / commands
  · ↑↓ history · ctrl+c quit"). Consistent. But hint lines are **long and
  dense** — they list every shortcut inline rather than progressive disclosure.
- **Voice/tone:** terse, lowercase, technical ("no file changes this turn").
  Honest and calm, but reads "developer utility," not "crafted product." There's
  no warmth or polish in the microcopy except the time-of-day greeting.
- **The greeting** (art.go) is the one piece of "voice" — good instinct, lonely.

---

## 6. Motion & feedback

- **Breathing-λ loader** (brand.go): 6-frame brightness cycle + synced dot.
  Tasteful. The signature motion.
- **Rail working spinner** (railSpinnerFrames, braille), tick 1.2s→300ms when busy.
- **bubbletea spinner.MiniDot** (tui.go) — a second spinner idiom alongside the λ.
- **App liveGlyph pulse** (WorkingRamp, ~1.2s poll) — a third "working" animation.
- **Flash pill** (showFlash, auto-clear) — the main action feedback; tonal
  (ok/warn/bad). Good.
- **Terminal-tab title dots** (ping.go, wall-clock) — "λ eigen working…".
- **Turn-done:** bell + notifier + flash on long turns.

Designer view:
- **Three different "working" animations** (λ breath, rail braille, app pulse,
  + MiniDot) — no single motion signature.
- **Most discrete actions have NO feedback** beyond a transcript note or nothing
  (toggles flash now; but navigation, selection, tab switches just snap).
- **No transitions** — everything snaps (panel open/close, page switch, attach).
  A little easing/settle would read premium. (Terminal limits this, but reveal
  cadence + a settle frame are possible.)
- No "press" affordance on clickable chrome (it just acts).

---

## 7. Content rendering (the transcript)

(From the one fleet report that survived + source.)
- **User** = `❯ ` + body, whole thing **Title cyan bold** (blocks.go:291). Reads
  like a command, very distinct.
- **Assistant** = `renderProse` markdown (blocks.go:378), no speaker label.
- **Tool** = left-lane gutter `▏` (violet) + `▸/▾` + status glyph + icon +
  summary + `(+N −M)`; collapsible.
- **Markdown supported:** ATX headings (═/─ underlines), ``` fences (╭─ frame,
  NO right/bottom border, NO bg, NO syntax highlight), `code` (teal, no chip),
  **bold**, *italic*, links (text only, underlined — **URL dropped**),
  `> quotes` (▏ italic dim), `-/*/+` bullets (→•), `1.` ordered.
- **NOT supported:** setext headings, `~~strike~~`, `__bold__`/`_italic_`, task
  lists `- [ ]`, tables, images, autolinks, nested emphasis, `1)` lists.

Designer view:
- **The transcript reads like a styled log, not a document.** Code blocks have no
  real frame (open-sided ╭─) and no syntax color → code looks like teal prose.
  Diffs are functional. No table support (models emit tables often → they fall
  apart). Links drop their URL (clean but lossy).
- **Speaker distinction** leans entirely on "user = cyan bold / assistant =
  default prose." There's no avatar/lane/timestamp/rhythm — long transcripts
  blur together.

---

## 8. App shell (the dashboard)

Pages: home, sessions, machines, projects, config, skills, models, providers,
memory, crons, plugins, live. Composition: title bar (`eigen › page`) + bordered
rail + content + (wide) inspector + status bar. Rows: selected = rose bold
(post-Tier-22), live/working = pulsing λ.

Designer view (user already flagged home emptiness):
- **Home is sparse** — big EIGEN wordmark + greeting + a short feed leave acres
  of blank rows; it echoes rather than invites (this is Tier 21).
- **The wide inspector is a placeholder** — the third column at ≥130 cols is
  mostly empty.
- **App shell vs chat chrome don't fully feel like one product** — the app shell
  has a top title bar + bordered rail; the chat has the headerless left sidebar.
  Different chrome models for the same product.
- Pages are **information-sparse, list-heavy** — they present data, they don't
  compose a "command center" feel.

---

## TOP design problems → what to redesign from zero

Ranked by how much each makes eigen feel like a dev tool rather than a
high-taste luxury super-app:

1. ~~**Emoji tool icons** (📖🔍🌐) amid monochrome line-art~~ — DONE: one
   coherent monochrome/Nerd-Font icon set in theme.ToolIcon (no emoji).
2. ~~**No surfaces / elevation / tonal depth**~~ — DONE: base→surface→overlay
   elevation (deep-teal palette); rail/panels on Surface, transcript on Base,
   selection on Overlay; the whole View is painted (no terminal-bg leak).
3. ~~**Glyph overload & duplication**~~ — DONE: one vocabulary in theme/icons.go
   (theme.Ellipsis ⋯, CollapseAll/ExpandAll ⊟/⊞, Status*, Caret); the `eigen
   theme` swatch is the documentation. Each symbol = one meaning.
4. ~~**No single "selection / active" component**~~ — DONE: selectLine() renders
   ONE selection (clay ▎ bar) across palette/pickers/switcher/tray/app lists;
   "active" (rail page, right-panel tab, active session) is all clay Focus, never
   brand blue. styleAsk is back to meaning only "approval".
5. **Transcript reads like a document** — MOSTLY DONE: framed code blocks with
   real chroma syntax highlighting (distinct hues), markdown tables, headings
   with rules, blockquotes, composed turn rhythm. REMAINING: finer speaker
   rhythm / reading polish if wanted.
6. **Spacing is uniform-1, not composed** — no deliberate spacing scale, tight
   gutters, no "air" around key moments. Premium = rhythm + breathing room.
7. **Three+ "working" motions, snapping transitions, missing feedback** — one
   motion signature; tasteful reveals/settles; acknowledge every action.
8. **Microcopy is terse dev-speak** — calm but cold. A crafted, consistent voice
   (still minimal) lifts the whole thing.
9. **App-shell ≠ chat chrome** — PARTLY DONE: home section headers now use the
   sidebar's hairline-divider treatment (sectionLabel), and selection/active read
   identically across both. REMAINING: sparse home density + empty wide inspector.
10. **Code/diff aesthetics** — the thing developers stare at most; deserves the
    most craft (real framing, subtle syntax tint, beautiful diffs).

This is the slate. Next: web-research high-taste references (typographic
restraint, terminal art, color/elevation systems), then design the new system
from zero — principles first, then tokens (color incl. surfaces, spacing scale,
glyph set, motion), then components, then roll it across theme/tui/app.

**Progress (2026-06-14):** #1 icons, #2 surfaces/elevation, #3 glyph vocabulary,
#4 selection+active all DONE; #5 transcript-as-document and #9 app↔chat cohesion
MOSTLY done. REMAINING: #6 spacing scale (composed rhythm), #7 motion signature
(reveals/settles, fewer working animations), #8 microcopy voice (warm empty
states), #9 tail (home density + wide inspector), #10 diff aesthetics polish.
