<script lang="ts">
  import { ago } from "../lib";
  let { live }: { live: any } = $props();
  const res = $derived(live?.resources);
</script>

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
            <tr><td colspan="6"><div class="empty">no keys — anonymous only</div></td></tr>
          {/if}
        </tbody>
      </table>
    </div>
  </section>

  <section class="panel p-4">
    <div class="eyebrow">proxy nodes</div>
    <div class="overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 60vh">
      <table class="table-cards">
        <thead><tr><th class="num">idx</th><th>address</th><th>health</th><th class="num">zen</th><th class="num">go</th><th class="num">anon</th></tr></thead>
        <tbody>
          {#each res?.proxies ?? [] as p (p.index)}
            <tr>
              <td data-label="idx" class="num">{p.index}</td>
              <td data-label="address" class="mono min-w-0 truncate text-[12px]" title={p.address}>{p.address}</td>
              <td data-label="health"><span class="pill" class:good={p.healthy} class:bad={!p.healthy}><span class="dot"></span>{p.healthy ? "healthy" : p.checking ? "checking" : "down"}</span></td>
              <td data-label="zen" class="num">{p.zen_keys}</td>
              <td data-label="go" class="num">{p.go_keys}</td>
              <td data-label="anon" class="num">{p.anonymous ? 1 : 0}</td>
            </tr>
          {/each}
          {#if !(res?.proxies ?? []).length}
            <tr><td colspan="6"><div class="empty">no proxy nodes reported</div></td></tr>
          {/if}
        </tbody>
      </table>
    </div>
  </section>
</div>
