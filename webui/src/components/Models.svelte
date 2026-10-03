<script lang="ts">
  import { onMount } from "svelte";
  import { api } from "../lib";

  let data = $state<any>(null);
  let modelFilter = $state("");
  let failed = $state(false);

  async function load() {
    failed = false;
    try {
      data = await api("/api/debug/models");
    } catch {
      failed = true;
    }
  }

  onMount(load);

  const visible = $derived(
    (data?.models ?? []).filter((m: any) =>
      !modelFilter.trim() || m.model.toLowerCase().includes(modelFilter.trim().toLowerCase()),
    ),
  );
</script>

<div class="fade-up flex flex-wrap items-center gap-2">
  <div class="eyebrow !mb-0">routable models · {visible.length} / {(data?.models ?? []).length}</div>
  <span class="grow"></span>
  <input class="w-full sm:w-56" placeholder="filter models…" bind:value={modelFilter} />
  <button class="btn-ghost" onclick={load}>refresh</button>
</div>

{#if data?.metadata?.last_error}
  <p class="mt-2 text-[12px] warn">catalog: {data.metadata.last_error}</p>
{/if}

{#if !data && !failed}
  <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each Array(6) as _, i (i)}
      <div class="skel h-36"></div>
    {/each}
  </div>
{:else if failed}
  <div class="empty mt-3">failed to load models — check the gateway</div>
{:else}
  <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
    {#each visible as m (m.model)}
      <div class="card flex flex-col">
        <div class="flex items-start gap-2">
          <div class="min-w-0 grow">
            <div class="truncate font-mono text-[13px]" title={m.model}>{m.model}</div>
            <div class="mt-2 flex flex-wrap gap-1">
              <span class="pill">{m.native_protocol ?? "?"}</span>
              <span class="pill" class:good={m.available_zen}>zen</span>
              <span class="pill" class:good={m.available_go}>go</span>
              {#if m.anonymous}<span class="pill good">anon</span>{/if}
              {#if m.route_error}<span class="pill bad">{m.route_error}</span>{/if}
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-end gap-1.5 text-right text-[11px] leading-tight text-[color:var(--color-faint)]">
            <div class="tnum">protocol: {m.protocol_source ?? "?"}</div>
            <div class="tnum">tier keys: {(m.key_tiers ?? []).join("/") || "–"}</div>
          </div>
        </div>

        <div class="mt-3 flex items-center gap-1.5 text-[11px]">
          <span class="pill" class:good={m.anonymous_eligibility?.allowed}>
            {m.anonymous_eligibility?.allowed ? (m.anonymous_eligibility.source ?? "free") : "paid"}
          </span>
          <span class="grow"></span>
          <span class="dim mono">{m.anonymous_eligibility?.input_cost ?? ""}{m.anonymous_eligibility?.input_cost !== undefined ? " / " : ""}{m.anonymous_eligibility?.output_cost ?? ""}</span>
        </div>

        {#if m.channel}
          <div class="mt-2 text-[11px] text-[color:var(--color-faint)]">
            last route: <span class="mono">{m.channel}</span>{m.attempts ? ` · ${m.attempts} attempts` : ""}
          </div>
        {/if}
      </div>
    {/each}
    {#if !visible.length}
      <div class="empty sm:col-span-2 xl:col-span-3">no model matches "{modelFilter}"</div>
    {/if}
  </div>
{/if}
