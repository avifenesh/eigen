<script lang="ts">
  // Per-rule model fallback-chain editor. One row per role (primary, explore,
  // research, general, code, dreamer, judge); each row is an ORDERED chain of
  // models tried in turn, each falling through to the next on a quota/billing
  // failure until one answers. The user reorders (←/→), removes, and appends
  // models; an empty chain reverts the role to its built-in default. Every edit
  // persists through Bridge.SetRuleChain and re-reads the stored chain.
  //
  // Uses the custom Dropdown (not a native <select>: webkit2gtk paints the
  // option list black-on-black) for the add-model picker.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { RuleChainsDTO, RuleChainDTO } from "$lib/types";
  import Card from "./Card.svelte";
  import Dropdown from "./Dropdown.svelte";

  let data = $state<RuleChainsDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let saving = $state<Record<string, boolean>>({});

  let alive = true;
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.RuleChains();
      if (alive && seq === loadSeq && d) data = d;
    } catch (e) {
      if (alive && seq === loadSeq) error = errText(e);
    } finally {
      if (alive && seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      alive = false;
      loadSeq++;
    };
  });

  // Persist a role's chain, then fold the stored result back in (so a clear that
  // reverts to default shows the default, and Custom flips correctly).
  async function save(role: string, chain: string[]) {
    saving[role] = true;
    try {
      const stored = await Bridge.SetRuleChain(role, chain);
      if (!alive) return;
      if (data) {
        const r = data.roles.find((x) => x.role === role);
        if (r) {
          r.chain = stored;
          // Custom = the user set a non-default chain. A clear (empty request)
          // reverts to default → not custom; any explicit set → custom.
          r.custom = chain.length > 0;
        }
      }
      toasts.success(`${role} chain saved`);
    } catch (e) {
      if (!alive) return;
      toasts.error(errText(e));
      load(); // reload the authoritative state on failure
    } finally {
      if (alive) delete saving[role];
    }
  }

  function move(r: RuleChainDTO, i: number, delta: number) {
    const j = i + delta;
    if (j < 0 || j >= r.chain.length) return;
    const next = [...r.chain];
    [next[i], next[j]] = [next[j], next[i]];
    save(r.role, next);
  }
  function remove(r: RuleChainDTO, i: number) {
    const next = r.chain.filter((_, k) => k !== i);
    save(r.role, next);
  }
  function add(r: RuleChainDTO, model: string) {
    if (!model) return;
    // Allow a model to repeat? No — a chain visits each link once; dedupe.
    if (r.chain.includes(model)) {
      toasts.error(`${model} is already in the ${r.role} chain`);
      return;
    }
    save(r.role, [...r.chain, model]);
  }
  function reset(r: RuleChainDTO) {
    save(r.role, []); // empty → revert to built-in default
  }

  // Picker options: the model choices the bridge offers, minus what's already in
  // this row's chain.
  function addOptions(r: RuleChainDTO) {
    const inChain = new Set(r.chain);
    return (data?.models ?? [])
      .filter((m) => !inChain.has(m))
      .map((m) => ({ value: m, label: m }));
  }
</script>

