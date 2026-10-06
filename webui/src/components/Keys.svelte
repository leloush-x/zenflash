<script lang="ts">
  import { onMount } from "svelte";
  import { api, ago } from "../lib";
  import PageHeading from "./PageHeading.svelte";
  let { live }: { live: any } = $props();
  const res = $derived(live?.resources);
  const keyCount = $derived((res?.keys ?? []).length);
  const zenKeys = $derived((res?.keys ?? []).filter((k: any) => k.tier === "zen").length);
  const goKeys = $derived((res?.keys ?? []).filter((k: any) => k.tier === "go").length);
  const codexPoolKeys = $derived((res?.keys ?? []).filter((k: any) => k.tier === "codex").length);
  const agKeys = $derived((res?.keys ?? []).filter((k: any) => k.tier === "antigravity").length);
  const healthyNodes = $derived((res?.proxies ?? []).filter((p: any) => p.healthy).length);

  let serverKeys = $state(1);
  let codexServiceCount = $state(0);
  let debug = $state<any>(null);
  let models = $state<any>(null);

  async function loadExtra() {
    try {
      const c = await api<any>("/api/config");
      serverKeys = (c?.server_keys ?? []).length;
    } catch {}
    try {
      const r = await api<any>("/api/codex/accounts");
      codexServiceCount = (r?.accounts ?? []).length;
    } catch {}
    try {
      debug = await api<any>("/api/debug/models");
    } catch {}
    try {
      models = await api<any>("/api/catalog").catch(() => null);
    } catch {}
  }
  onMount(loadExtra);

  const codexTotal = $derived(codexPoolKeys + codexServiceCount);
  const providersActive = $derived(
    (zenKeys > 0 ? 1 : 0) + (goKeys > 0 ? 1 : 0) + (codexTotal > 0 ? 1 : 0) + (agKeys > 0 ? 1 : 0),
  );
  const countAvail = $derived((key: string) => (debug?.models ?? []).filter((m: any) => m[key]).length);
  const freeModels = $derived((debug?.models ?? []).filter((m: any) => m.anonymous_eligibility?.allowed).length);
</script>

<PageHeading section="KEYS" title="Key health" description="One API key controls everything. Providers below show active keys + their model counts." icon="keys" />

<div class="resource-strip fade-up">
  <div class="resource-stat highlight"><span>API KEY</span><strong>{serverKeys}</strong><small>single key · see Config</small></div>
  <div class="resource-stat"><span>PROVIDERS ACTIVE</span><strong>{providersActive}<i> / 4</i></strong><small>pools with keys</small></div>
  <div class="resource-stat"><span>FREE MODELS</span><strong>{freeModels}</strong><small>anonymous allowed</small></div>
  <div class="resource-stat"><span>ZEN POOL</span><strong>{zenKeys}</strong><small>{countAvail("available_zen")} models</small></div>
  <div class="resource-stat"><span>GO POOL</span><strong>{goKeys}</strong><small>{countAvail("available_go")} models</small></div>
  <div class="resource-stat"><span>CODEX POOL</span><strong>{codexTotal}</strong><small>{codexServiceCount} account · {countAvail("available_codex")} catalog</small></div>
  <div class="resource-stat"><span>ANTIGRAVITY POOL</span><strong>{agKeys}</strong><small>{countAvail("available_antigravity")} models</small></div>
  <div class="resource-stat"><span>ALL KEYS</span><strong>{keyCount + codexServiceCount}</strong><small>pool + service</small></div>
  <div class="resource-stat"><span>PROXY HEALTH</span><strong>{healthyNodes}<i> / {(res?.proxies ?? []).length}</i></strong><small>healthy nodes</small></div>
</div>

<p class="dim mt-2 text-[12px]">Clients use the single <span class="mono">server key</span> from Config for <span class="mono">/v1/models</span>, <span class="mono">/chat/completions</span>, <span class="mono">/responses</span>, <span class="mono">/messages</span>. Provider keys above only decide which upstreams can serve. Live catalog: <span class="mono">GET /v1/models?refresh=1</span> forces a fresh poll.</p>

<div class="fade-up grid gap-3 xl:grid-cols-2">
  <section class="panel p-4">
    <div class="eyebrow">key pool · cooldowns</div>
    <div class="overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 60vh">
      <table class="table-cards">
        <thead><tr><th>id</th><th>tier</th><th class="num">idx</th><th class="num">proxy</th><th class="num">fails</th><th>cooldown</th></tr></thead>
        <tbody>
          {#each res?.keys ?? [] as k (k.id + k.index)}
            <tr>
              <td data-label="id" class="mono text-[12px]">{k.id}</td>
              <td data-label="tier"><span class="pill">{k.tier}</span></td>
              <td data-label="idx" class="num">{k.index}</td>
              <td data-label="proxy" class="num">{k.proxy_index}</td>
              <td data-label="fails" class="num" class:bad={k.failures > 0}>{k.failures}</td>
              <td data-label="cooldown" class:warn={k.cooldown_until}>{k.cooldown_until ? ago(k.cooldown_until) : "–"}</td>
            </tr>
          {/each}
          {#if !(res?.keys ?? []).length}
            <tr><td colspan="6"><div class="empty">no file-based keys — anonymous + service accounts only</div></td></tr>
          {/if}
        </tbody>
      </table>
    </div>
  </section>

  <section class="panel p-4">
    <div class="eyebrow">proxy nodes</div>
    <div class="overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 60vh">
      <table class="table-cards">
        <thead><tr><th class="num">idx</th><th>address</th><th>health</th><th class="num">zen</th><th class="num">go</th><th class="num">codex</th><th class="num">ag</th><th class="num">anon</th></tr></thead>
        <tbody>
          {#each res?.proxies ?? [] as p (p.index)}
            <tr>
              <td data-label="idx" class="num">{p.index}</td>
              <td data-label="address" class="mono min-w-0 truncate text-[12px]" title={p.address}>{p.address}</td>
              <td data-label="health"><span class="pill" class:good={p.healthy} class:bad={!p.healthy}><span class="dot"></span>{p.healthy ? "healthy" : p.checking ? "checking" : "down"}</span></td>
              <td data-label="zen" class="num">{p.zen_keys}</td>
              <td data-label="go" class="num">{p.go_keys}</td>
              <td data-label="codex" class="num">{p.codex_keys ?? 0}</td>
              <td data-label="ag" class="num">{p.antigravity_keys ?? 0}</td>
              <td data-label="anon" class="num">{p.anonymous ? 1 : 0}</td>
            </tr>
          {/each}
          {#if !(res?.proxies ?? []).length}
            <tr><td colspan="8"><div class="empty">no proxy nodes reported</div></td></tr>
          {/if}
        </tbody>
      </table>
    </div>
  </section>
</div>
