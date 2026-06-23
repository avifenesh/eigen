<script lang="ts">
  // Config — the editable ~/.eigen/config.json as a typed form. Each field
  // renders by its option shape: a boolean toggle, a select for a closed/dynamic
  // set, a space-separated multi-select (chips) for route_providers, or a free
  // text/number input. Every change validates + persists through the bridge
  // (config.Set), so an invalid value is rejected with a toast and the field
  // reverts to its stored value.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { ConfigDTO, ConfigFieldDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<ConfigDTO | null>(null);
  let loading = $state(true);
  let saving = $state<Record<string, boolean>>({});
  // Local working copy of values so inputs are editable before commit.
  let values = $state<Record<string, string>>({});

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Config();
      if (seq === loadSeq && d) {
        data = d;
        values = Object.fromEntries(d.fields.map((f) => [f.key, f.value]));
      }
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

  function isBool(f: ConfigFieldDTO): boolean {
    const o = f.options ?? [];
    return o.length === 2 && o.includes("true") && o.includes("false");
  }

  async function commit(key: string, value: string) {
    saving[key] = true;
    try {
      const stored = await Bridge.SetConfig(key, value);
      values[key] = stored;
      if (data) {
        const f = data.fields.find((x) => x.key === key);
        if (f) f.value = stored;
      }
      toasts.success(`${key} saved`);
    } catch (e) {
      // Reject: revert to the stored value and surface why.
      if (data) {
        const f = data.fields.find((x) => x.key === key);
        if (f) values[key] = f.value;
      }
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete saving[key];
    }
  }

  // Multi-select (route_providers): toggle a provider in the space-separated set.
  function toggleMulti(f: ConfigFieldDTO, opt: string) {
    const set = new Set((values[f.key] ?? "").split(/\s+/).filter(Boolean));
    if (set.has(opt)) set.delete(opt);
    else set.add(opt);
    commit(f.key, [...set].join(" "));
  }
  function multiHas(key: string, opt: string): boolean {
    return (values[key] ?? "").split(/\s+/).filter(Boolean).includes(opt);
  }

  // Commit a text/number field on blur or Enter if it changed from stored.
  function commitIfChanged(f: ConfigFieldDTO) {
    if (values[f.key] !== f.value) commit(f.key, values[f.key] ?? "");
  }
</script>

<div class="cfg">
  {#if loading && !data}
    <div class="cfg__pad">
      {#each Array(6) as _, i (i)}<div class="cfg__skel"></div>{/each}
    </div>
  {:else if !data}
    <EmptyState glyph="⚙" title="Config unavailable" line="Could not read ~/.eigen/config.json." />
  {:else}
    <div class="cfg__scroll">
      <div class="cfg__path selectable">{data.path}</div>
      <div class="cfg__list">
        {#each data.fields as f (f.key)}
          <Card>
            <div class="field">
              <div class="field__info">
                <label class="field__key" for="cfg-{f.key}">{f.key}</label>
                <p class="field__desc">{f.desc}</p>
              </div>
              <div class="field__control">
                {#if isBool(f)}
                  <button
                    id="cfg-{f.key}"
                    class="toggle"
                    class:toggle--on={values[f.key] === "true"}
                    role="switch"
                    aria-checked={values[f.key] === "true"}
                    aria-label={f.key}
                    disabled={saving[f.key]}
                    onclick={() => commit(f.key, values[f.key] === "true" ? "false" : "true")}
                  >
                    <span class="toggle__knob"></span>
                  </button>
                {:else if f.multi && f.options}
                  <div class="chips">
                    {#each f.options as opt (opt)}
                      <button
                        class="chip"
                        class:chip--on={multiHas(f.key, opt)}
                        disabled={saving[f.key]}
                        onclick={() => toggleMulti(f, opt)}
                      >
                        {opt}
                      </button>
                    {/each}
                  </div>
                {:else if f.options && f.options.length > 0}
                  <select
                    id="cfg-{f.key}"
                    class="select"
                    value={values[f.key]}
                    disabled={saving[f.key]}
                    onchange={(e) => commit(f.key, (e.currentTarget as HTMLSelectElement).value)}
                  >
                    {#if !f.options.includes(values[f.key])}
                      <option value={values[f.key]}>{values[f.key] || "(unset)"}</option>
                    {/if}
                    {#each f.options as opt (opt)}
                      <option value={opt}>{opt}</option>
                    {/each}
                  </select>
                {:else}
                  <input
                    id="cfg-{f.key}"
                    class="input"
                    type="text"
                    bind:value={values[f.key]}
                    disabled={saving[f.key]}
                    onblur={() => commitIfChanged(f)}
                    onkeydown={(e) => e.key === "Enter" && commitIfChanged(f)}
                    placeholder="(unset)"
                  />
                {/if}
              </div>
            </div>
          </Card>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .cfg {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .cfg__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .cfg__pad {
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .cfg__skel {
    height: 64px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: cfg-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes cfg-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .cfg__path {
    font: var(--fw-regular) var(--fs-label) / 1 var(--font-mono);
    color: var(--text-faint);
    padding-bottom: var(--sp-2);
  }
  .cfg__list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    max-width: 880px;
  }
  .field {
    display: flex;
    align-items: center;
    gap: var(--sp-6);
    padding: var(--sp-5) var(--sp-6);
  }
  .field__info {
    flex: 1;
    min-width: 0;
  }
  .field__key {
    display: block;
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-mono);
    color: var(--text-primary);
  }
  .field__desc {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
  }
  .field__control {
    flex: none;
    display: flex;
    justify-content: flex-end;
    min-width: 180px;
    max-width: 320px;
  }

  /* Toggle (boolean fields) */
  .toggle {
    width: 40px;
    height: 22px;
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
    background: var(--bg-inset);
    cursor: pointer;
    position: relative;
    transition: background var(--dur-fast) var(--ease-out);
    padding: 0;
  }
  .toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .toggle--on {
    background: var(--brand-dim);
    border-color: var(--brand);
  }
  .toggle__knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: var(--r-full);
    background: var(--text-secondary);
    transition:
      transform var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .toggle--on .toggle__knob {
    transform: translateX(18px);
    background: var(--brand-bright);
  }

  /* Select + input */
  .select,
  .input {
    width: 100%;
    height: 32px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised-2);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
  }
  .select:focus-visible,
  .input:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .input::placeholder {
    color: var(--text-ghost);
  }

  /* Multi-select chips */
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
    justify-content: flex-end;
  }
  .chip {
    height: 24px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised-2);
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .chip:hover {
    color: var(--text-primary);
  }
  .chip:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .chip--on {
    background: var(--state-selected);
    border-color: var(--border-brand-faint);
    color: var(--brand-bright);
  }
  @media (prefers-reduced-motion: reduce) {
    .cfg__skel {
      animation: none;
    }
  }
</style>
