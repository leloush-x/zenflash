<script lang="ts">
  import { onMount } from "svelte";
  import { api, modelSource, post, put, rawModelID, shortModelID, sourceLabel } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let catalog = $state<any>(null);
  let flags = $state<any[]>([]);
  let modelFilter = $state("");
  let providerFilter = $state("all");
  let verifiedOnly = $state(false);
  let failed = $state(false);
  let refreshing = $state(false);
  let verifying = $state(false);
  let flagBusy = $state("");
  let notice = $state("");

  async function load() {
    failed = false;
    try {
      const q = verifiedOnly ? "?all=1&working=1" : "?all=1";
      const [nextCatalog, flagData] = await Promise.all([
        api(`/api/catalog${q}`),
        api<any>("/api/flags").catch(() => ({ flags: [], persisted: false })),
      ]);
      catalog = nextCatalog;
      flags = flagData?.flags ?? [];
    } catch {
      failed = true;
    }
  }

  async function refreshLive() {
    refreshing = true;
    try {
      const result = await post<any>("/api/catalog/refresh");
      if (result?.error?.message) throw new Error(result.error.message);
    } catch (error) {
      notice = String(error);
    } finally {
      refreshing = false;
    }
    await load();
  }

  async function toggleVerified() {
    verifiedOnly = !verifiedOnly;
    if (verifiedOnly) verifying = true;
    try {
      await load();
    } finally {
      verifying = false;
    }
  }

  async function setDeprecated(id: string, deprecated: boolean) {
    const raw = rawModelID(id);
    flagBusy = raw;
    notice = "";
    try {
      const result = await put<any>("/api/flags", { id: raw, deprecated });
      if (result?.error?.message) throw new Error(result.error.message);
      if (!result?.ok) throw new Error("Postgres is unavailable; set DATABASE_URL to save model switches");
      notice = deprecated ? `${shortModelID(id)} moved to deprecated` : `${shortModelID(id)} restored`;
      await load();
    } catch (error) {
      notice = String(error);
    } finally {
      flagBusy = "";
    }
  }

  onMount(load);

  const flagState = $derived(new Map(flags.map((flag: any) => [rawModelID(flag.id), flag.deprecated === true])));
  const modelRows = $derived(
    (catalog?.data ?? []).map((m: any) => ({
      ...m,
      source: modelSource(m),
      label: shortModelID(m.id ?? ""),
      _deprecated: flagState.get(rawModelID(m.id ?? "")) ?? m.deprecated === true,
    })),
  );
  const searched = $derived(
    modelRows.filter((m: any) => {
      const q = modelFilter.trim().toLowerCase();
      return !q || String(m.id ?? "").toLowerCase().includes(q);
    }),
  );
  const selectedRows = $derived(
    searched.filter((m: any) => providerFilter === "all" || m.source === providerFilter),
  );
  const activeRows = $derived(selectedRows.filter((m: any) => !m._deprecated));
  const deprecatedRows = $derived(selectedRows.filter((m: any) => m._deprecated));
  const activeAll = $derived(modelRows.filter((m: any) => !m._deprecated));
  const sourceOrder = ["opencode", "cline", "antigravity", "codex", "other"];
  const sourceGroups = $derived(
    sourceOrder
      .map((key) => ({ key, label: sourceLabel(key), items: activeRows.filter((m: any) => m.source === key) }))
      .filter((group) => group.items.length),
  );
  const counts = $derived(
    Object.fromEntries(sourceOrder.map((key) => [key, activeAll.filter((m: any) => m.source === key).length])),
  );
  const providers: Array<[string, string]> = [
    ["all", "All"],
    ["opencode", "OpenCode"],
    ["cline", "Cline"],
    ["antigravity", "Antigravity"],
    ["codex", "Codex"],
  ];
</script>

<PageHeading section="MODELS" title="Model catalog" description="Source groups, compact IDs, live checks, and admin switches." icon="models" />

<div class="fade-up flex flex-wrap items-center gap-2">
  <div class="eyebrow !mb-0">{verifying ? "verifying…" : `${activeRows.length} active · ${deprecatedRows.length} deprecated · ${modelRows.length} total`}</div>
  <span class="grow"></span>
  <input class="w-full sm:w-64" placeholder="filter model id…" bind:value={modelFilter} />
  <button class="btn-ghost" onclick={load}>refresh</button>
  <button class="btn-ghost" onclick={refreshLive} disabled={refreshing}>{refreshing ? "updating…" : "refresh live"}</button>
