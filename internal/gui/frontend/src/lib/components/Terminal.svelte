<script lang="ts">
  // A live interactive terminal. The PTY lives server-side (the GUI runs on the
  // host); this is the xterm.js front-end of it. On mount we start a PTY via
  // TerminalStart(cols,rows), pipe the user's keystrokes to TerminalWrite, ride
  // the eigen:terminal event for output (base64 raw bytes → xterm), and on the
  // container resizing call TerminalResize. On destroy we Kill the PTY so its
  // shell + goroutines never outlive the panel. xterm + addon-fit only — no
  // VT logic here; xterm IS the emulator.
  import { onMount } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { Bridge } from "$lib/bridge";
  import { on, ev } from "$lib/events";
  import type { TerminalEventDTO } from "$lib/types";

  // active gates the eager start: the parent only mounts/keeps this when the
  // Terminal tab is the visible one, but expose a prop so a hidden instance
  // doesn't spawn a shell. Default true (mounted = wanted).
  let { active = true }: { active?: boolean } = $props();

  let host = $state<HTMLDivElement | undefined>(undefined);

  // base64 → bytes → string for xterm.write. The PTY emits raw bytes (incl.
  // UTF-8 multibyte + control sequences); decode without mangling.
  const dec = new TextDecoder();
  function b64ToText(b64: string): string {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return dec.decode(bytes);
  }

  onMount(() => {
    if (!host || !active) return;
    const term = new Terminal({
      fontFamily: "var(--font-mono, monospace)",
      fontSize: 13,
      cursorBlink: true,
      // Match the app's dark surface so the terminal isn't a bright rectangle.
      theme: {
        background: "#0d1117",
        foreground: "#d6deeb",
        cursor: "#69c2b8",
        selectionBackground: "#2d3b44",
      },
      allowProposedApi: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    let id = "";
    let disposed = false;
    let off: (() => void) | null = null;

    // Start the server PTY at the fitted size, then wire output + input.
    Bridge.TerminalStart(term.cols, term.rows)
      .then((newId) => {
        if (disposed) {
          // Raced a destroy before start resolved — kill the orphan PTY.
          if (newId) Bridge.TerminalKill(newId).catch(() => {});
          return;
        }
        id = newId;
        // Output: ride eigen:terminal, filtering to OUR id.
        off = on<TerminalEventDTO>(ev.terminal, (e) => {
          if (e.id !== id) return;
          if (e.data) term.write(b64ToText(e.data));
          if (e.exited) term.write("\r\n\x1b[2m[process exited]\x1b[0m\r\n");
        });
        // Input: pipe keystrokes straight to the PTY.
        term.onData((d) => {
          if (id) Bridge.TerminalWrite(id, d).catch(() => {});
        });
      })
      .catch((err) => {
        term.write(`\r\n\x1b[31mterminal unavailable: ${err}\x1b[0m\r\n`);
      });

    // Keep the PTY sized to the container.
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
        if (id) Bridge.TerminalResize(id, term.cols, term.rows).catch(() => {});
      } catch {
        /* fit before open can throw; ignore */
      }
    });
    ro.observe(host);

    return () => {
      disposed = true;
      ro.disconnect();
      off?.();
      if (id) Bridge.TerminalKill(id).catch(() => {});
      term.dispose();
    };
  });
</script>

<div class="term" bind:this={host}></div>

<style>
  .term {
    width: 100%;
    height: 100%;
    min-height: 0;
    background: #0d1117;
    padding: var(--sp-3);
    box-sizing: border-box;
    overflow: hidden;
  }
  /* xterm sizes its own canvas; just ensure the viewport fills the panel. */
  .term :global(.xterm) {
    height: 100%;
  }
  .term :global(.xterm-viewport) {
    overflow-y: auto;
  }
</style>
