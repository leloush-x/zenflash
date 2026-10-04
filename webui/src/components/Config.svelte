<script lang="ts">
  import { onMount } from "svelte";
  import { api, post, put, configToUpdate, PUBLIC_API_BASE } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let config = $state<any>(null);
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
  let manualApiKey = $state("");
  let activeSection = $state("access");
  let accountsLoading = $state(true);
  let accountBusy = $state(false);

  async function loadAccounts() {
    accountsLoading = true;
    try {
      const r = await api<any>("/api/cline/accounts");
      accounts = r?.data?.accounts ?? r?.accounts ?? [];
    } catch { accounts = []; }
    finally { accountsLoading = false; }
  }

  async function removeAccount(id: string) {
    if (!confirm("Remove this Cline account?")) return;
    accountBusy = true;
    try {
      const r = await post<any>("/api/cline/accounts/delete", { accountId: id });
      if (r?.success === false) throw new Error(r.error ?? "Remove failed");
      flash("Cline account removed");
      await loadAccounts();
    } catch (e) { flash(`remove failed: ${e}`); }
    finally { accountBusy = false; }
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
      manualApiKey = "";
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
    accountBusy = true;
    try {
      const r = await post<any>("/api/cline/accounts/add", { refreshToken: refreshToken.trim() });
      if (r?.success) { flash("Cline account connected"); refreshToken = ""; await loadAccounts(); }
      else flash("account rejected: " + (r?.error ?? JSON.stringify(r).slice(0, 120)));
    } catch (e) { flash(`account import failed: ${e}`); }
    finally { accountBusy = false; }
  }

  async function saveManualApiKey() {
    const value = manualApiKey.trim();
    if (!value) { flash("Enter a non-empty API key"); return; }
    try {
      const next = { ...config, server_keys: [{ value }] };
      const r = await put<any>("/api/config", configToUpdate(next));
      if (!r?.config) throw new Error(r?.error?.message ?? "Could not save API key");
      config = r.config;
      apiKeyOnce = value;
      manualApiKey = "";
      flash("Your API key is active. Copy it now; it is shown only once.");
    } catch (e) { flash(String(e)); }
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

  async function saveAccount() {
    const r = await put<any>("/api/account", { current_password: curPw, username: newUser, new_password: newPw });
    if (r?.updated) {
      acctNote = "updated — reauthenticate with the new credentials";
      setTimeout(() => location.reload(), 1200);
    } else acctNote = "rejected: " + JSON.stringify(r?.error ?? r).slice(0, 120);
  }
</script>

<PageHeading section="SETTINGS" title="Settings" description="Manage API access, Cline accounts, and gateway behavior." icon="settings" />

{#if !config && !failed}
  <div class="skel h-64"></div>
{:else if failed}
  <div class="empty">Could not load settings. Check the gateway connection and try again.</div>
{:else}
  <div class="settings-shell fade-up">
    <section class="settings-top">
      <div class="settings-status">
        <span class="settings-orb"></span>
        <div><strong>Gateway configuration</strong><small>Changes validate before they are applied</small></div>
      </div>
      <div class="settings-actions">
        <button class="btn-ghost" onclick={async () => { const r = await post("/api/config/reload"); flash(`Reloaded · ${JSON.stringify(r).slice(0, 80)}`); }}>Reload</button>
        <button class="btn-primary" onclick={saveConfig}>Save changes</button>
      </div>
    </section>
    {#if note}<div class="settings-notice" role="status">{note}</div>{/if}
    {#if config.restart_required_fields?.length}<div class="settings-warning">Restart required for: {config.restart_required_fields.join(", ")}</div>{/if}

    <nav class="settings-tabs" aria-label="Settings sections">
      <button class:current={activeSection === "access"} onclick={() => activeSection = "access"}><span>01</span> API access</button>
      <button class:current={activeSection === "accounts"} onclick={() => activeSection = "accounts"}><span>02</span> Cline accounts <b>{accounts.length}</b></button>
      <button class:current={activeSection === "runtime"} onclick={() => activeSection = "runtime"}><span>03</span> Gateway</button>
    </nav>

    {#if activeSection === "access"}
      <section class="settings-section">
        <div class="section-intro"><div><div class="eyebrow">API ACCESS</div><h2>One key. Every API.</h2><p>Use this key for OpenAI compatible and Anthropic endpoints.</p></div><span class="section-index">01 / 03</span></div>
        <div class="access-grid">
          <article class="settings-card key-card">
            <div class="card-heading"><div><div class="eyebrow">CLIENT CREDENTIAL</div><h3>API key</h3></div><span class="state-pill" class:ready={config.server_keys?.length}>{config.server_keys?.length ? "Active" : "Not configured"}</span></div>
            <div class="key-display"><span class="key-mark">KEY</span><code>{apiKeyOnce || config.server_keys?.[0]?.display || "Create a key to connect your clients"}</code>{#if apiKeyOnce}<button class="icon-action" aria-label="Copy API key" title="Copy" onclick={() => navigator.clipboard.writeText(apiKeyOnce).then(() => flash("API key copied"))}>Copy</button>{/if}</div>
            {#if apiKeyOnce}<p class="key-hint">This is the only time the full key will be shown. Save it in your client now.</p>{:else}<p class="key-hint">Existing keys stay hidden. Replace the key to set a value you can copy.</p>{/if}
            <div class="key-actions"><button class="btn-primary" onclick={rotateApiKey}>{config.server_keys?.length ? "Generate replacement" : "Generate API key"}</button><span>Replaces any previous client key</span></div>
            <div class="manual-key"><label for="manual-api-key">Or choose your own key</label><div class="manual-key-row"><input id="manual-api-key" autocomplete="off" spellcheck="false" placeholder="Enter any non-empty key" bind:value={manualApiKey} onkeydown={(e) => e.key === "Enter" && saveManualApiKey()} /><button class="btn-ghost" disabled={!manualApiKey.trim()} onclick={saveManualApiKey}>Use this key</button></div><small>Any non-empty value works. Fresh installations use <code>free</code> by default.</small></div>
          </article>
          <aside class="settings-card usage-card"><div class="eyebrow">CONNECT YOUR CLIENT</div><h3>Ready for both protocols</h3><p>Send requests to either endpoint with the same bearer key.</p><div class="endpoint"><span>OpenAI compatible</span><code>{PUBLIC_API_BASE}/chat/completions</code></div><div class="endpoint"><span>Anthropic</span><code>{PUBLIC_API_BASE}/messages</code></div><div class="endpoint-note"><span class="settings-orb"></span> Client requests use the API key</div></aside>
        </div>
      </section>
    {:else if activeSection === "accounts"}
      <section class="settings-section">
        <div class="section-intro"><div><div class="eyebrow">ACCOUNT MANAGEMENT</div><h2>Cline accounts</h2><p>Connect accounts, check availability, and remove access you no longer use.</p></div><span class="section-index">02 / 03</span></div>
        <div class="settings-card cline-card">
          <div class="cline-connect">
            <div class="cline-symbol" aria-hidden="true">C</div><div class="cline-copy"><div class="eyebrow">SECURE SIGN IN</div><h3>Connect a Cline account</h3><p>Approve the sign-in in your browser. Your account stays on this gateway.</p></div>
            {#if !cline?.sessionId || cline?.done}<button class="btn-primary" onclick={startCline} disabled={clineBusy}>{clineBusy ? "Starting…" : "Connect with Cline"}</button>{/if}
          </div>
          {#if cline?.sessionId && !cline?.done}
            <div class="oauth-approval"><div><div class="eyebrow">WAITING FOR APPROVAL</div><strong>Enter this code in Cline</strong><p>Keep this page open while you approve the request.</p></div><div class="code-copy"><code>{cline.userCode}</code><button class="btn-ghost" onclick={() => navigator.clipboard.writeText(cline.userCode).then(() => flash("Sign-in code copied"))}>Copy code</button></div><a href={cline.verificationUri} target="_blank" rel="noreferrer">Open Cline verification page ↗</a><button class="text-action" onclick={cancelCline}>Cancel sign-in</button></div>
          {:else if cline?.done}
            <div class="oauth-result" class:success={cline.success}><strong>{cline.success ? `Connected as ${cline.email}` : "Sign-in could not be completed"}</strong>{#if !cline.success}<span>{cline.error}</span>{/if}<button class="text-action" onclick={() => cline = null}>Dismiss</button></div>
          {/if}
        </div>

        <div class="account-list-head"><div><h3>Connected accounts</h3><p>{accounts.length} {accounts.length === 1 ? "account" : "accounts"} available to the gateway</p></div><button class="btn-ghost" onclick={loadAccounts} disabled={accountsLoading}>{accountsLoading ? "Refreshing…" : "Refresh list"}</button></div>
        {#if accountsLoading && !accounts.length}
          <div class="settings-card account-empty">Loading account status…</div>
        {:else if accounts.length}
          <div class="account-list">
            {#each accounts as a (a.accountId)}
              <article class="account-row">
                <div class="account-avatar">{String(a.email || "C").slice(0, 1).toUpperCase()}</div>
                <div class="account-identity"><strong title={a.email}>{a.email}</strong><span>Added account</span></div>
                <span class="account-state" class:ready={a.status === "active"} class:cooling={a.status === "cooldown"} class:expired={a.status === "expired"}><i></i>{a.status || "unknown"}</span>
                <div class="account-usage"><strong>{a.usageCountToday ?? 0}</strong><span>requests today</span></div>
                <div class="account-actions"><button class="btn-ghost" disabled={testingAccount === a.accountId || accountBusy} onclick={() => testAccount(a.accountId)}>{testingAccount === a.accountId ? "Checking…" : "Test account"}</button><button class="remove-action" disabled={accountBusy} onclick={() => removeAccount(a.accountId)}>Remove</button></div>
              </article>
            {/each}
          </div>
        {:else}
          <div class="settings-card account-empty"><div class="empty-mark">C</div><h3>No connected accounts</h3><p>Connect with Cline above to make an account available here.</p></div>
        {/if}

        <details class="manual-import"><summary>Have a refresh token? Import it manually</summary><div class="manual-import-body"><p>Tokens are validated with Cline before an account is added.</p><div class="manual-key-row"><input class="token-field" type="password" autocomplete="off" placeholder="Paste Cline refresh token" bind:value={refreshToken} /><button class="btn-ghost" onclick={addClineToken} disabled={!refreshToken.trim() || accountBusy}>{accountBusy ? "Checking…" : "Validate and add"}</button></div></div></details>
      </section>
    {:else}
      <section class="settings-section">
        <div class="section-intro"><div><div class="eyebrow">GATEWAY CONTROL</div><h2>Runtime settings</h2><p>Adjust routing, model behavior, performance, and dashboard security.</p></div><span class="section-index">03 / 03</span></div>
        <div class="runtime-grid">
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">NETWORK</div><h3>Gateway &amp; routing</h3></div></div><div class="field-grid">
            <label>Preference<select bind:value={config.prefer}><option value="go">Primary</option><option value="zen">Secondary</option></select></label><label>Provider endpoint A<input class="font-mono" bind:value={config.upstream.zen} /></label><label>Provider endpoint B<input class="font-mono" bind:value={config.upstream.go} /></label><label>Proxy file<input class="font-mono" bind:value={config.proxyfile} /></label><label>Upstream access<span class="toggle-field"><input type="checkbox" bind:checked={config.anonymous} /><span>Permit anonymous upstream access</span></span></label>
          </div></article>
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">REQUEST POLICY</div><h3>Retries &amp; models</h3></div></div><div class="field-grid"><label>Retry attempts<input type="number" min="1" bind:value={config.retry.max_attempts} /></label><label>Request timeout (seconds)<input type="number" min="1" bind:value={config.retry.timeout_seconds} /></label><label>Catalog refresh (seconds)<input type="number" min="1" bind:value={config.models.refresh_seconds} /></label><label class="wide-field">Protocol overrides (JSON)<textarea rows="4" class="font-mono" bind:value={protocols}></textarea></label></div></article>
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">CAPACITY</div><h3>Performance</h3></div></div><div class="field-grid"><label>Failure cooldown (seconds)<input type="number" min="1" bind:value={config.performance.failure_cooldown_seconds} /></label><label>Attempt timeout (seconds)<input type="number" min="0" bind:value={config.performance.attempt_timeout_seconds} /></label><label>Connect timeout (seconds)<input type="number" min="1" bind:value={config.performance.connect_timeout_seconds} /></label><label>Max connections per host<input type="number" min="0" bind:value={config.performance.max_conns_per_host} /></label></div></article>
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">MODEL BEHAVIOR</div><h3>Reasoning</h3></div></div><div class="field-grid"><label>Forced effort<input placeholder="Disabled" bind:value={config.reasoning.effort} /></label><label class="wide-field">Effort by model (JSON)<textarea rows="4" class="font-mono" bind:value={effortByModel}></textarea></label></div></article>
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">OPERATIONS</div><h3>Logs &amp; dashboard</h3></div></div><div class="field-grid"><label>Log level<select bind:value={config.logging.level}>{#each ["debug", "info", "warn", "error"] as l (l)}<option value={l}>{l}</option>{/each}</select></label><label>Log ring size<input type="number" min="100" max="50000" bind:value={config.logging.ring_size} /></label><label>Session lifetime (minutes)<input type="number" min="5" max="10080" bind:value={config.webui.session_ttl_minutes} /></label><label>Dashboard enabled<span class="toggle-field"><input type="checkbox" bind:checked={config.webui.enabled} /><span>{config.effective?.webui_enabled ? "Enabled" : "Disabled"}</span></span></label><label>Request body logging<span class="toggle-field"><input type="checkbox" bind:checked={config.logging.dump_request_bodies} /><span>Include bodies in diagnostic logs</span></span></label></div></article>
          <article class="settings-card"><div class="card-heading"><div><div class="eyebrow">ADMIN ACCESS</div><h3>Dashboard account</h3></div></div><div class="field-grid account-fields"><label>Current password<input type="password" autocomplete="current-password" bind:value={curPw} /></label><label>New username<input autocomplete="username" placeholder="Leave blank to keep current" bind:value={newUser} /></label><label>New password<input type="password" autocomplete="new-password" placeholder="Leave blank to keep current" bind:value={newPw} /></label><div class="wide-field account-save"><button class="btn-ghost" onclick={saveAccount}>Update dashboard account</button>{#if acctNote}<span>{acctNote}</span>{/if}</div></div></article>
        </div>
      </section>
    {/if}
  </div>
{/if}

<style>
  .settings-shell { --line: color-mix(in oklab, var(--color-edge) 82%, transparent); max-width: 1180px; margin: 0 auto; }
  .settings-top { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:15px 18px; border:1px solid var(--line); border-radius:16px; background:linear-gradient(110deg,color-mix(in oklab,var(--color-accent) 5%,var(--color-panel)),var(--color-panel) 58%); }
  .settings-status { display:flex; align-items:center; gap:12px; }.settings-status div { display:grid; gap:2px; }.settings-status strong { font-size:13px; }.settings-status small,.key-hint,.key-actions span,.manual-key small,.section-intro p,.usage-card p,.account-list-head p,.cline-copy p,.oauth-approval p,.manual-import-body p { color:var(--color-faint); font-size:12px; }
  .settings-orb { width:9px;height:9px;border-radius:50%;background:var(--color-good);box-shadow:0 0 0 4px color-mix(in oklab,var(--color-good) 14%,transparent);flex:none; }.settings-actions { display:flex;gap:8px; }.settings-notice,.settings-warning { margin-top:9px;padding:10px 13px;border-radius:10px;border:1px solid var(--line);background:var(--color-panel);font-size:12px;color:var(--color-accent); }.settings-warning { color:var(--color-warn); }
  .settings-tabs { display:flex;gap:5px;overflow-x:auto;margin:18px 0 8px;padding:5px;border:1px solid var(--line);border-radius:13px;background:color-mix(in oklab,var(--color-panel) 84%,transparent); }.settings-tabs button { min-height:39px;display:flex;align-items:center;gap:9px;padding:0 13px;border:1px solid transparent;border-radius:9px;background:transparent;color:var(--color-dim);white-space:nowrap;font-size:12px; }.settings-tabs button span { color:var(--color-faint);font:10px var(--font-mono); }.settings-tabs button b { padding:1px 7px;border:1px solid var(--line);border-radius:99px;font:10px var(--font-mono); }.settings-tabs button.current { color:var(--color-ink);border-color:var(--line);background:var(--color-raised); }.settings-tabs button.current span { color:var(--color-accent); }
  .settings-section { padding:16px 0 28px; }.section-intro { display:flex;justify-content:space-between;align-items:end;gap:16px;margin:12px 2px 18px; }.section-intro h2 { margin:4px 0 2px;font-size:clamp(21px,3vw,27px);letter-spacing:-.04em; }.section-intro p { margin:0; }.section-index { color:var(--color-faint);font:11px var(--font-mono);white-space:nowrap;padding-bottom:5px; }.eyebrow { color:var(--color-accent);font-size:9px;font-weight:750;letter-spacing:.14em; }
  .access-grid { display:grid;grid-template-columns:minmax(0,1.35fr) minmax(250px,.65fr);gap:13px; }.settings-card { min-width:0;padding:19px;border:1px solid var(--line);border-radius:15px;background:linear-gradient(145deg,color-mix(in oklab,var(--color-panel) 94%,var(--color-accent) 2%),var(--color-panel));box-shadow:0 12px 32px #0002; }.card-heading { display:flex;align-items:start;justify-content:space-between;gap:12px;margin-bottom:17px; }.card-heading h3,.usage-card h3,.cline-copy h3,.account-list-head h3,.account-empty h3 { margin:3px 0 0;font-size:16px;letter-spacing:-.025em; }.state-pill { padding:4px 9px;border-radius:99px;border:1px solid var(--line);color:var(--color-faint);font-size:10px; }.state-pill.ready { color:var(--color-good);border-color:color-mix(in oklab,var(--color-good) 30%,transparent); }.key-display { display:flex;align-items:center;gap:12px;min-height:58px;padding:11px 13px;border:1px solid var(--line);border-radius:11px;background:color-mix(in oklab,var(--color-bg) 68%,var(--color-panel)); }.key-mark { flex:none;color:var(--color-accent);font:700 9px var(--font-mono);letter-spacing:.12em; }.key-display code { min-width:0;flex:1;overflow-wrap:anywhere;color:var(--color-ink);font:12px var(--font-mono); }.icon-action,.text-action,.remove-action { min-height:30px;padding:4px 9px;border:1px solid var(--line);border-radius:7px;background:transparent;color:var(--color-dim);font-size:11px; }.key-hint { margin:9px 0 0; }.key-actions { display:flex;align-items:center;gap:12px;margin-top:16px; }.key-actions span { font-size:11px; }.manual-key { display:grid;gap:7px;margin-top:20px;padding-top:17px;border-top:1px solid var(--line); }.manual-key label { font-size:12px;font-weight:600; }.manual-key-row { display:flex;gap:8px; }.manual-key-row input { flex:1;min-width:0; }.manual-key small { font-size:10px; }.usage-card { align-self:stretch; }.usage-card h3 { margin-top:8px;font-size:19px; }.usage-card p { margin:6px 0 18px; }.endpoint { display:grid;gap:4px;padding:11px 0;border-top:1px solid var(--line); }.endpoint span { color:var(--color-faint);font-size:10px;text-transform:uppercase;letter-spacing:.08em; }.endpoint code { font:11px var(--font-mono);overflow-wrap:anywhere; }.endpoint-note { display:flex;align-items:center;gap:9px;margin-top:13px;padding:11px;border-radius:9px;background:color-mix(in oklab,var(--color-bg) 58%,transparent);color:var(--color-dim);font-size:11px; }
  .cline-card { padding:0;overflow:hidden; }.cline-connect { display:flex;align-items:center;gap:15px;padding:18px; }.cline-symbol,.account-avatar,.empty-mark { display:grid;place-items:center;flex:none;width:42px;height:42px;border:1px solid color-mix(in oklab,var(--color-accent) 27%,var(--line));border-radius:13px;background:color-mix(in oklab,var(--color-accent) 9%,var(--color-panel));color:var(--color-accent);font-weight:700; }.cline-copy { flex:1;min-width:0; }.cline-copy h3 { margin-top:3px; }.cline-copy p { margin:2px 0 0; }.oauth-approval,.oauth-result { display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px 20px;padding:16px 18px;border-top:1px solid var(--line);background:color-mix(in oklab,var(--color-accent) 4%,var(--color-panel)); }.oauth-approval strong,.oauth-result strong { display:block;margin-top:5px;font-size:13px; }.oauth-approval p { margin:2px 0 0; }.code-copy { display:flex;align-items:center;gap:8px;padding:5px;border:1px solid var(--line);border-radius:10px;background:var(--color-bg); }.code-copy code { padding:5px 9px;font:600 16px var(--font-mono);letter-spacing:.12em; }.oauth-approval>a { grid-column:1/2;font-size:11px; }.oauth-approval .text-action { grid-column:2;grid-row:2; }.oauth-result { grid-template-columns:1fr auto; }.oauth-result span { color:var(--color-bad);font-size:11px; }.oauth-result.success strong { color:var(--color-good); }
  .account-list-head { display:flex;align-items:center;justify-content:space-between;gap:12px;margin:24px 2px 10px; }.account-list-head h3 { font-size:15px; }.account-list-head p { margin:2px 0 0;font-size:11px; }.account-list { display:grid;gap:8px; }.account-row { display:flex;align-items:center;gap:12px;min-width:0;padding:12px;border:1px solid var(--line);border-radius:12px;background:color-mix(in oklab,var(--color-panel) 90%,transparent); }.account-avatar { width:36px;height:36px;border-radius:11px;font-size:12px; }.account-identity { display:grid;gap:3px;flex:1;min-width:0; }.account-identity strong { overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px; }.account-identity span,.account-usage span { color:var(--color-faint);font-size:10px; }.account-state { display:flex;align-items:center;gap:6px;padding:4px 8px;border:1px solid var(--line);border-radius:99px;color:var(--color-faint);font-size:10px;text-transform:capitalize;white-space:nowrap; }.account-state i { width:6px;height:6px;border-radius:50%;background:currentColor; }.account-state.ready { color:var(--color-good); }.account-state.cooling { color:var(--color-warn); }.account-state.expired { color:var(--color-bad); }.account-usage { display:grid;gap:2px;min-width:73px;text-align:right; }.account-usage strong { font:12px var(--font-mono); }.account-actions { display:flex;align-items:center;gap:4px; }.remove-action { color:var(--color-bad); }.account-empty { display:grid;justify-items:center;text-align:center;padding:28px 16px; }.empty-mark { width:38px;height:38px;border-radius:50%; }.account-empty h3 { margin-top:10px; }.account-empty p { margin:4px 0 0;color:var(--color-faint);font-size:12px; }.manual-import { margin-top:13px;border:1px solid var(--line);border-radius:12px;background:color-mix(in oklab,var(--color-panel) 78%,transparent); }.manual-import summary { padding:13px 15px;cursor:pointer;color:var(--color-dim);font-size:12px; }.manual-import-body { padding:0 15px 15px; }.manual-import-body p { margin:0 0 9px; }.token-field { flex:1;min-width:0; }
  .runtime-grid { display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px; }.field-grid { display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:13px 14px; }.field-grid label { display:grid;align-content:start;gap:6px;min-width:0;color:var(--color-faint);font-size:10px;font-weight:600;letter-spacing:.025em; }.field-grid input,.field-grid select,.field-grid textarea { width:100%;font-size:12px; }.field-grid .wide-field { grid-column:1/-1; }.toggle-field { display:flex;align-items:center;gap:9px;min-height:36px;color:var(--color-dim);font-size:11px;font-weight:400;letter-spacing:0; }.account-save { display:flex;align-items:center;gap:10px; }.account-save span { font-size:11px;color:var(--color-accent); }
  @media(max-width:800px) { .access-grid,.runtime-grid { grid-template-columns:1fr; }.usage-card { display:grid;grid-template-columns:1fr 1fr;column-gap:18px; }.usage-card>.eyebrow,.usage-card h3,.usage-card p,.endpoint-note { grid-column:1/-1; }.usage-card p { margin-bottom:8px; }.endpoint { min-width:0; } }
  @media(max-width:600px) { .settings-top { align-items:flex-start;flex-direction:column; }.settings-actions { width:100%; }.settings-actions button { flex:1; }.settings-tabs { margin-top:12px; }.settings-tabs button { flex:1;justify-content:center;padding:0 9px; }.settings-tabs button span { display:none; }.section-intro { align-items:flex-start; }.section-index { font-size:9px; }.access-grid { gap:9px; }.settings-card { padding:15px; }.key-actions { align-items:flex-start;flex-direction:column;gap:6px; }.manual-key-row { flex-direction:column; }.usage-card { grid-template-columns:1fr; }.usage-card>* { grid-column:1/-1!important; }.cline-connect { align-items:flex-start;flex-wrap:wrap; }.cline-copy { width:calc(100% - 58px); }.cline-connect>.btn-primary { width:100%; }.oauth-approval { grid-template-columns:1fr; }.code-copy { width:max-content;max-width:100%; }.oauth-approval>a,.oauth-approval .text-action { grid-column:1;grid-row:auto;justify-self:start; }.account-list-head { align-items:flex-start; }.account-row { display:grid;grid-template-columns:36px minmax(0,1fr) auto;gap:9px; }.account-identity { grid-column:2; }.account-state { grid-column:3;grid-row:1; }.account-usage { grid-column:1/3;grid-row:2;display:flex;align-items:baseline;gap:5px;text-align:left;padding-left:45px; }.account-actions { grid-column:1/-1;justify-content:flex-end;border-top:1px solid var(--line);padding-top:8px; }.field-grid { grid-template-columns:1fr; }.field-grid .wide-field { grid-column:1; }.account-save { align-items:flex-start;flex-direction:column; } }
</style>
