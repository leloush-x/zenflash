<script lang="ts">
  import { onMount } from "svelte";
  import { api, post, put, configToUpdate } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let config = $state<any>(null);
  let revealed = $state(false);
  let revealPw = $state("");
  let note = $state("");
  let failed = $state(false);
  let protocols = $state("{}");
  let effortByModel = $state("{}");

  // cline oauth
  let cline = $state<any>(null);
  let clineBusy = $state(false);
  let accounts = $state<any[]>([]);
  let refreshToken = $state("");
  let testingAccount = $state("");
  let apiKeyOnce = $state("");

  async function loadAccounts() {
    try {
      const r = await api<any>("/api/cline/accounts");
      accounts = r?.data?.accounts ?? r?.accounts ?? [];
    } catch { accounts = []; }
  }

  async function removeAccount(id: string) {
    await post("/api/cline/accounts/delete", { accountId: id });
    await loadAccounts();
  }

  async function testAccount(id: string) {
    testingAccount = id;
    try {
      const r = await post<any>("/api/cline/accounts/test", { accountId: id });
      flash(r?.success ? "Cline account is ready" : `account check: ${r?.message ?? r?.error ?? "failed"}`);
      await loadAccounts();
    } catch (e) { flash(`account check failed: ${e}`); }
    finally { testingAccount = ""; }
  }

  async function rotateApiKey() {
    try {
      const bytes = crypto.getRandomValues(new Uint8Array(32));
      const value = `zf_${Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")}`;
      const next = { ...config, server_keys: [{ value }] };
      const r = await put<any>("/api/config", configToUpdate(next));
      if (!r?.config) throw new Error(r?.error?.message ?? "Could not update API key");
      config = r.config;
      apiKeyOnce = value;
      flash("API key replaced. Copy it now; it is shown only once.");
    } catch (e) { flash(String(e)); }
  }

  onMount(() => { load(); loadAccounts(); });
  let clineTimer: any;
  async function startCline() {
    clineBusy = true;
    try {
      const r = await post<any>("/api/cline/oauth/start");
      const d = r?.data ?? r;
      if (r?.success && d?.sessionId) {
        cline = { sessionId: d.sessionId, verificationUri: d.verificationUri, userCode: d.userCode };
        clineTimer = setInterval(pollCline, 2000);
      } else note = "cline login start failed: " + (r?.error ?? JSON.stringify(r).slice(0, 100));
    } catch (e) { note = String(e); }
    finally { clineBusy = false; }
  }
  async function pollCline() {
    try {
      const r = await api<any>(`/api/cline/oauth/status?sessionId=${cline.sessionId}`);
      const d = r?.data ?? r;
      if (d?.done) { cline = { ...cline, done: true, success: d.success, email: d.email, error: d.error }; clearInterval(clineTimer); }
    } catch { /* keep polling */ }
  }
  function cancelCline() { clearInterval(clineTimer); cline = null; }

  // account
  let curPw = $state("");
  let newUser = $state("");
  let newPw = $state("");
  let acctNote = $state("");

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

  async function addClineToken() {
    if (!refreshToken.trim()) return;
    const r = await post<any>("/api/cline/accounts/add", { refreshToken: refreshToken.trim() });
    if (r?.success) { flash("cline account added"); refreshToken = ""; await loadAccounts(); }
    else flash("add failed: " + (r?.error ?? JSON.stringify(r).slice(0, 120)));
  }

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