</div>

<div class="fade-up mt-2 flex flex-wrap items-center gap-1.5">
  {#each providers as [key, label] (key)}
    <button class="pillbtn" class:on={providerFilter === key} onclick={() => (providerFilter = key)}>
      {label}
      {#if key !== "all"}<span class="pillcount">{counts[key] ?? 0}</span>{/if}
    </button>
  {/each}
  <span class="grow"></span>
  <label class="verify">
    <input type="checkbox" checked={verifiedOnly} onchange={toggleVerified} />
    verified only
  </label>
</div>
{#if verifiedOnly}
  <p class="mt-2 text-[12px] dim">Each model gets one tiny live reply (8 at a time) — this takes a while on long lists.</p>
{/if}
{#if notice}
  <p class="mt-2 text-[12px]" class:good={!flagBusy && notice.includes("restored")}>{notice}</p>
{/if}

{#if !catalog && !failed}
  <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each Array(6) as _, i (i)}
      <div class="skel h-20"></div>
    {/each}
  </div>
{:else if failed}
  <div class="empty mt-3">failed to load models — check the gateway</div>
{:else}
  <div class="catalog-groups fade-up mt-3">
    {#each sourceGroups as group (group.key)}
      <details class="source-block" open={providerFilter === group.key}>
        <summary>
          <span class="source-name"><i></i><strong>{group.label}</strong></span>
          <span class="source-count">{group.items.length} active</span>
          <span class="chev">⌄</span>
        </summary>
        <div class="model-list">
          {#each group.items as m (m.id)}
            <article class="model-row" class:busy={flagBusy === rawModelID(m.id)}>
              <span class="source-chip">{group.label}</span>
              <div class="model-copy">
                <strong title={m.id}>{m.label}</strong>
                <span>{m.route_protocol ?? "auto"}{m.context_window ? ` · ${Math.round(m.context_window / 1000)}k` : ""}{m.max_output ? ` out ${Math.round(m.max_output / 1000)}k` : ""}</span>
              </div>
              <div class="model-tags">
                {#if m.reasoning || m.supports_reasoning}<span>R</span>{/if}
                {#if m.tool_call}<span>T</span>{/if}
              </div>
              <button class="switch off" disabled={flagBusy === rawModelID(m.id)} title={`Disable ${m.id}`} onclick={() => setDeprecated(m.id, true)}>
                <i></i><span>off</span>
              </button>
            </article>
          {/each}
        </div>
      </details>
    {/each}

    {#if !sourceGroups.length}
      <div class="empty">no active model matches the current filters</div>
    {/if}

    {#if deprecatedRows.length}
      <details class="deprecated-block">
        <summary>
          <span class="source-name"><i></i><strong>Deprecated</strong></span>
          <span class="source-count">{deprecatedRows.length} disabled</span>
          <span class="chev">⌄</span>
        </summary>
        <div class="deprecated-list">
          {#each deprecatedRows as m (m.id)}
            <article class="deprecated-row" class:busy={flagBusy === rawModelID(m.id)}>
              <span class="source-chip warn">{sourceLabel(m.source)}</span>
              <div class="model-copy">
                <strong title={m.id}>{m.label}</strong>
                <span>{m.route_protocol ?? "auto"} · disabled</span>
              </div>
              <button class="switch on" disabled={flagBusy === rawModelID(m.id)} title={`Restore ${m.id}`} onclick={() => setDeprecated(m.id, false)}>
                <i></i><span>on</span>
              </button>
            </article>
          {/each}
        </div>
      </details>
    {/if}
  </div>
{/if}

<style>
  .pillbtn { padding: 4px 11px; border-radius: 99px; border: 1px solid var(--color-edge); color: var(--color-dim); font-size: 11px; }
  .pillbtn.on { color: var(--color-ink); border-color: var(--color-accent); background: var(--color-raised); }
  .pillcount { margin-left: 5px; color: var(--color-faint); font: 9px var(--font-mono); }
  .verify { display: flex; align-items: center; gap: 6px; color: var(--color-dim); font-size: 12px; }
  .verify input { width: 15px; height: 15px; }

  .catalog-groups { display: grid; gap: 11px; }
  details {
    overflow: hidden;
    border: 1px solid var(--color-edge);
    border-radius: 14px;
    background: linear-gradient(180deg, var(--color-panel), var(--color-surface));
  }
  details[open] { box-shadow: 0 9px 26px oklch(0 0 0 / .1); }
  summary {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 49px;
    padding: 9px 13px;
    cursor: pointer;
    list-style: none;
    user-select: none;
  }
  summary::-webkit-details-marker { display: none; }
  summary:hover { background: oklch(1 0 0 / .02); }
  .source-name { display: flex; align-items: center; gap: 9px; min-width: 0; }
  .source-name i { width: 8px; height: 8px; border-radius: 50%; background: var(--color-accent); box-shadow: 0 0 10px color-mix(in oklab, var(--color-accent) 55%, transparent); }
  .source-name strong { font-size: 12.5px; font-weight: 620; }
  .source-count { margin-left: auto; color: var(--color-faint); font: 10px var(--font-mono); }
  .chev { color: var(--color-faint); transition: transform .16s var(--ease-out); }
  details[open] .chev { transform: rotate(180deg); }

  .model-list, .deprecated-list { display: grid; gap: 5px; padding: 0 8px 8px; }
  .model-row, .deprecated-row {
    display: grid;
    grid-template-columns: auto minmax(120px, 1fr) auto auto;
    align-items: center;
    gap: 10px;
    min-height: 43px;
    padding: 6px 8px;
    border: 1px solid color-mix(in oklab, var(--color-edge) 76%, transparent);
    border-radius: 10px;
    background: oklch(0.115 0.011 272 / .7);
  }
  .model-row:hover, .deprecated-row:hover { border-color: color-mix(in oklab, var(--color-accent) 25%, var(--color-edge)); }
  .source-chip {
    min-width: 67px;
    padding: 2px 7px;
    border: 1px solid color-mix(in oklab, var(--color-accent) 25%, var(--color-edge));
    border-radius: 999px;
    color: var(--color-accent);
    background: color-mix(in oklab, var(--color-accent) 7%, transparent);
    font-size: 8.5px;
    font-weight: 700;
    text-align: center;
    text-transform: uppercase;
    letter-spacing: .06em;
  }
  .source-chip.warn { color: var(--color-warn); border-color: color-mix(in oklab, var(--color-warn) 30%, var(--color-edge)); background: color-mix(in oklab, var(--color-warn) 7%, transparent); }
  .model-copy { display: flex; align-items: baseline; gap: 9px; min-width: 0; }
  .model-copy strong { overflow: hidden; color: var(--color-ink); font: 600 12px var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }
  .model-copy span { overflow: hidden; color: var(--color-faint); font-size: 9.5px; text-overflow: ellipsis; white-space: nowrap; }
  .model-tags { display: flex; gap: 4px; }
  .model-tags span { display: grid; place-items: center; width: 20px; height: 20px; border: 1px solid var(--color-edge); border-radius: 6px; color: var(--color-dim); font: 8px var(--font-mono); }

  .switch {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-height: 25px;
    padding: 2px 7px;
    border: 1px solid var(--color-edge);
    border-radius: 999px;
    background: transparent;
    color: var(--color-faint);
    font-size: 8.5px;
    text-transform: uppercase;
  }
  .switch i { width: 7px; height: 7px; border-radius: 50%; background: var(--color-good); box-shadow: 0 0 7px color-mix(in oklab, var(--color-good) 55%, transparent); }
  .switch.off i { background: var(--color-warn); box-shadow: none; }
  .switch:hover { border-color: var(--color-edge-strong); color: var(--color-ink); }
  .deprecated-block { border-color: color-mix(in oklab, var(--color-warn) 25%, var(--color-edge)); }
  .deprecated-block summary .source-name i { background: var(--color-warn); box-shadow: none; }
  .deprecated-list { grid-template-columns: repeat(auto-fit, minmax(min(360px, 100%), 1fr)); }
  .deprecated-row { opacity: .78; }
  .deprecated-row:hover { opacity: 1; }
  .busy { opacity: .55; pointer-events: none; }

  @media (max-width: 680px) {
    .model-row, .deprecated-row { grid-template-columns: auto minmax(0, 1fr) auto; }
    .model-tags { display: none; }
    .model-copy { display: grid; gap: 1px; }
    .source-chip { min-width: 58px; }
  }
</style>
