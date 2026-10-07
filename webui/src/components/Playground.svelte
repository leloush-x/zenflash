<script lang="ts">
  import { onMount } from "svelte";
  import { api, modelSource, num, post, put, rawModelID, replyMeta, replyText, shortModelID, sourceLabel } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let data = $state<any>(null);
  let catalog = $state<any>(null);
  let flags = $state<any[]>([]);
  let protocol = $state("chat");
  let model = $state("");
  let effort = $state("");
  let sourceFilter = $state("all");
  let showDeprecated = $state(false);
  let keyMode = $state("auto");
  let keyTier = $state("zen");
  let keyId = $state("");
  let prompt = $state("Say hi in 5 words.");
  let busy = $state(false);
  let flagBusy = $state(false);
  let notice = $state("");
  let out = $state<any>(null);

  async function load() {
    try {
      const [debugData, catalogData, flagData] = await Promise.all([
        api("/api/debug/models"),
        api<any>("/api/catalog?all=1").catch(() => null),
        api<any>("/api/flags").catch(() => ({ flags: [] })),
      ]);
      data = debugData;
      catalog = catalogData;
      flags = flagData?.flags ?? [];
    } catch (error) {
      notice = String(error);
    }
  }

  onMount(load);

  const flagState = $derived(new Map(flags.map((flag: any) => [rawModelID(flag.id), flag.deprecated === true])));
  const catalogRows = $derived(
    (catalog?.data ?? data?.models ?? []).map((m: any) => {
      const id = m.id ?? m.model ?? "";
      const source = modelSource(m);
      return {
        ...m,
        id,
        model: id,
        source,
        label: shortModelID(id),
        route_protocol: m.route_protocol ?? m.native_protocol,
        _deprecated: flagState.get(rawModelID(id)) ?? m.deprecated === true,
      };
    }),
  );
  const filteredModels = $derived(
    catalogRows.filter((m: any) => {
      if (m.route_protocol === "systemone" || m.native_protocol === "systemone") return false;
      if (!showDeprecated && m._deprecated) return false;
      if (sourceFilter !== "all" && m.source !== sourceFilter) return false;
      return true;
    }),
  );
  const sourceOrder = ["opencode", "cline", "antigravity", "codex", "other"];
  const sourceCounts = $derived(
    Object.fromEntries(
      sourceOrder.map((key) => [
        key,
        catalogRows.filter((m: any) => m.source === key && m.route_protocol !== "systemone" && m.native_protocol !== "systemone" && (showDeprecated || !m._deprecated)).length,
      ]),
    ),
  );
  const selectGroups = $derived(
    (sourceFilter === "all" ? sourceOrder : [sourceFilter])
      .map((key) => ({ key, label: sourceLabel(key), models: filteredModels.filter((m: any) => m.source === key) }))
      .filter((group) => group.models.length),
  );
  const keys = $derived(data?.keys ?? { zen: [], go: [], codex: [], antigravity: [] });
  const selected = $derived(filteredModels.find((m: any) => m.id === model) ?? null);
  const selectedDeprecated = $derived(selected?._deprecated === true);
  const keyOptions = $derived(keys[keyTier] ?? []);

  $effect(() => {
    if (filteredModels.length && !filteredModels.some((m: any) => m.id === model)) {
      model = filteredModels[0].id;
    }
  });

  async function toggleSelected() {
    if (!selected) return;
    flagBusy = true;
    notice = "";
    try {
      const next = !selectedDeprecated;
      const result = await put<any>("/api/flags", { id: rawModelID(selected.id), deprecated: next });
      if (result?.error?.message) throw new Error(result.error.message);
      if (!result?.ok) throw new Error("Postgres is unavailable; set DATABASE_URL to save model switches");
      notice = next ? `${selected.label} moved to deprecated` : `${selected.label} restored`;
      await load();
    } catch (error) {
      notice = String(error);
    } finally {
      flagBusy = false;
    }
  }

  async function run() {
    busy = true;
    out = null;
    try {
      const request: Record<string, any> = { model, messages: [{ role: "user", content: prompt }] };
      if (protocol === "responses") {
        delete request.messages;
        request.input = prompt;
      }
      if (protocol === "anthropic") request.max_tokens = 512;
      if (effort) {
        if (protocol === "chat") request.reasoning_effort = effort;
        else if (protocol === "responses") request.reasoning = { effort };
        else request.output_config = { effort };
      }
      const key: Record<string, any> = { mode: keyMode };
      if (keyMode === "selected") {
        key.tier = keyTier;
        key.id = keyId;
      }
      out = await post("/api/debug/inference", { protocol, key, request });
    } catch (e) {
      out = { error: String(e) };
    } finally {
      busy = false;
    }
  }

  const sources: Array<[string, string]> = [
    ["all", "All"],
    ["opencode", "OpenCode"],
    ["cline", "Cline"],
    ["antigravity", "Antigravity"],
    ["codex", "Codex"],
  ];
