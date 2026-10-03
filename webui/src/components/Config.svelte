<script lang="ts">
  import { onMount } from "svelte";
  import { api, post, put, configToUpdate } from "../lib";

  let config = $state<any>(null);
  let revealed = $state(false);
  let revealPw = $state("");
  let note = $state("");
  let failed = $state(false);
  let protocols = $state("{}");
  let effortByModel = $state("{}");

  // account
  let curPw = $state("");
  let newUser = $state("");
  let newPw = $state("");
  let acctNote = $state("");

  onMount(load);

  async function load() {
    failed = false;
    try {
      config = await api("/api/config");
      protocols = JSON.stringify(config.models?.protocols ?? {}, null, 2);
      effortByModel = JSON.stringify(config.reasoning?.effort_by_model ?? {}, null, 2);
    } catch {
      failed = true;
    }
  }

  const flash = (m: string) => {
    note = m;
    setTimeout(() => (note = ""), 5000);
  };

  async function saveConfig() {
    try {
      config.models.protocols = JSON.parse(protocols);
    } catch {
      flash("invalid JSON in models.protocols");
      return;
    }
    try {
      config.reasoning.effort_by_model = JSON.parse(effortByModel);
    } catch {
      flash("invalid JSON in reasoning.effort_by_model");
      return;
    }
    const r = await put<any>("/api/config", configToUpdate(config));
    if (r?.config) {
      flash(`saved · restart fields: ${(r.result?.restart_required ?? []).join(", ") || "none"}`);
      config = r.config;
      protocols = JSON.stringify(config.models?.protocols ?? {}, null, 2);
      effortByModel = JSON.stringify(config.reasoning?.effort_by_model ?? {}, null, 2);
    } else flash("rejected: " + JSON.stringify(r?.error ?? r).slice(0, 160));
  }

  async function reveal() {
    if (revealed) {
      config.server_keys = (config.server_keys ?? []).map((s: any) => s.id ? { id: s.id, display: s.display } : s);
      await load();
      revealed = false;
      revealPw = "";
      return;
    }
    try {
      const r = await post<any>("/api/config/reveal", { password: revealPw });
      if (r?.server_keys) {
        config.server_keys = r.server_keys.map((v: string) => ({ value: v, display: v }));
        config.zen_keys = r.zen_keys.map((v: string) => ({ value: v, display: v }));
        config.go_keys = r.go_keys.map((v: string) => ({ value: v, display: v }));
        config.proxies = r.proxies.map((v: string) => ({ value: v, display: v }));
        revealed = true;
        flash("secrets revealed (save will keep values you did not remove)");
      } else flash("rejected: " + JSON.stringify(r?.error ?? r).slice(0, 120));
    } catch (e) {
      flash(String(e));
    }
  }

  async function saveAccount() {
    const r = await put<any>("/api/account", { current_password: curPw, username: newUser, new_password: newPw });
    if (r?.updated) {
      acctNote = "updated — reauthenticate with the new credentials";
      setTimeout(() => location.reload(), 1200);
    } else acctNote = "rejected: " + JSON.stringify(r?.error ?? r).slice(0, 120);
  }
</script>

