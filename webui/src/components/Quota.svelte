<script lang="ts">
  import { onMount } from "svelte";
  import { api, num } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let data = $state<any>(null);
  let failed = $state("");
  let loading = $state(true);
  let showInternal = $state(false);
  let filter = $state("");

  async function load() {
    loading = true;
    failed = "";
    try {
      data = await api<any>("/api/quota");
    } catch (e) {
      failed = String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function pct(f: number | null | undefined): string {
    if (f === null || f === undefined) return "–";
    return (f * 100).toFixed(1) + "%";
  }
  function tone(f: number | null | undefined): string {
    if (f === null || f === undefined) return "";
    if (f >= 0.5) return "good";
    if (f >= 0.2) return "warn";
    return "bad";
  }

  const antigravity = $derived<any[]>(data?.antigravity ?? []);
  const codexAccounts = $derived<any[]>(data?.codex?.accounts ?? []);
  const codexModels = $derived<any[]>(data?.codex?.models ?? []);
  const clineAccounts = $derived<any[]>(data?.cline?.accounts ?? []);
  const clineAvailable = $derived<boolean>(data?.cline?.available !== false);
</script>

<PageHeading section="QUOTA" title="Quota meters" description="Live remaining quota for Antigravity, plus local usage for Codex and Cline." icon="quota" />

<div class="fade-up flex flex-wrap items-center gap-2">
  <div class="eyebrow !mb-0">quota · {antigravity.length} antigravity · {codexAccounts.length} codex · {clineAccounts.length} cline</div>
  <span class="grow"></span>
  <label class="flex items-center gap-1.5 text-[12px] text-[color:var(--color-dim)]">
    <input type="checkbox" bind:checked={showInternal} /> show internal
  </label>
  <input class="w-full sm:w-56" placeholder="filter models…" bind:value={filter} />
  <button class="btn-ghost" onclick={load} disabled={loading}>{loading ? "loading…" : "refresh"}</button>
</div>

{#if failed}
  <div class="empty mt-3">failed to load quota — {failed}</div>
{:else if loading && !data}
  <div class="mt-3 grid grid-cols-1 gap-3">
    {#each Array(3) as _, i (i)}<div class="skel h-36"></div>{/each}
  </div>
{:else}
  <!-- Antigravity -->
  <h2 class="mt-4 text-[15px] font-semibold">Antigravity — live Google quota</h2>
  <p class="dim text-[12px]">From <span class="mono">fetchAvailableModels.quotaInfo.remainingFraction</span> per account. 100% = full.</p>
  {#if !antigravity.length}
    <div class="empty mt-2">no Antigravity accounts linked — run <span class="mono">zenflash-llm login antigravity</span></div>
  {:else}
    <div class="mt-2 grid grid-cols-1 gap-3">
      {#each antigravity as acc (acc.project_id + acc.email)}
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-2">
            <strong class="text-[13px]">{acc.email || acc.project_id || "Antigravity account"}</strong>
            {#if acc.project_id}<span class="pill">{acc.project_id}</span>{/if}
            <span class="grow"></span>
            <span class="dim mono text-[11px]">{acc.total ?? (acc.models?.length ?? 0)} models · {acc.full_quota ?? 0} full · {acc.partial ?? 0} partial · {acc.exhausted ?? 0} empty</span>
          </div>
          {#if acc.error}
            <p class="mt-2 text-[12px] bad">quota fetch failed: {acc.error}</p>
          {:else}
            {@const models = (acc.models ?? []).filter((m: any) => (showInternal || !m.is_internal) && (!filter.trim() || m.id.toLowerCase().includes(filter.trim().toLowerCase())))}
            <div class="mt-3 grid gap-2">
              {#each models as m (m.id)}
                <div class="flex items-center gap-3">
                  <div class="min-w-0 flex-1">
                    <div class="flex items-baseline justify-between gap-2">
                      <span class="mono truncate text-[12px]" title={m.display_name ? `${m.id} — ${m.display_name}` : m.id}>{m.id}</span>
                      <span class="tnum shrink-0 text-[11px]">{pct(m.remaining_fraction)}</span>
                    </div>
                    <div class="quota-bar"><div class="quota-fill {tone(m.remaining_fraction)}" style="width:{m.remaining_fraction == null ? 0 : Math.round(m.remaining_fraction * 100)}%"></div></div>
                    {#if m.display_name}<div class="dim truncate text-[11px]">{m.display_name}{m.is_internal ? " · internal" : ""}</div>{/if}
                  </div>
                </div>
              {/each}
              {#if !models.length}<div class="dim text-[12px]">no models match filter</div>{/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <!-- Codex -->
  <h2 class="mt-6 text-[15px] font-semibold">Codex — local usage</h2>
  <p class="dim text-[12px]">OpenAI exposes no remaining monthly quota via API, so this is ZenFlash-observed usage in the last 30 days. <a class="inline-link" href="https://chatgpt.com/#settings/Usage" target="_blank" rel="noreferrer">Manage usage</a></p>
  {#if !codexAccounts.length}
    <div class="empty mt-2">no Codex accounts linked</div>
  {:else}
    <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2">
      {#each codexAccounts as a (a.id)}
        <div class="card p-4">
          <div class="flex items-center gap-2">
            <strong class="truncate text-[13px]" title={a.email}>{a.email || a.id}</strong>
            <span class="grow"></span>
            <span class="pill">{a.models} models</span>
          </div>
          <div class="mt-2 grid grid-cols-3 gap-2 text-center">
            <div><div class="tnum text-[15px]">{num(a.requests_30d)}</div><div class="dim text-[10px]">requests / 30d</div></div>
            <div><div class="tnum text-[15px]">{num(a.input_tokens_30d)}</div><div class="dim text-[10px]">in tokens / 30d</div></div>
            <div><div class="tnum text-[15px]">{num(a.output_tokens_30d)}</div><div class="dim text-[10px]">out tokens / 30d</div></div>
          </div>
          <div class="dim mt-2 text-[11px]">remaining: {a.openai_remaining ?? "not exposed by OpenAI API"}</div>
        </div>
      {/each}
    </div>
    {#if codexModels.length}
      <div class="card mt-3 p-4">
        <div class="eyebrow">codex models · {codexModels.length}</div>
        <div class="mt-2 flex flex-wrap gap-1.5">
          {#each codexModels as m (m.id)}<code class="mono rounded-md border border-[color:var(--color-edge)] px-2 py-1 text-[11px]">{m.id}</code>{/each}
        </div>
      </div>
    {/if}
  {/if}

  <!-- Cline -->
  <h2 class="mt-6 text-[15px] font-semibold">Cline — local usage</h2>
  <p class="dim text-[12px]">Counters from the embedded Cline proxy (local successful calls and tokens, not upstream quota).</p>
  {#if !clineAvailable}
    <div class="empty mt-2">{data?.cline?.reason ?? "cline proxy unavailable"}</div>
  {:else if !clineAccounts.length}
    <div class="empty mt-2">no Cline accounts linked</div>
  {:else}
    <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2">
      {#each clineAccounts as a (a.accountId || a.email)}
        <div class="card p-4">
          <div class="flex items-center gap-2">
            <strong class="truncate text-[13px]" title={a.email}>{a.email || a.accountId}</strong>
            <span class="grow"></span>
            <span class="pill" class:good={a.status === "active"} class:warn={a.status === "cooldown"} class:bad={a.status === "expired"}>{a.status ?? "unknown"}</span>
          </div>
          <div class="mt-2 grid grid-cols-4 gap-2 text-center">
            <div><div class="tnum text-[15px]">{num(a.usageCountToday)}</div><div class="dim text-[10px]">req today</div></div>
            <div><div class="tnum text-[15px]">{num(a.usageCount)}</div><div class="dim text-[10px]">req total</div></div>
            <div><div class="tnum text-[15px]">{num(a.tokensToday)}</div><div class="dim text-[10px]">tok today</div></div>
            <div><div class="tnum text-[15px]">{num(a.tokensTotal)}</div><div class="dim text-[10px]">tok total</div></div>
          </div>
          {#if a.cooldownUntil}<div class="dim mt-2 text-[11px]">cooldown until {a.cooldownUntil}{a.lastReason ? ` — ${a.lastReason}` : ""}</div>{/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}

<style>
  .quota-bar { height: 6px; border-radius: 99px; background: color-mix(in oklab, var(--color-edge) 70%, transparent); overflow: hidden; margin-top: 4px; }
  .quota-fill { height: 100%; border-radius: 99px; background: var(--color-faint); transition: width .3s; }
  .quota-fill.good { background: oklch(0.79 0.15 155); }
  .quota-fill.warn { background: oklch(0.825 0.15 86); }
  .quota-fill.bad { background: oklch(0.705 0.19 28); }
</style>
