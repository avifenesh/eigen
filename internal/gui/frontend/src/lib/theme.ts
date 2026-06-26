// GUI theme application. The `theme` config key (deepteal | nord | gruvbox)
// is stored by the daemon and also drives the TUI (internal/theme); the GUI
// mirrors it by setting <html data-theme=...>, which selects the matching token
// block in styles/tokens.css. deepteal is the :root default, so it needs no
// attribute. Called once on load (App.svelte, from Config().theme) and again on
// a live change in the Config view, so the palette swaps without a restart.

const KNOWN = new Set(["deepteal", "nord", "gruvbox"]);

export function applyTheme(name: string | undefined | null): void {
  const root = document.documentElement;
  // Guard non-string config values + match case-insensitively.
  const t = (typeof name === "string" ? name : "").trim().toLowerCase();
  // deepteal / unknown / empty → the :root default: clear the attribute.
  if (!t || t === "deepteal" || !KNOWN.has(t)) {
    root.removeAttribute("data-theme");
    return;
  }
  root.setAttribute("data-theme", t);
}
