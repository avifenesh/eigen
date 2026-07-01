<script lang="ts">
  // Crons — scheduled work around eigen: systemd --user timers and the user's
  // crontab. Timers show next/last run + active state and can be
  // started/stopped/enabled/disabled; crontab jobs can be ADDED (with friendly
  // presets) and removed. Read + written via the bridge (systemctl/crontab).
  //
  // Taste pass: read as a SCHEDULE, not a list. Timers are ordered by next run
  // (soonest first, no-next-run sinks to the bottom) and carry a relative
  // "next in 2h" lede plus a 24h day-position track. The single soonest live
  // run is the lead — teal-lit so the eye lands on what fires next. Crontab
  // entries decode their spec into a human cadence. All bridge calls, controls,
  // badges and empty states are unchanged.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { viewCache } from "$lib/stores/viewCache.svelte";
  import type { CronsDTO, CronDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  const CACHE_KEY = "crons";
  let data = $state<CronsDTO | null>(viewCache.get<CronsDTO>(CACHE_KEY) ?? null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let acting = $state<Record<string, boolean>>({});
  let confirmRemove = $state<Record<string, boolean>>({});

  // The router fully unmounts/remounts this view on every nav switch (no
  // keep-alive), so without a cache every revisit re-fetches from zero —
  // Bridge.Crons() fans out into systemctl subprocess spawns underneath, the
  // single most expensive bridge call in the app. viewCache lets a revisit
  // paint the previous result instantly while this refresh runs behind it.
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await viewCache.fetch(CACHE_KEY, () => Bridge.Crons());
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = errText(e);
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
  // Returns null when the spec can't be decoded — the caller then labels it
  // "(custom schedule)" above the verbatim spec instead of echoing the glob twice.
  const DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  // Render a day-of-week field that may be a single day (1), a range (1-5),
  // or a comma list (1,3,5) as readable day names; null if it isn't day-shaped.
  function dowPhrase(dow: string): string | null {
    const named = (d: number) => (d >= 0 && d <= 7 ? DOW[d % 7] : null);
    const rng = dow.match(/^(\d)-(\d)$/);
    if (rng) {
      const a = named(Number(rng[1])),
        b = named(Number(rng[2]));
      return a && b ? `${a}–${b}` : null;
    }
    if (/^\d(,\d)+$/.test(dow)) {
      const days = dow.split(",").map((d) => named(Number(d)));
      return days.every((d) => d) ? days.join(", ") : null;
    }
    const d = Number(dow);
    return Number.isInteger(d) ? named(d) : null;
  }
  function cadence(spec: string): string | null {
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
    if (f.length !== 5) return null;
    const [mi, h, dom, mon, dow] = f;
    const hm = (hh: string, mm: string) => `${hh.padStart(2, "0")}:${mm.padStart(2, "0")}`;
    if (mi !== "*" && h === "*" && dom === "*" && mon === "*" && dow === "*")
      return `every hour at :${mi.padStart(2, "0")}`;
    if (h.startsWith("*/") && mi === "0" && dom === "*" && dow === "*")
      return `every ${h.slice(2)}h`;
    if (mi.startsWith("*/") && h === "*" && dom === "*" && dow === "*")
      return `every ${mi.slice(2)}m`;
    // Step minutes within an hour-range on weekdays/days: "*/15 9-17 * * 1-5".
    if (mi.startsWith("*/") && /^\d+-\d+$/.test(h) && dom === "*" && mon === "*") {
      const days = dow === "*" ? "" : (() => {
        const p = dowPhrase(dow);
        return p ? ` on ${p}` : null;
      })();
      if (days !== null) return `every ${mi.slice(2)}m, ${h}h${days}`;
    }
    if (mi !== "*" && h !== "*" && dom === "*" && mon === "*" && dow === "*")
      return `daily at ${hm(h, mi)}`;
    if (mi !== "*" && h !== "*" && dow !== "*" && dom === "*" && mon === "*") {
      const day = dowPhrase(dow);
      if (day) return `${day} at ${hm(h, mi)}`;
    }
    if (mi !== "*" && h !== "*" && dom !== "*" && dow === "*")
      return `day ${dom} at ${hm(h, mi)}`;
    if (mi === "*" && h === "*") return "every minute";
    return null;
  }
  type CronRow = CronDTO & { cadence: string | null };
  const crontab = $derived<CronRow[]>(
    (data?.crons ?? []).filter((c) => c.kind === "crontab").map((c) => ({ ...c, cadence: cadence(c.next) })),
  );

  async function ctl(c: CronDTO, verb: string) {
    if (!c.unit) return;
    acting[c.unit] = true;
    try {
      await Bridge.SetTimer(c.unit, verb);
      toasts.success(`${verb} ${c.name}`);
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      if (c.unit) delete acting[c.unit];
    }
  }

  // --- Add / remove crontab jobs (the writable half).
  let addOpen = $state(false);
  let addSpec = $state("");
  let addCmd = $state("");
  let addSaving = $state(false);
  // A few friendly presets so the user doesn't have to recall cron syntax.
  const specPresets = [
    { label: "Every hour", spec: "@hourly" },
    { label: "Every day 9am", spec: "0 9 * * *" },
    { label: "Weekdays 9am", spec: "0 9 * * 1-5" },
    { label: "Weekly (Mon)", spec: "0 9 * * 1" },
  ];

  async function addJob() {
    const spec = addSpec.trim();
    const cmd = addCmd.trim();
    if (!spec || !cmd) {
      toasts.error("Schedule and command are required");
      return;
    }
    addSaving = true;
    try {
      await Bridge.AddCrontab(spec, cmd);
      toasts.success("Scheduled job added");
      addOpen = false;
      addSpec = addCmd = "";
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      addSaving = false;
    }
  }

  async function removeJob(c: CronRow) {
    const key = c.next + c.command;
    acting[key] = true;
    delete confirmRemove[key];
    try {
      await Bridge.RemoveCrontab(c.next, c.command);
      toasts.success("Removed");
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting[key];
    }
  }
</script>

<div class="crons">
  {#if loading && !data}
    <div class="crons__pad">
      <Skeleton count={4} height="88px" gap="var(--sp-5)" />
    </div>
  {:else if error && !data}
    <EmptyState glyph="◷" title="Couldn't load scheduled work" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <div class="crons__scroll">
      <!-- Add a scheduled job (crontab) — the writable entry point. -->
      <div class="crons__addbar">
        {#if !addOpen}
          <Button variant="primary" size="sm" onclick={() => (addOpen = true)}>+ Schedule a job</Button>
        {/if}
      </div>
      {#if addOpen}
        <Card>
          <div class="addjob">
            <div class="addjob__row">
              <label class="addjob__lbl" for="cron-spec">Schedule</label>
              <input id="cron-spec" class="addjob__in" bind:value={addSpec} placeholder="@daily  ·  or  0 9 * * 1-5" />
            </div>
            <div class="addjob__presets">
              {#each specPresets as p (p.spec)}
                <button class="preset" onclick={() => (addSpec = p.spec)}>{p.label}</button>
              {/each}
            </div>
            <div class="addjob__row">
              <label class="addjob__lbl" for="cron-cmd">Command</label>
              <input id="cron-cmd" class="addjob__in" bind:value={addCmd} placeholder="eigen run … / notify-send 'standup'" />
            </div>
            <div class="addjob__btns">
              <Button variant="primary" disabled={addSaving} onclick={addJob}>{addSaving ? "Adding…" : "Add job"}</Button>
              <Button variant="ghost" disabled={addSaving} onclick={() => (addOpen = false)}>Cancel</Button>
            </div>
          </div>
        </Card>
      {/if}

      {#if (!data || (data.timers === 0 && data.crontab === 0)) && !addOpen}
        <EmptyState
          glyph="◷"
          title="No scheduled work yet"
          line="Schedule a job above to automate eigen runs, reminders, or chores."
        />
      {/if}

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
          </div>
          <div class="crons__list">
            {#each crontab as c, i (i)}
              <Card>
                <div class="cron cron--tab">
                  <div class="cron__cadence">
                    {#if c.cadence !== null}
                      <span class="cron__cadence-v">{c.cadence}</span>
                    {:else}
                      <span class="cron__cadence-v cron__cadence-v--custom">custom schedule</span>
                    {/if}
                    <span class="cron__spec selectable">{c.next}</span>
                  </div>
                  <div class="cron__main">
                    <div class="cron__cmd selectable">{c.command}</div>
                  </div>
                  <div class="cron__actions">
                    {#if confirmRemove[c.next + c.command]}
                      <Button variant="danger" size="sm" loading={acting[c.next + c.command]} onclick={() => removeJob(c)}>Confirm</Button>
                      <Button variant="ghost" size="sm" disabled={acting[c.next + c.command]} onclick={() => delete confirmRemove[c.next + c.command]}>Cancel</Button>
                    {:else}
                      <Button variant="ghost" size="sm" onclick={() => (confirmRemove[c.next + c.command] = true)}>Remove</Button>
                    {/if}
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
    padding: var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-8);
  }
  .crons__pad {
    padding: var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
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
  /* Undecodable spec: the verbatim glob below is the truth, so the label sits
     back as a faint italic note rather than competing as a cadence. */
  .cron__cadence-v--custom {
    font-weight: var(--fw-regular);
    font-style: italic;
    color: var(--text-faint);
  }
  .cron__spec {
    font: var(--fw-medium) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--brand-bright);
    background: var(--bg-overlay);
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--r-xs);
    align-self: flex-start;
  }

  /* --- Add-job form. */
  .crons__addbar {
    display: flex;
    justify-content: flex-end;
  }
  .addjob {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-5);
  }
  .addjob__row {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .addjob__lbl {
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    color: var(--text-muted);
  }
  .addjob__in {
    height: 34px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised-2);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-code-sm) / 1 var(--font-mono);
    outline: none;
  }
  .addjob__in:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .addjob__presets {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .preset {
    height: 24px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised-2);
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .preset:hover {
    color: var(--brand-bright);
    border-color: var(--border-brand-faint);
  }
  .addjob__btns {
    display: flex;
    gap: var(--sp-2);
    margin-top: var(--sp-2);
  }

  @media (prefers-reduced-motion: reduce) {
    .cron__edge {
      animation: none;
    }
  }
</style>