{#if !config && !failed}
  <div class="fade-up grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
    {#each Array(3) as _, i (i)}
      <div class="skel h-56"></div>
    {/each}
  </div>
{:else if failed}
  <div class="empty">failed to load config — check the gateway</div>
{:else}
  <section class="panel fade-up p-4">
    <div class="flex flex-wrap items-center gap-2">
      <div class="eyebrow !mb-0">config · validated on save</div>
      <span class="grow"></span>
      <button class="btn-ghost" onclick={saveConfig}>save + apply</button>
      <button class="btn-ghost" onclick={async () => { const r = await post("/api/config/reload"); flash(`reloaded · ${JSON.stringify(r).slice(0, 80)}`); }}>reload from disk</button>
    </div>
    {#if note}<div class="mt-2 text-[12px] text-[color:var(--color-accent)]">{note}</div>{/if}
    {#if config.restart_required_fields?.length}
      <div class="mt-1 text-[12px] warn">restart required for: {config.restart_required_fields.join(", ")}</div>
    {/if}
  </section>

  <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
    <div class="card fade-up">
      <div class="eyebrow">general</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <label class="flex items-center justify-between gap-2">
          <span class="dim">anonymous</span>
          <input type="checkbox" bind:checked={config.anonymous} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">prefer</span>
          <select bind:value={config.prefer} class="w-auto"><option value="go">go</option><option value="zen">zen</option></select>
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">listen</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.listen} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">upstream zen</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.upstream.zen} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">upstream go</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.upstream.go} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">proxyfile</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.proxyfile} />
        </label>
      </div>
    </div>

    <div class="card fade-up" style="--d:50ms">
      <div class="eyebrow">routing &amp; keys</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <label class="flex items-center justify-between gap-2">
          <span class="dim">retry attempts</span>
          <input type="number" min="1" class="w-20 font-mono" bind:value={config.retry.max_attempts} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">timeout s</span>
          <input type="number" min="1" class="w-20 font-mono" bind:value={config.retry.timeout_seconds} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">catalog refresh s</span>
          <input type="number" min="1" class="w-20 font-mono" bind:value={config.models.refresh_seconds} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">models.protocols (json)</span>
          <textarea rows="3" class="w-full rounded-lg bg-[oklch(0.14_0.012_272)] font-mono text-[11px]" bind:value={protocols}></textarea>
        </label>
      </div>
    </div>

    <div class="card fade-up" style="--d:100ms">
      <div class="eyebrow">performance &amp; logging</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <label class="flex items-center justify-between gap-2">
          <span class="dim">cooldown s</span>
          <input type="number" min="1" class="w-20 font-mono" bind:value={config.performance.failure_cooldown_seconds} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">attempt timeout s</span>
          <input type="number" min="0" class="w-20 font-mono" bind:value={config.performance.attempt_timeout_seconds} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">connect timeout s</span>
          <input type="number" min="1" class="w-20 font-mono" bind:value={config.performance.connect_timeout_seconds} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">max conns/host</span>
          <input type="number" min="0" class="w-20 font-mono" bind:value={config.performance.max_conns_per_host} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">log ring</span>
          <input type="number" min="100" max="50000" class="w-24 font-mono" bind:value={config.logging.ring_size} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">log level</span>
          <select bind:value={config.logging.level} class="w-auto">
            {#each ["debug", "info", "warn", "error"] as l (l)}<option value={l}>{l}</option>{/each}
          </select>
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">dump request bodies</span>
          <input type="checkbox" bind:checked={config.logging.dump_request_bodies} />
        </label>
      </div>
    </div>
  </div>

  <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
    <div class="card fade-up" style="--d:140ms">
      <div class="eyebrow">reasoning</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">forced effort</span>
          <input class="w-full font-mono text-[12px]" placeholder="off (leave empty)" bind:value={config.reasoning.effort} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">effort by model (json)</span>
          <textarea rows="3" class="w-full rounded-lg bg-[oklch(0.14_0.012_272)] font-mono text-[11px]" bind:value={effortByModel}></textarea>
        </label>
      </div>
    </div>

    <div class="card fade-up" style="--d:180ms">
      <div class="eyebrow">webui</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <label class="flex items-center justify-between gap-2">
          <span class="dim">enabled</span>
          <input type="checkbox" bind:checked={config.webui.enabled} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">listen</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.webui.listen} />
        </label>
        <label class="flex items-center justify-between gap-2">
          <span class="dim">session ttl min</span>
          <input type="number" min="5" max="10080" class="w-24 font-mono" bind:value={config.webui.session_ttl_minutes} />
        </label>
        <div class="text-[12px] dim">effective: {config.effective?.webui_listen} · {config.effective?.webui_enabled ? "on" : "off"}</div>
      </div>
    </div>
  </div>

  <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
    <div class="card fade-up" style="--d:200ms">
      <div class="eyebrow">keys</div>
      <div class="grid grid-cols-3 gap-2 text-[12px]">
        <div class="rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] p-2.5 text-center">
          <div class="text-lg font-semibold text-[color:var(--color-accent)] tnum">{config.server_keys.length}</div>
          <div class="faint text-[10px] uppercase tracking-wider">server</div>
        </div>
        <div class="rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] p-2.5 text-center">
          <div class="text-lg font-semibold text-[color:var(--color-accent)] tnum">{config.zen_keys.length}</div>
          <div class="faint text-[10px] uppercase tracking-wider">zen</div>
        </div>
        <div class="rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] p-2.5 text-center">
          <div class="text-lg font-semibold text-[color:var(--color-accent)] tnum">{config.go_keys.length}</div>
          <div class="faint text-[10px] uppercase tracking-wider">go</div>
        </div>
      </div>
      {#if revealed}
        <pre class="mt-2 max-h-32 overflow-auto rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.7)] p-2 text-[11px]">{JSON.stringify({ zen: config.zen_keys.map((k: any) => k.display), go: config.go_keys.map((k: any) => k.display), server: config.server_keys.map((k: any) => k.display) }, null, 2)}</pre>
      {:else}
        <div class="mt-2 flex flex-col gap-2">
          <input class="w-full text-[12px]" type="password" placeholder="admin password to reveal" bind:value={revealPw} />
          <button class="btn-ghost w-fit" onclick={reveal}>reveal secrets</button>
        </div>
      {/if}
    </div>

    <div class="card fade-up" style="--d:240ms">
      <div class="eyebrow">account</div>
      <div class="flex flex-col gap-2.5 text-[13px]">
        <input class="w-full text-[12px]" type="password" placeholder="current password" bind:value={curPw} />
        <input class="w-full text-[12px]" placeholder="new username (optional)" bind:value={newUser} />
        <input class="w-full text-[12px]" type="password" placeholder="new password (optional)" bind:value={newPw} />
        <button class="btn-ghost w-fit" onclick={saveAccount}>update account</button>
        {#if acctNote}<p class="text-[12px] text-[color:var(--color-accent)]">{acctNote}</p>{/if}
      </div>
    </div>
  </div>
{/if}