<PageHeading section="SETTINGS" title="Gateway settings" description="One API key for your clients, account connections, and gateway behavior." icon="settings" />

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
          <span class="dim">routing preference</span>
          <select bind:value={config.prefer} class="w-auto"><option value="go">primary</option><option value="zen">secondary</option></select>
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">listen</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.listen} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">provider endpoint A</span>
          <input class="w-full font-mono text-[12px]" bind:value={config.upstream.zen} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">provider endpoint B</span>
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

  <div class="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-[0.8fr_1.2fr]">
    <div class="card fade-up" style="--d:200ms">
      <div class="eyebrow">API access</div>
      <h2 class="text-lg font-semibold">One key. Every API.</h2>
      <p class="mt-1 text-[12px] dim">Use the same key with OpenAI compatible and Anthropic endpoints.</p>
      <div class="mt-4 rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] p-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-[12px] dim">Client API key</span>
          <span class="pill" class:good={config.server_keys?.length}> {config.server_keys?.length ? "configured" : "not set"}</span>
        </div>
        <div class="mt-2 font-mono text-[12px]">{apiKeyOnce || config.server_keys?.[0]?.display || "No key configured"}</div>
      </div>
      {#if apiKeyOnce}<button class="btn-ghost mt-3" onclick={() => navigator.clipboard.writeText(apiKeyOnce).then(() => flash("API key copied"))}>copy key</button>{/if}
      <button class="btn-ghost mt-3" onclick={rotateApiKey}>{config.server_keys?.length ? "replace API key" : "create API key"}</button>
      <p class="mt-3 text-[11px] faint">Replacing the key disconnects clients still using the previous value.</p>
    </div>

  <div class="card fade-up" style="--d:260ms">
    <div class="eyebrow">Cline accounts</div>
    <h2 class="text-lg font-semibold">Connected accounts</h2>
    <p class="text-[12px] text-[color:var(--color-faint)]">Connect, verify, and manage accounts used by the Cline provider.</p>
    {#if accounts.length}
      <div class="mt-3 flex flex-col gap-1.5">
        {#each accounts as a (a.accountId)}
          <div class="flex items-center gap-2 rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.6)] px-3 py-2 text-[12px]">
            <span class="min-w-0 grow truncate mono" title={a.email}>{a.email}</span>
            <span class="pill" class:good={a.status === "active"} class:warn={a.status === "cooldown"} class:bad={a.status === "expired"}>{a.status}</span>
            <span class="pill faint">today {a.usageCountToday ?? 0} req</span>
            <button class="btn-ghost !px-2 !py-0.5 text-[11px]" disabled={testingAccount === a.accountId} onclick={() => testAccount(a.accountId)}>{testingAccount === a.accountId ? "checking…" : "test"}</button>
            <button class="btn-danger !px-2 !py-0.5 text-[11px]" onclick={() => removeAccount(a.accountId)} title="remove">×</button>
          </div>
        {/each}
      </div>
    {/if}
    <div class="mt-4 flex flex-col gap-1.5">
      <span class="text-[11px] uppercase tracking-wider text-[color:var(--color-faint)]">manual token import</span>
      <div class="flex flex-col gap-2 sm:flex-row">
        <input class="grow font-mono text-[12px]" placeholder="Cline refresh token" bind:value={refreshToken} />
        <button class="btn-ghost shrink-0" onclick={addClineToken}>add</button>
      </div>
    </div>
    {#if cline?.done}
      <p class="mt-2 text-[12px] good">{cline.success ? `logged in as ${cline.email}` : `login failed: ${cline.error}`}</p>
      <button class="btn-ghost mt-2 w-fit" onclick={startCline}>login again</button>
    {:else if cline?.sessionId}
      <div class="mt-2 flex flex-col gap-1.5 text-[12px]">
        <a class="text-[color:var(--color-accent)] underline" href={cline.verificationUri} target="_blank" rel="noreferrer">open verification page</a>
        <div>user code: <span class="mono">{cline.userCode}</span></div>
        <div class="faint">waiting for browser approval…</div>
      </div>
      <button class="btn-ghost mt-2 w-fit" onclick={cancelCline}>cancel</button>
    {:else}
      <button class="btn-ghost mt-2 w-fit" onclick={startCline} disabled={clineBusy}>{clineBusy ? "starting…" : "sign in with Cline"}</button>
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
