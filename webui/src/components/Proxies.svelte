<script lang="ts">
  import { onMount } from "svelte";
  import { api, put, configToUpdate } from "../lib";
  import { ago } from "../lib";

  let config = $state<any>(null);
  let nodes = $state<any[]>([]);
  let addProxy = $state("");
  let note = $state("");
  let failed = $state(false);

  async function load() {
    failed = false;
    try {
      config = await api("/api/config");
      const m = await api<any>("/api/monitor");
      nodes = m?.resources?.proxies ?? [];
    } catch {
      failed = true;
    }
  }

  onMount(load);

  const flash = (m: string) => {
    note = m;
    setTimeout(() => (note = ""), 4000);
  };

  async function saveProxies(proxies: any[]) {
    const update = configToUpdate(config);
    update.proxies = proxies;
    const r = await put<any>("/api/config", update);
    if (r?.config) {
      config = r.config;
      flash(`saved · restart fields: ${(r.result?.restart_required ?? []).join(",") || "none"}`);
    } else {
      flash("rejected: " + JSON.stringify(r?.error ?? r).slice(0, 120));
    }
  }

  const add = () => {
    if (!addProxy.trim()) return;
    saveProxies([...(config.proxies ?? []).map((p: any) => ({ id: p.id })), { value: addProxy.trim() }]).then(
      () => (addProxy = ""),
    );
  };
  const remove = (idx: number) =>
    saveProxies((config.proxies ?? []).filter((_: any, i: number) => i !== idx).map((p: any) => ({ id: p.id })));
</script>

<section class="panel fade-up p-4">
  <div class="flex flex-wrap items-center gap-2">
    <span class="pill">configured {config?.proxies?.length ?? 0}</span>
    <span class="pill">nodes healthy {nodes.filter((n) => n.healthy).length}/{nodes.length}</span>
    <span class="grow"></span>
    <button class="btn-ghost" onclick={load}>refresh</button>
  </div>

  <div class="mt-3 flex flex-col gap-2 sm:flex-row">
    <input class="grow" placeholder="http://user:pass@host:port or socks5://host:port or direct" bind:value={addProxy} />
    <button class="btn-primary shrink-0" onclick={add}>add</button>
  </div>

  {#if note}<p class="mt-2 text-[12px] text-[color:var(--color-accent)]">{note}</p>{/if}
  {#if failed}<p class="mt-2 text-[12px] bad">failed to load proxy state</p>{/if}

  <div class="mt-3 overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 40vh">
    <table class="table-cards">
      <thead><tr><th>proxy</th><th></th></tr></thead>
      <tbody>
        {#each config?.proxies ?? [] as p, i (p.id)}
          <tr>
            <td data-label="proxy" class="mono min-w-0 break-all text-[12px]">{p.display}</td>
            <td data-label="" class="num">
              <button class="btn-danger !px-2.5 !py-1 text-[11px]" onclick={() => remove(i)}>del</button>
            </td>
          </tr>
        {/each}
        {#if !(config?.proxies ?? []).length}
          <tr><td colspan="2"><div class="empty">no proxies configured — direct routing</div></td></tr>
        {/if}
      </tbody>
    </table>
  </div>

  <div class="eyebrow mt-4">live proxy nodes</div>
  <div class="overflow-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.45)]" style="max-height: 40vh">
    <table class="table-cards">
      <thead><tr><th>address</th><th>health</th><th class="num">zen</th><th class="num">go</th></tr></thead>
      <tbody>
        {#each nodes as n (n.index)}
          <tr>
            <td data-label="address" class="mono min-w-0 truncate text-[12px]">{n.address}</td>
            <td data-label="health"><span class="pill" class:good={n.healthy} class:bad={!n.healthy}><span class="dot"></span>{n.healthy ? "healthy" : n.checking ? "checking" : "down"}</span></td>
            <td data-label="zen" class="num">{n.zen_keys}</td>
            <td data-label="go" class="num">{n.go_keys}</td>
          </tr>
        {/each}
        {#if !nodes.length}
          <tr><td colspan="4"><div class="empty">no proxy nodes reported</div></td></tr>
        {/if}
      </tbody>
    </table>
  </div>
</section>