</script>

<PageHeading section="PLAYGROUND" title="API lab" description="Filter a source, pick a model, run it, or switch its admin state." icon="playground" />

<section class="panel fade-up p-3 sm:p-4">
  <div class="source-toolbar" class:has-notice={!!notice}>
    <div class="source-filters" aria-label="model source">
      {#each sources as [key, label] (key)}
        <button class="pillbtn" class:on={sourceFilter === key} onclick={() => (sourceFilter = key)}>
          {label}
          {#if key !== "all"}<span>{sourceCounts[key] ?? 0}</span>{/if}
        </button>
      {/each}
    </div>
    <span class="grow"></span>
    <label class="deprecated-filter">
      <input type="checkbox" bind:checked={showDeprecated} />
      include deprecated
    </label>
  </div>

  <div class="flex flex-col gap-2">
    <div class="flex flex-col gap-2 sm:flex-row">
      <select class="min-w-0 grow font-mono text-[13px]" bind:value={model} aria-label="model">
        {#if !filteredModels.length}
          <option value="">no model matches this source</option>
        {/if}
        {#each selectGroups as group (group.key)}
          <optgroup label={group.label}>
            {#each group.models as m (m.id)}
              <option value={m.id}>{m.label} · {m.route_protocol ?? "auto"}{m._deprecated ? " · deprecated" : ""}</option>
            {/each}
          </optgroup>
        {/each}
      </select>
      <select bind:value={protocol} class="w-auto shrink-0" aria-label="protocol">
        <option value="chat">chat</option>
        <option value="responses">responses</option>
        <option value="anthropic">anthropic</option>
      </select>
      <button onclick={run} disabled={busy || !model || selectedDeprecated} class="btn-primary shrink-0 px-6">
        {busy ? "running…" : "run"}
      </button>
    </div>

    {#if selected}
      <div class="selected-model">
        <div class="selected-info">
          <span class="source-pill">{sourceLabel(selected.source)}</span>
          <strong title={selected.id}>{selected.label}</strong>
          <span class="dim">ctx <b class="mono">{num(selected.context_window ?? selected.metadata?.context_window)}</b></span>
          <span class="dim">out <b class="mono">{num(selected.max_output ?? selected.metadata?.max_output)}</b></span>
          {#if selected._deprecated}<span class="pill warn">deprecated</span>{/if}
        </div>
        <button
          class={selectedDeprecated ? "admin-switch restore" : "admin-switch disable"}
          disabled={flagBusy}
          onclick={toggleSelected}
          title={selectedDeprecated ? `Restore ${selected.id}` : `Disable ${selected.id}`}
        >
          <i></i>{flagBusy ? "saving…" : selectedDeprecated ? "enable" : "disable"}
        </button>
      </div>
    {/if}

    {#if notice}
      <p class="notice" class:good={notice.includes("restored")}>{notice}</p>
    {/if}

    <details>
      <summary class="cursor-pointer text-[12px] text-[color:var(--color-faint)]">advanced: key + effort</summary>
      <div class="mt-2 flex flex-wrap items-center gap-2">
        <select bind:value={keyMode} class="w-auto" aria-label="key mode">
          <option value="auto">auto key</option>
          <option value="selected">select key</option>
        </select>
        {#if keyMode === "selected"}
          <select bind:value={keyTier} class="w-auto" aria-label="tier"><option value="zen">opencode</option><option value="go">opencode go</option><option value="codex">codex</option><option value="antigravity">antigravity</option></select>
          <select bind:value={keyId} class="w-auto" aria-label="key">
            {#each keyOptions as k (k.id)}<option value={k.id}>{k.display}</option>{/each}
          </select>
        {/if}
        <select bind:value={effort} class="w-auto" title="reasoning effort" aria-label="effort">
          <option value="">effort: default</option>
          {#each selected?.metadata?.reasoning_efforts?.length ? selected.metadata.reasoning_efforts.map((v: any) => String(v).replace(/"/g, "")) : ["minimal", "low", "medium", "high", "xhigh", "max"] as lv (lv)}<option value={lv}>{lv}</option>{/each}
        </select>
      </div>
    </details>
  </div>

  <p class="mt-2 text-[11px] text-[color:var(--color-faint)]">
    single attempt via the gateway (12/min per IP), no client rotation — use <code>/v1/*</code> directly for streaming.
  </p>
</section>

<div class="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-2">
  <div class="card fade-up flex flex-col" style="--d:60ms">
    <div class="eyebrow">prompt</div>
    <textarea
      rows="8"
      class="min-h-28 w-full grow rounded-lg !bg-[oklch(0.14_0.012_272)] font-mono text-[13px] sm:min-h-32"
      placeholder="Type your prompt…"
      bind:value={prompt}
    ></textarea>
  </div>

  <div class="card fade-up flex flex-col" style="--d:100ms">
    <div class="eyebrow !mb-2">output</div>
    <div class="min-h-32 grow overflow-auto rounded-lg border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.7)] p-3">
      {#if out}
        <div class="mb-2 flex flex-wrap items-center gap-1.5 text-[11px]">
          <span class="pill" class:good={out.ok} class:bad={!out.ok}>{out.ok ? "ok" : "http " + out.http_status}</span>
          {#each replyMeta(out) as m (m.label)}
            <span class="pill faint"><span class="dim mr-1">{m.label}</span>{m.value}</span>
          {/each}
        </div>
        {#if replyText(out.response)}
          <div class="whitespace-pre-wrap break-words text-[13.5px] leading-relaxed">{replyText(out.response)}</div>
        {:else if out.error}
          <div class="text-[13px] bad">{out.error}</div>
        {:else}
          <div class="text-[13px] dim">no visible reply text</div>
        {/if}
        <details class="mt-3">
          <summary class="cursor-pointer text-[11px] text-[color:var(--color-faint)]">raw response</summary>
          <pre class="mt-1 max-h-80 overflow-auto text-[11px]">{JSON.stringify(out, null, 2)}</pre>
        </details>
      {:else}
        <div class="empty min-h-24">
          <div class="text-2xl opacity-40">⌁</div>
          {busy ? "waiting for response…" : "run a request to see output"}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .source-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 9px; margin-bottom: 10px; }
  .source-filters { display: flex; flex-wrap: wrap; gap: 5px; }
  .pillbtn { min-height: 30px; padding: 4px 11px; border-radius: 999px; border: 1px solid var(--color-edge); color: var(--color-dim); font-size: 11px; }
  .pillbtn.on { color: var(--color-ink); border-color: var(--color-accent); background: color-mix(in oklab, var(--color-accent) 9%, var(--color-raised)); }
  .pillbtn span { margin-left: 5px; color: var(--color-faint); font: 9px var(--font-mono); }
  .deprecated-filter { display: flex; align-items: center; gap: 7px; color: var(--color-dim); font-size: 11px; }
  .deprecated-filter input { width: 15px; height: 15px; }

  .selected-model {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 42px;
    padding: 7px 9px;
    border: 1px solid var(--color-edge);
    border-radius: 11px;
    background: oklch(0.115 0.011 272 / .7);
  }
  .selected-info { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; min-width: 0; }
  .selected-info strong { max-width: min(48vw, 360px); overflow: hidden; font: 600 12px var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }
  .selected-info > .dim { font-size: 10px; }
  .source-pill {
    padding: 2px 7px;
    border: 1px solid color-mix(in oklab, var(--color-accent) 30%, var(--color-edge));
    border-radius: 999px;
    color: var(--color-accent);
    background: color-mix(in oklab, var(--color-accent) 8%, transparent);
    font-size: 8.5px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: .05em;
  }
  .admin-switch {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
    min-height: 27px;
    padding: 3px 9px;
    border: 1px solid var(--color-edge);
    border-radius: 999px;
    background: transparent;
    color: var(--color-dim);
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: .04em;
  }
  .admin-switch i { width: 7px; height: 7px; border-radius: 50%; background: var(--color-warn); }
  .admin-switch.restore { color: var(--color-good); border-color: color-mix(in oklab, var(--color-good) 35%, var(--color-edge)); }
  .admin-switch.restore i { background: var(--color-good); box-shadow: 0 0 7px color-mix(in oklab, var(--color-good) 55%, transparent); }
  .admin-switch:disabled { opacity: .55; cursor: wait; }
  .notice { margin: 0; color: var(--color-warn); font-size: 11px; }
  .notice.good { color: var(--color-good); }

  @media (max-width: 680px) {
    .source-toolbar { align-items: flex-start; }
    .selected-model { align-items: flex-start; }
    .selected-info strong { max-width: 100%; }
  }
</style>
