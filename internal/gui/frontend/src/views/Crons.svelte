<script lang="ts">
  // Crons — scheduled work around eigen: systemd --user timers and the user's
  // crontab. Timers show next/last run + active state and can be
  // started/stopped/enabled/disabled; crontab entries are read-only (their
  // spec + command). Read directly via the bridge (systemctl/crontab).
  //
  // Taste pass: read as a SCHEDULE, not a list. Timers are ordered by next run
  // (soonest first, no-next-run sinks to the bottom) and carry a relative
  // "next in 2h" lede plus a 24h day-position track. The single soonest live
  // run is the lead — teal-lit so the eye lands on what fires next. Crontab
  // entries decode their spec into a human cadence. All bridge calls, controls,
  // badges and empty states are unchanged.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { CronsDTO, CronDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<CronsDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let acting = $state<Record<string, boolean>>({});

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.Crons();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      loadSeq++;
    };
  });

  // --- Time-axis decoding (read-only, from the bridge's pre-formatted strings).
  // Timer `next`/`last` are "—", "today HH:MM", or "YYYY-MM-DD HH:MM".
  // We re-derive a sortable timestamp + a relative lede + 24h day position so
  // the section reads as a schedule rather than stacked cards.

  // A live clock that ticks once a minute keeps "next in …" honest without
  // re-hitting the bridge. Cleanup clears the interval (leak contract).
  let now = $state(Date.now());
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 60_000);
    return () => clearInterval(id);
  });

  function parseWhen(s: string): number | null {
    if (!s || s === "—") return null;
    const today = s.startsWith("today ");
    const body = today ? s.slice(6) : s;
    let datePart: string, timePart: string;
    if (today) {
      const d = new Date();
      datePart = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      timePart = body.trim();
    } else {
      const sp = body.indexOf(" ");
      if (sp < 0) return null;
      datePart = body.slice(0, sp);
      timePart = body.slice(sp + 1).trim();
    }
    const [y, mo, da] = datePart.split("-").map(Number);
    const [h, mi] = timePart.split(":").map(Number);
    if ([y, mo, da, h, mi].some((n) => Number.isNaN(n))) return null;
    return new Date(y, mo - 1, da, h, mi).getTime();
  }

  // Compact relative distance, e.g. "in 2h", "in 3d", "in 12m", "now".
  function relative(ts: number, ref: number): string {
    const ms = ts - ref;
    if (ms <= 0) return "due";
    const m = Math.round(ms / 60_000);
    if (m < 1) return "now";
    if (m < 60) return `in ${m}m`;
    const h = Math.round(m / 60);
    if (h < 24) return `in ${h}h`;
    const d = Math.round(h / 24);
    if (d < 14) return `in ${d}d`;
    return `in ${Math.round(d / 7)}w`;
  }

  // Position within the 24h day (0..1) for the day-position track. null when the
  // run isn't anchored to a clock time we can read.
  function dayPos(ts: number | null): number | null {
    if (ts === null) return null;
    const d = new Date(ts);
    return (d.getHours() * 60 + d.getMinutes()) / 1440;
  }

  type TimerRow = CronDTO & { ts: number | null; rel: string; pos: number | null };

  const timers = $derived.by<TimerRow[]>(() => {
    const ref = now;
    const rows: TimerRow[] = (data?.crons ?? [])
      .filter((c) => c.kind === "timer")
      .map((c) => {
        const ts = parseWhen(c.next);
        return { ...c, ts, rel: ts !== null ? relative(ts, ref) : "", pos: dayPos(ts) };
      });
    // Soonest first; no-next-run (ts null) sinks to the bottom, then by name.
    rows.sort((a, b) => {
      if (a.ts === null && b.ts === null) return a.name.localeCompare(b.name);
      if (a.ts === null) return 1;
      if (b.ts === null) return -1;
      return a.ts - b.ts;
    });
    return rows;
  });

  // Lead row: the soonest run that will actually fire — running (active) with a
  // future next. teal=alive, so a stopped timer (even one with a future next)
  // is never the lead. timers is already sorted soonest-first.
  const leadUnit = $derived(timers.find((t) => t.ts !== null && t.active)?.unit ?? null);

  // Crontab specs → a human cadence, so the column reads as a rhythm not a glob.
  const DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  function cadence(spec: string): string {
    const s = spec.trim();
    const named: Record<string, string> = {
      "@hourly": "hourly",
      "@daily": "daily",
      "@midnight": "daily at midnight",
      "@weekly": "weekly",
      "@monthly": "monthly",
      "@yearly": "yearly",
      "@annually": "yearly",
      "@reboot": "on reboot",
    };
    if (named[s]) return named[s];
    const f = s.split(/\s+/);
    if (f.length !== 5) return s;
    const [mi, h, dom, mon, dow] = f;
    const hm = (hh: string, mm: string) => `${hh.padStart(2, "0")}:${mm.padStart(2, "0")}`;
    if (mi !== "*" && h === "*" && dom === "*" && mon === "*" && dow === "*")
      return `every hour at :${mi.padStart(2, "0")}`;
    if (h.startsWith("*/") && mi === "0" && dom === "*" && dow === "*")
      return `every ${h.slice(2)}h`;
    if (mi.startsWith("*/") && h === "*" && dom === "*" && dow === "*")
      return `every ${mi.slice(2)}m`;
    if (mi !== "*" && h !== "*" && dom === "*" && mon === "*" && dow === "*")
      return `daily at ${hm(h, mi)}`;
    if (mi !== "*" && h !== "*" && dow !== "*" && dom === "*" && mon === "*") {
      const d = Number(dow);
      const day = Number.isInteger(d) && d >= 0 && d <= 6 ? DOW[d % 7] : dow;
      return `${day} at ${hm(h, mi)}`;
    }
    if (mi !== "*" && h !== "*" && dom !== "*" && dow === "*")
      return `day ${dom} at ${hm(h, mi)}`;
    if (mi === "*" && h === "*") return "every minute";
    return s;
  }
  type CronRow = CronDTO & { cadence: string };
  const crontab = $derived<CronRow[]>(
    (data?.crons ?? []).filter((c) => c.kind === "crontab").map((c) => ({ ...c, cadence: cadence(c.next) })),
  );

  async function ctl(c: CronDTO, verb: string) {
    if (!c.unit) return;
    acting[c.unit] = true;
    try {
      await Bridge.SetTimer(c.unit, verb);
      toasts.success(`${verb} ${c.name}`);
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      if (c.unit) delete acting[c.unit];
    }
  }
