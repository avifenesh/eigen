# eigen design — references & principles for the from-scratch system

Research/synthesis to inform a luxury, high-taste TUI for eigen. North star
(user): *"high value, luxury, a joy to look at and to use — show this is a
super app."* This is the inspiration layer; the actual token/component spec
comes after we agree on direction.

---

## Reference products (what "premium terminal" looks like in 2024–25)

- **Charmbracelet** ("we make the command line glamorous") — the gold standard,
  and now ships **Crush**, an AI coding-agent TUI = our direct peer. What they
  do: a tight signature palette (their violet/charm-pink `#FF5FAF`-ish + cream
  `#FFFDF5` on near-black), **rounded borders used sparingly**, generous
  padding, **soft-serve** spring motion (Harmonica), and Glow/Glamour's
  **markdown rendering as a beautiful document** (real code blocks, styled
  headings, tasteful margins). Lesson: one confident accent, lots of breathing
  room, markdown that reads like a typeset page.
- **Catppuccin / Rosé Pine / Nord** (palette systems) — the key idea we're
  MISSING: **named elevation surfaces**. Catppuccin defines `base` → `mantle` →
  `crust` (descending) and `surface0/1/2` (ascending) plus `overlay0/1/2`. Rosé
  Pine: `base`/`surface`/`overlay`/`highlight-low/med/high`. Depth comes from
  2–3 subtle background tints, not shadows. This is how a flat fg-on-default TUI
  becomes a layered, premium one.
- **Warp** — the "luxury terminal" benchmark: blocks as cards with subtle
  surface fills + soft separators, restrained accent, real type hierarchy,
  smooth motion. Reads like a product, not a shell.
- **lazygit / k9s** — premium *information density* done right: bordered panes,
  a clear active-pane highlight, consistent status glyphs, color = meaning, fast
  legible tables. Lesson: dense ≠ cluttered when hierarchy + color are disciplined.
- **atuin / Starship / Fish** — calm, restrained prompts: muted palettes, one
  accent, faint secondary text, tasteful single glyphs. Lesson: restraint reads
  as confidence.

## The big levers (what most cheaply lifts dev-tool → luxury)

1. **Elevation surfaces.** Add `base` (default bg), `surface` (panels: rail,
   right panel, code blocks — a hair lighter than base), and `overlay` (popovers,
   palette, tray — lighter still). Suddenly regions have depth without borders
   everywhere. THE single highest-impact change.
2. **One coherent monochrome icon set — no emoji.** Replace 📖🔍🌐 with
   single-weight geometric/line glyphs so the whole UI is one material.
   Candidate families: status `● ○ ◆ ◇ ✗`, tools from a geometric set
   (`◇ read · ✎ edit · ⌕ search · ❯ shell · ⊕ run · ↗ web · ⊙ task`), nav
   `❯ ▸ ▹`, all the same visual weight. Pick ONE caret for "pointer," ONE for
   "expand."
3. **Markdown as a document, not a log.** Real framed code blocks on a `surface`
   tint with a language chip + subtle syntax tinting; proper tables; tasteful
   headings with margin; quotes/lists with rhythm. This is where users LIVE.
4. **A spacing scale + breathing room.** Adopt a small scale (e.g. gutters of 1
   between dense items, 2 between sections, generous padding inside surfaces).
   Air around the composer and key moments. Premium = proportion + space.
5. **Confident, restrained accent + tonal neutrals.** Keep the brand blue, but
   give neutrals real tiers (text / dim / faint / ghost) and let surfaces carry
   tone so color is spent only on meaning.
6. **One motion signature.** Collapse the 3 "working" animations into ONE
   (the breathing λ is the keeper); add a *settle* (a 1–2 frame ease) on reveals
   (panel open, page switch) so things don't snap. Charm's Harmonica = the model.
7. **Microcopy voice: calm, terse, warm.** Lowercase, minimal, but with a little
   craft and consistency ("nothing waiting" → "all quiet"; "no file changes this
   turn" → "no edits yet"). Progressive disclosure on hints, not a wall of keys.
8. **One selection treatment + one active treatment, everywhere.** A single
   "selected row" style (e.g. a `surface` fill + a Focus-colored leading bar)
   and a single "active" style, reused across palette/switcher/tray/pickers/rail.
9. **Hairlines + few full boxes.** Prefer a single hairline or a surface tint to
   demarcate regions; reserve full rounded boxes for true containers. Less framing
   = more premium.
10. **Signature flourishes, sparingly.** A beautiful welcome, a satisfying
    turn-done settle, a crafted empty state. One or two "wow" moments, not glitter
    everywhere.

## Concrete palette direction (dark; to refine)

A calm-rich dark base with elevation (hex families to tune in nord/our terms):
- `base` ≈ `#1b1f27` (current OnBright value — our near-black) → the canvas.
- `surface` ≈ `#222734` (a hair lifted) → rail, right panel, code blocks.
- `overlay` ≈ `#2b3140` → palette, tray, popovers, selected-row fill.
- text tiers: `text #D8DEE9` · `dim #9aa5b8` · `faint #79839a` · `ghost #5b657a`.
- one brand accent (blue, kept), Focus rose for "you are here," semantic
  Ok/Warn/Err, and code on a `surface` tint with a hint of syntax color.
Light mode mirrors with descending lightness (base lightest, surfaces darker).

## What to preserve from today
- The λ brand mark + breathing loader (signature, keep).
- The brand rule (blue = brand only) and the role-based theme architecture +
  drift guard (great foundation — we EXTEND it with surfaces, not replace it).
- The headless left-sidebar chrome direction.

## Open questions for the user (decide before building)
1. **Density vs air:** lean Warp-spacious (more padding, fewer items per screen)
   or k9s-dense (more at a glance)? Super-app suggests a confident middle.
2. **Accent identity:** keep the calm Nord blue as THE brand, or move to a more
   singular signature hue (Charm goes bold pink/violet)? Blue is "tool-y";
   a distinctive hue says "product."
3. **Light mode:** first-class, or dark-first (most luxury terminals are dark-first)?
4. **Nerd Font glyphs:** allowed (richer icon set) or pure-Unicode only (works
   everywhere)? You run ghostty w/ a Nerd Font — we could use them for you but
   should degrade.
5. **How far on motion** given ghostty + your latency tolerance.
