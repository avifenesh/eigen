<script lang="ts">
  // Crons — scheduled work around eigen: systemd --user timers and the user's
  // crontab. Timers show next/last run + active state and can be
  // started/stopped/enabled/disabled; crontab entries are read-only (their
  // spec + command). Read directly via the bridge (systemctl/crontab).
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
  let acting = $state<Record<string, boolean>>({});

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Crons();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) toasts.error(e instanceof Error ? e.message : String(e));
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

  const timers = $derived((data?.crons ?? []).filter((c) => c.kind === "timer"));
  const crontab = $derived((data?.crons ?? []).filter((c) => c.kind === "crontab"));

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
            <h2 class="crons__section-title">Systemd timers</h2>
            <span class="crons__n tnum">{timers.length}</span>
          </div>
          <div class="crons__list">
            {#each timers as c (c.unit)}
              <Card>
                <div class="cron">
                  <div class="cron__main">
                    <div class="cron__top">
                      <StatusDot state={c.active ? "ok" : "idle"} size={7} />
                      <span class="cron__name">{c.name}</span>
                      <Badge tone={c.active ? "success" : "neutral"}>{c.active ? "scheduled" : "no next run"}</Badge>
                    </div>
                    <div class="cron__cmd selectable">{c.command}</div>
                    <div class="cron__times">
                      <span class="cron__time"><span class="cron__time-l">next</span> {c.next}</span>
                      <span class="cron__time"><span class="cron__time-l">last</span> {c.last}</span>
                    </div>
                  </div>
                  <div class="cron__actions">
                    {#if c.active}
                      <Button variant="ghost" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "stop")}>Stop</Button>
                      <Button variant="ghost" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "disable")}>Disable</Button>
                    {:else}
                      <Button variant="primary" size="sm" loading={acting[c.unit ?? ""]} onclick={() => ctl(c, "start")}>Start</Button>
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
            <h2 class="crons__section-title">Crontab</h2>
            <span class="crons__n tnum">{crontab.length}</span>
          </div>
          <div class="crons__list">
            {#each crontab as c, i (i)}
              <Card>
                <div class="cron">
                  <div class="cron__main">
                    <div class="cron__top">
                      <span class="cron__spec">{c.next}</span>
                    </div>
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
  .crons__list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .cron {
    display: flex;
    gap: var(--sp-5);
    padding: var(--sp-5);
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
  .cron__spec {
    font: var(--fw-medium) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--brand-bright);
    background: var(--bg-overlay);
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--r-xs);
  }
  .cron__cmd {
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-snug) var(--font-mono);
    color: var(--text-secondary);
    word-break: break-all;
  }
  .cron__times {
    display: flex;
    gap: var(--sp-6);
  }
  .cron__time {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .cron__time-l {
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    font-size: var(--fs-micro);
    margin-right: var(--sp-2);
  }
  .cron__actions {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    align-items: flex-end;
  }
  @media (prefers-reduced-motion: reduce) {
    .crons__skel {
      animation: none;
    }
  }
</style>