</script>

<div class="crons">
  {#if loading && !data}
    <div class="crons__pad">
      {#each Array(4) as _, i (i)}<div class="crons__skel"></div>{/each}
    </div>
  {:else if error && !data}
    <EmptyState glyph="◷" title="Couldn't load scheduled work" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if !data || (data.timers === 0 && data.crontab === 0)}
    <EmptyState
      glyph="◷"
      title="No scheduled work"
      line={data && !data.systemdAvail
        ? "No systemd --user timers or crontab found on this machine."
        : "No systemd --user timers or crontab entries. Set one up to automate eigen runs."}
    />
  {:else}
    <div class="crons__scroll">
      {#if timers.length > 0}
        <section class="crons__section">
          <div class="crons__section-head">
            <span class="crons__rail" aria-hidden="true"></span>
            <h2 class="crons__section-title">Systemd timers</h2>
            <span class="crons__n tnum">{timers.length}</span>
            <span class="crons__by">by next run</span>
          </div>
          <div class="crons__list">
            {#each timers as c (c.unit)}
              {@const lead = c.unit === leadUnit}
              <Card>
                <div class="cron" class:cron--lead={lead}>
                  {#if lead}<span class="cron__edge" aria-hidden="true"></span>{/if}
                  <div class="cron__when">
                    {#if c.ts !== null}
                      <span class="cron__rel" class:cron__rel--lead={lead}>{c.rel}</span>
                      <span class="cron__at tnum">{c.next.replace(/^today /, "")}</span>
                    {:else}
                      <span class="cron__rel cron__rel--off">—</span>
                      <span class="cron__at cron__at--off">idle</span>
                    {/if}
                  </div>
                  <div class="cron__main">
                    <div class="cron__top">
                      <StatusDot state={c.active ? "ok" : "idle"} size={7} pulse={lead} />
                      <span class="cron__name">{c.name}</span>
                      <Badge tone={c.active ? "success" : "neutral"}>{c.active ? "running" : "stopped"}</Badge>
                      <Badge tone={c.enabled ? "info" : "neutral"}>{c.enabled ? "enabled" : "disabled"}</Badge>
                    </div>
                    <div class="cron__cmd selectable">{c.command}</div>
                    {#if c.pos !== null || (c.last && c.last !== "—")}
                      <div class="cron__track-row">
                        <div class="cron__track" title="position in a 24-hour day">
                          {#if c.pos !== null}
                            <span class="cron__tick" class:cron__tick--lead={lead} style="left: {c.pos * 100}%"></span>
                          {/if}
                        </div>
                        {#if c.last && c.last !== "—"}
                          <span class="cron__last"><span class="cron__last-l">last</span> {c.last}</span>
                        {/if}
                      </div>
                    {/if}
                  </div>
                  <div class="cron__actions">
                    {#if c.active}
                      <Button variant="ghost" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "stop")}>Stop</Button>
                    {:else}
                      <Button variant="primary" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "start")}>Start</Button>
                    {/if}
                    {#if c.enabled}
                      <Button variant="ghost" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "disable")}>Disable</Button>
                    {:else}
                      <Button variant="ghost" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "enable")}>Enable</Button>
                    {/if}
                  </div>
                </div>
              </Card>
            {/each}
          </div>
        </section>
      {/if}

      {#if crontab.length > 0}
        <section class="crons__section">
          <div class="crons__section-head">
            <span class="crons__rail crons__rail--alt" aria-hidden="true"></span>
            <h2 class="crons__section-title">Crontab</h2>
            <span class="crons__n tnum">{crontab.length}</span>
            <span class="crons__by">read-only</span>
          </div>
          <div class="crons__list">
            {#each crontab as c, i (i)}
              <Card>
                <div class="cron cron--tab">
                  <div class="cron__cadence">
                    <span class="cron__cadence-v">{c.cadence}</span>
                    <span class="cron__spec selectable">{c.next}</span>
                  </div>
                  <div class="cron__main">
                    <div class="cron__cmd selectable">{c.command}</div>
                  </div>
                </div>
              </Card>
            {/each}
          </div>
        </section>
      {/if}
    </div>
  {/if}
</div>

<style>
  .crons {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .crons__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-8);
  }
  .crons__pad {
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .crons__skel {
    height: 88px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: cron-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes cron-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .crons__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .crons__section-head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  /* A short accent rail anchors each section to a time-axis feel. */
  .crons__rail {
    width: 3px;
    height: 13px;
    border-radius: var(--r-full);
    background: var(--brand);
    box-shadow: 0 0 8px rgba(105, 194, 184, 0.4);
  }
  .crons__rail--alt {
    background: var(--text-faint);
    box-shadow: none;
  }
  .crons__section-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .crons__n {
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  .crons__by {
    margin-left: auto;
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .crons__list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }

  /* --- Timer row: time gutter | body | controls. */
  .cron {
    position: relative;
    display: flex;
    gap: var(--sp-5);
    padding: var(--sp-5);
  }
  /* Fixed-width time gutter so next-run reads as an axis down the left edge. */
  .cron__when {
    flex: none;
    width: 78px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding-top: 1px;
  }
  .cron__rel {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-secondary);
    letter-spacing: var(--ls-heading);
  }
  .cron__rel--lead {
    color: var(--brand-bright);
  }
  .cron__rel--off {
    color: var(--text-faint);
  }
  .cron__at {
    font-size: var(--fs-micro);
    color: var(--text-ghost);
    letter-spacing: var(--ls-eyebrow);
  }
  .cron__at--off {
    text-transform: uppercase;
  }
  .cron__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .cron__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .cron__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .cron__cmd {
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-snug) var(--font-mono);
    color: var(--text-secondary);
    word-break: break-all;
  }
  .cron__track-row {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
  }
  /* 24h day-position track: a hairline rail with a tick where the run lands. */
  .cron__track {
    position: relative;
    flex: 1;
    height: 3px;
    border-radius: var(--r-full);
    background: linear-gradient(90deg, var(--bg-overlay), var(--bg-overlay-2));
    min-width: 80px;
  }
  .cron__tick {
    position: absolute;
    top: 50%;
    width: 6px;
    height: 6px;
    border-radius: var(--r-full);
    background: var(--text-muted);
    transform: translate(-50%, -50%);
  }
  .cron__tick--lead {
    background: var(--brand);
    box-shadow: var(--glow-live);
  }
  .cron__last {
    flex: none;
    font-size: var(--fs-micro);
    color: var(--text-ghost);
  }
  .cron__last-l {
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    margin-right: var(--sp-2);
  }
  .cron__actions {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    align-items: flex-end;
  }

  /* --- Lead row: the soonest live run is teal-lit so the eye lands on it. */
  .cron--lead {
    background: linear-gradient(90deg, rgba(105, 194, 184, 0.07), transparent 62%);
    border-radius: var(--r-md);
  }
  .cron__edge {
    position: absolute;
    left: 0;
    top: var(--sp-4);
    bottom: var(--sp-4);
    width: 2px;
    border-radius: var(--r-full);
    background: var(--edge-live);
    animation: cron-breath var(--breath) var(--ease-inout) infinite;
  }
  @keyframes cron-breath {
    0%,
    100% {
      opacity: 0.55;
    }
    50% {
      opacity: 1;
    }
  }

  /* --- Crontab row: cadence column reads as the rhythm; spec stays verbatim. */
  .cron--tab {
    align-items: baseline;
  }
  .cron__cadence {
    flex: none;
    width: 168px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .cron__cadence-v {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .cron__spec {
    font: var(--fw-medium) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--brand-bright);
    background: var(--bg-overlay);
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--r-xs);
    align-self: flex-start;
  }

  @media (prefers-reduced-motion: reduce) {
    .crons__skel,
    .cron__edge {
      animation: none;
    }
  }
</style>
