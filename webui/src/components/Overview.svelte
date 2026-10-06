<script lang="ts">
  import { num, ms, clock, sparkline, ago } from "../lib";

  let { live }: { live: any } = $props();

  const m = $derived(live?.metrics);
  const lt = $derived(m?.lifetime);
  const wn = $derived(m?.last_hour);
  const usage = $derived(m?.usage);
  const upstream = $derived(m?.upstream);
  const series = $derived(m?.series ?? []);
  const spark = $derived(sparkline(series.map((b: any) => b.total)));
  const sparkTok = $derived(sparkline(series.map((b: any) => b.total_tokens)));
  const recent = $derived((upstream?.requests ?? []).slice(-30).reverse());
  const started = $derived(m?.started_at);
</script>

{#snippet tile(label: string, value: string, sub = "", tone = "", i = 0)}
  <div class="tile fade-up" style="--d:{i * 45}ms">
    <div class="tile-k">{label}</div>
    <div class="tile-v {tone}">{value}</div>
    {#if sub}<div class="tile-s">{sub}</div>{/if}
  </div>
{/snippet}

<div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
  {@render tile("requests 1h", num(wn?.total), `${num(wn?.success)} ok · ${num(wn?.errors)} err`, "", 0)}
  {@render tile("latency p50", ms(wn?.p50_ms), "", "", 1)}
  {@render tile("latency p95", ms(wn?.p95_ms), `p99 ${ms(wn?.p99_ms)}`, "", 2)}
  {@render tile("tokens in", num(usage?.lifetime?.tokens?.input_tokens), `cached ${num(usage?.lifetime?.tokens?.cached_tokens)}`, "", 3)}
  {@render tile("tokens out", num(usage?.lifetime?.tokens?.output_tokens), `reasoning ${num(usage?.lifetime?.tokens?.reasoning_tokens)}`, "", 4)}
  {@render tile("upstream", String(upstream?.last_hour?.success_rate ? Math.round(upstream.last_hour.success_rate * 100) + "%" : "–"), `${num(upstream?.last_hour?.total)} attempts`, upstream?.last_hour?.success_rate >= 0.95 ? "good" : upstream?.last_hour?.total > 0 ? "warn" : "", 5)}
</div>

<div class="mt-3 grid gap-3 xl:grid-cols-3">
  <section class="panel fade-up p-4 xl:col-span-2" style="--d:120ms">
    <div class="eyebrow">requests · last 60 min</div>
    <div class="rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] p-3">
      {#if spark}
        <svg viewBox="0 0 120 28" class="h-24 w-full" preserveAspectRatio="none" aria-label="request volume">
          <defs>
            <linearGradient id="sparkFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="oklch(0.77 0.125 204)" stop-opacity="0.35" />
              <stop offset="100%" stop-color="oklch(0.77 0.125 204)" stop-opacity="0" />
            </linearGradient>
          </defs>
          <path d="{spark} L120,28 L0,28 Z" fill="url(#sparkFill)" stroke="none" />
          <path d={spark} fill="none" stroke="var(--color-accent)" stroke-width="1.4" stroke-linejoin="round" vector-effect="non-scaling-stroke" />
        </svg>
        <div class="mt-1.5 flex justify-between font-mono text-[10px] text-[color:var(--color-faint)]">
          <span>60m ago</span><span>30m</span><span>now</span>
        </div>
      {:else}
        <div class="empty">no traffic in the last hour</div>
      {/if}
    </div>
  </section>

  <section class="panel fade-up p-4" style="--d:160ms">
    <div class="eyebrow">routing &amp; resources</div>
    <div class="grid grid-cols-1 gap-x-4">
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">prefer</span><span class="tnum">{live?.resources ? "see config" : "–"}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">anonymous</span><span class="tnum">{String(live?.resources?.anonymous ?? false)}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">catalog</span><span class="tnum">{live?.resources?.models?.total ?? 0} · {live?.resources?.models?.stale ? "stale" : "fresh"}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">zen keys</span><span class="tnum">{(live?.resources?.keys ?? []).filter((k: any) => k.tier === "zen").length}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">go keys</span><span class="tnum">{(live?.resources?.keys ?? []).filter((k: any) => k.tier === "go").length}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">codex keys</span><span class="tnum">{(live?.resources?.keys ?? []).filter((k: any) => k.tier === "codex").length}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">antigravity keys</span><span class="tnum">{(live?.resources?.keys ?? []).filter((k: any) => k.tier === "antigravity").length}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">proxies</span><span class="tnum">{(live?.resources?.proxies ?? []).length}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">upstream attempts 1h</span><span class="tnum">{num(upstream?.last_hour?.total)}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 border-b border-[color:var(--color-edge)]/60 py-1.5 text-[13px]">
        <span class="dim">usage coverage 1h</span><span class="tnum">{Math.round((usage?.last_hour?.coverage ?? 0) * 100)}%</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 py-1.5 text-[13px]">
        <span class="dim">active / streams</span><span class="tnum">{m?.active_requests ?? 0} / {m?.active_streams ?? 0}</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 py-1.5 text-[13px]">
        <span class="dim">uptime</span><span class="tnum">{ago(Date.now() - (m?.uptime_seconds ?? 0) * 1000).replace(" ago", "") }</span>
      </div>
      <div class="flex items-baseline justify-between gap-3 py-1.5 text-[13px]">
        <span class="dim">started</span><span class="mono max-w-[14rem] truncate text-[12px]" title={started}>{started ? clock(started) : "–"}</span>
      </div>
    </div>
  </section>
</div>

<section class="panel fade-up mt-3 p-4" style="--d:200ms">
  <div class="eyebrow">recent upstream requests</div>
  <div class="overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 20rem">
    <table class="table-cards">
      <thead>
        <tr><th>time</th><th>model</th><th>tier</th><th>channel</th><th class="num">status</th><th class="num">ms</th><th>outcome</th></tr>
      </thead>
      <tbody>
        {#each recent as r (r.request_id + r.time)}
          <tr>
            <td data-label="time" class="mono text-[12px] dim">{clock(r.time)}</td>
            <td data-label="model" class="mono min-w-0 truncate text-[12px]" title={r.model}>{r.model || "–"}</td>
            <td data-label="tier"><span class="pill">{r.tier || (r.anonymous ? "anon" : "–")}</span></td>
            <td data-label="channel" class="mono text-[12px]">{r.channel || "–"}</td>
            <td data-label="status" class="num" class:good={r.status > 0 && r.status < 400} class:bad={r.status >= 400}>{r.status || "–"}</td>
            <td data-label="ms" class="num">{ms(r.duration_ms)}</td>
            <td data-label="outcome" class:good={r.outcome === "success"} class:warn={r.outcome === "stream_error"} class:bad={r.outcome !== "success"}>{r.outcome}</td>
          </tr>
        {/each}
        {#if !recent.length}
          <tr><td colspan="7"><div class="empty">no traffic yet — send a request to /v1/*</div></td></tr>
        {/if}
      </tbody>
    </table>
  </div>
</section>
