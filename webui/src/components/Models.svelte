<script lang="ts">
  import { onMount } from "svelte";
  import { api, post } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let catalog = $state<any>(null);
  let modelFilter = $state("");
  let providerFilter = $state("all");
  let verifiedOnly = $state(false);
  let failed = $state(false);
  let refreshing = $state(false);
  let verifying = $state(false);

  async function load() {
    failed = false;
    try {
      const q = verifiedOnly ? "?all=1&working=1" : "?all=1";
      catalog = await api(`/api/catalog${q}`);
    } catch {
      failed = true;
    }
  }

  async function refreshLive() {
    refreshing = true;
    try {
      await post("/api/catalog/refresh");
    } catch {
      // fall through to cached reload
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

  onMount(load);

  const groupOf = (m: any) => {
    const p = m.provider ?? "";
    if (p === "zen" || p === "opencode") return "opencode";
    if (p === "go" || p === "cline") return "cline";
    return p || "other";
  };

  const all = $derived(catalog?.data ?? []);
  const visible = $derived(
    all.filter((m: any) => {
      if (providerFilter !== "all" && groupOf(m) !== providerFilter) return false;
      const q = modelFilter.trim().toLowerCase();
      return !q || String(m.id ?? "").toLowerCase().includes(q);
    }),
  );

  const providers: Array<[string, string]> = [
    ["all", "All"],
    ["opencode", "OpenCode"],
    ["cline", "Cline"],
    ["antigravity", "Antigravity"],
    ["codex", "Codex"],
  ];
</script>

<PageHeading section="MODELS" title="Models" description="Every model your gateway can serve. Verified only checks each one live." icon="models" />

<div class="fade-up flex flex-wrap items-center gap-2">
  <div class="eyebrow !mb-0">{verifying ? "verifying…" : `${visible.length} of ${all.length} models`}</div>
  <span class="grow"></span>
  <input class="w-full sm:w-56" placeholder="filter models…" bind:value={modelFilter} />
  <button class="btn-ghost" onclick={load}>refresh</button>
  <button class="btn-ghost" onclick={refreshLive} disabled={refreshing}>{refreshing ? "updating…" : "refresh live"}</button>
</div>

<div class="fade-up mt-2 flex flex-wrap items-center gap-1.5">
  {#each providers as [key, label] (key)}
    <button class="pillbtn" class:on={providerFilter === key} onclick={() => (providerFilter = key)}>{label}</button>
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

{#if !catalog && !failed}
  <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each Array(6) as _, i (i)}
      <div class="skel h-28"></div>
    {/each}
  </div>
{:else if failed}
  <div class="empty mt-3">failed to load models — check the gateway</div>
{:else}
  <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each visible as m (m.id)}
      <div class="card flex flex-col">
        <div class="flex items-start gap-2">
          <div class="min-w-0 grow">
            <div class="truncate font-mono text-[13px]" title={m.id}>{m.id}</div>
            <div class="mt-2 flex flex-wrap gap-1">
              <span class="pill good">{m.provider ?? "–"}</span>
              <span class="pill">{m.route_protocol ?? "?"}</span>
              {#if m.reasoning || m.supports_reasoning}<span class="pill">reasoning</span>{/if}
              {#if m.tool_call}<span class="pill">tools</span>{/if}
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-end gap-1.5 text-right text-[11px] leading-tight text-[color:var(--color-faint)]">
            <div class="tnum">ctx {m.context_window ? Math.round(m.context_window / 1000) + "k" : "–"}</div>
            <div class="tnum">out {m.max_output ? Math.round(m.max_output / 1000) + "k" : "–"}</div>
          </div>
        </div>
        {#if m.reasoning_efforts?.length}
          <div class="mt-2 flex flex-wrap gap-1">
            {#each m.reasoning_efforts as lv (lv)}
              <span class="mono rounded-full border border-[color:var(--color-edge)] px-2 py-0.5 text-[10px] text-[color:var(--color-dim)]">{typeof lv === "string" ? lv.replace(/"/g, "") : lv}</span>
            {/each}
          </div>
        {/if}
      </div>
    {/each}
    {#if !visible.length}
      <div class="empty sm:col-span-2 xl:col-span-3">no model matches the current filters</div>
    {/if}
  </div>
{/if}

<style>
  .pillbtn { padding: 4px 12px; border-radius: 99px; border: 1px solid var(--color-edge); color: var(--color-dim); font-size: 11px; }
  .pillbtn.on { color: var(--color-ink); border-color: var(--color-accent); background: var(--color-raised); }
  .verify { display: flex; align-items: center; gap: 6px; color: var(--color-dim); font-size: 12px; }
  .verify input { width: 15px; height: 15px; }
</style>