<div class="rc">
  <div class="rc__head">
    <h3 class="rc__title">Model fallback chains</h3>
    <p class="rc__sub">
      Each role tries its models in order — the first reachable one answers, and a
      quota/billing failure falls through to the next (the drained provider is
      frozen for the day). If the whole chain is exhausted, the request fails.
    </p>
  </div>

  {#if loading && !data}
    <div class="rc__skel-wrap">
      {#each Array(4) as _, i (i)}<div class="rc__skel"></div>{/each}
    </div>
  {:else if error && !data}
    <Card><div class="rc__error">Couldn't load chains: {error}</div></Card>
  {:else if data}
    <div class="rc__list">
      {#each data.roles as r (r.role)}
        <Card>
          <div class="row">
            <div class="row__info">
              <div class="row__name">
                <span class="row__role">{r.role}</span>
                {#if r.custom}
                  <span class="row__badge" title="customized — differs from the built-in default">custom</span>
                {:else}
                  <span class="row__badge row__badge--default" title="built-in default">default</span>
                {/if}
              </div>
              <p class="row__desc">{r.desc}</p>
            </div>

            <div class="row__chain">
              <ol class="chain">
                {#each r.chain as m, i (m)}
                  <li class="link">
                    <span class="link__ord">{i + 1}</span>
                    <span class="link__name">{m}</span>
                    <span class="link__ops">
                      <button
                        class="link__op"
                        title="move earlier"
                        disabled={saving[r.role] || i === 0}
                        onclick={() => move(r, i, -1)}
                        aria-label="move {m} earlier">←</button>
                      <button
                        class="link__op"
                        title="move later"
                        disabled={saving[r.role] || i === r.chain.length - 1}
                        onclick={() => move(r, i, 1)}
                        aria-label="move {m} later">→</button>
                      <button
                        class="link__op link__op--rm"
                        title="remove"
                        disabled={saving[r.role]}
                        onclick={() => remove(r, i)}
                        aria-label="remove {m}">×</button>
                    </span>
                    {#if i < r.chain.length - 1}<span class="link__arrow" aria-hidden="true">↳</span>{/if}
                  </li>
                {/each}
              </ol>

              <div class="row__actions">
                <div class="row__add">
                  <Dropdown
                    label="add a model to the {r.role} chain"
                    placeholder="add model…"
                    options={addOptions(r)}
                    value={undefined}
                    disabled={saving[r.role]}
                    onchange={(m) => add(r, m as string)}
                  />
                </div>
                {#if r.custom}
                  <button
                    class="row__reset"
                    disabled={saving[r.role]}
                    onclick={() => reset(r)}
                    title="revert to the built-in default chain">reset</button>
                {/if}
                {#if saving[r.role]}
                  <span class="row__saving" role="status" aria-live="polite">
                    <span class="row__spinner" aria-hidden="true"></span>saving…
                  </span>
                {/if}
              </div>
            </div>
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<style>
  .rc {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    max-width: 880px;
  }
  .rc__head {
    padding: 0 var(--sp-2);
  }
  .rc__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-body) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .rc__sub {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
    max-width: 70ch;
  }
  .rc__skel-wrap {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .rc__skel {
    height: 72px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: rc-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes rc-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .rc__error {
    padding: var(--sp-5);
    color: var(--error);
    font-size: var(--fs-body-sm);
  }
  .rc__list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    padding: var(--sp-5) var(--sp-6);
  }
  .row__name {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .row__role {
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-mono);
    color: var(--text-primary);
  }
  .row__badge {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 2px var(--sp-3);
    border-radius: var(--r-full);
    background: var(--state-selected);
    color: var(--brand-bright);
    border: 1px solid var(--border-brand-faint);
  }
  .row__badge--default {
    background: var(--bg-raised-2);
    color: var(--text-faint);
    border-color: var(--border-subtle);
  }
  .row__desc {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
  }

  .row__chain {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .chain {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--sp-2);
  }
  .link {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-2) var(--sp-2) var(--sp-3);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised-2);
  }
  .link__ord {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-mono);
    color: var(--text-faint);
    min-width: 12px;
    text-align: center;
  }
  .link__name {
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-mono);
    color: var(--text-primary);
  }
  .link__ops {
    display: inline-flex;
    align-items: center;
    gap: 1px;
  }
  .link__op {
    width: 20px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--r-sm);
    cursor: pointer;
    font-size: var(--fs-label);
    line-height: 1;
  }
  .link__op:hover:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .link__op:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }
  .link__op--rm:hover:not(:disabled) {
    color: var(--error);
  }
  /* The fall-through arrow sits OUTSIDE the chip, between links. */
  .link__arrow {
    margin-left: var(--sp-2);
    color: var(--text-ghost);
    font-size: var(--fs-label);
  }

  .row__actions {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .row__add {
    width: 180px;
  }
  .row__reset {
    height: 30px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .row__reset:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--border-strong);
  }
  .row__reset:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .row__saving {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    color: var(--working);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .row__spinner {
    width: 12px;
    height: 12px;
    border-radius: var(--r-full);
    border: 2px solid var(--working-bg);
    border-top-color: var(--working);
    animation: rc-spin 0.7s linear infinite;
  }
  @keyframes rc-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rc__skel,
    .row__spinner {
      animation: none;
    }
  }
</style>
