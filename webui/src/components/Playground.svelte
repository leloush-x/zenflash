<script lang="ts">
  import { onMount } from "svelte";
  import { api, post, ms, num, replyText, replyMeta } from "../lib";
  import PageHeading from "./PageHeading.svelte";

  let data = $state<any>(null);
  let catalog = $state<any>(null);
  let protocol = $state("chat");
  let model = $state("");
  let effort = $state("");
  let keyMode = $state("auto");
  let keyTier = $state("zen");
  let keyId = $state("");
  let prompt = $state("Say hi in 5 words.");
  let busy = $state(false);
  let out = $state<any>(null);

  async function load() {
    try {
      data = await api("/api/debug/models");
      catalog = await api("/api/catalog").catch(() => null);
      const list = (data?.models ?? []).filter((m: any) => usable(m) && !m.route_error);
      const first = list.find((m: any) => m.anonymous_eligibility?.allowed) ?? list[0];
      if (!model && first) model = first.model;
    } catch {}
  }

  onMount(load);

  const usable = (m: any) => m.anonymous_eligibility?.allowed || m.available_zen || m.available_go || m.available_codex || m.available_antigravity;
  const models = $derived((data?.models ?? []).filter((m: any) => usable(m) && !m.route_error));
  const providerName = (id: string) => id.startsWith("opencode/") ? "OpenCode" : id.startsWith("cline/") ? "Cline" : id.startsWith("codex/") ? "Codex" : id.startsWith("antigravity/") ? "Antigravity" : "auto";
  const keys = $derived(data?.keys ?? { zen: [], go: [], codex: [], antigravity: [] });
  const catById = $derived(new Map<string, any>((catalog?.data ?? []).map((m: any) => [m.id, m])));
  const selectedCat = $derived(catById.get(model));
  const keyOptions = $derived(keys[keyTier] ?? []);

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
</script>

<PageHeading section="PLAYGROUND" title="API lab" description="Build a request, choose a model, and inspect the gateway response." icon="playground" />

<section class="panel fade-up p-3 sm:p-4">
  <div class="eyebrow">request builder · /api/debug/inference</div>

  <div class="flex flex-col gap-2 lg:flex-row lg:flex-wrap lg:items-center">
    <div class="flex flex-wrap items-center gap-2">
      <select bind:value={protocol} class="w-auto" aria-label="protocol">
        <option value="chat">chat</option>
        <option value="responses">responses</option>
        <option value="anthropic">anthropic</option>
      </select>
      <select bind:value={keyMode} class="w-auto" aria-label="key mode">
        <option value="auto">auto key</option>
        <option value="selected">select key</option>
      </select>
      {#if keyMode === "selected"}
        <select bind:value={keyTier} class="w-auto" aria-label="tier"><option value="zen">zen</option><option value="go">go</option><option value="codex">codex</option><option value="antigravity">antigravity</option></select>
        <select bind:value={keyId} class="w-auto" aria-label="key">
          {#each keyOptions as k (k.id)}<option value={k.id}>{k.display}</option>{/each}
        </select>
      {/if}
      <select bind:value={effort} class="w-auto" title="reasoning effort" aria-label="effort">
        <option value="">effort: default</option>
        {#each ["minimal", "low", "medium", "high", "xhigh", "max"] as lv (lv)}<option value={lv}>{lv}</option>{/each}
      </select>
    </div>

    <div class="flex flex-wrap items-center gap-2 lg:ml-auto">
      <select class="min-w-0 grow sm:min-w-56 lg:grow-0 lg:min-w-64" bind:value={model} aria-label="model">
        {#each models as m (m.model)}
          <option value={m.model}>{providerName(m.model)} · {m.model.replace(/^(opencode|cline|codex|antigravity)\//, "")} · {m.native_protocol}</option>
        {/each}
      </select>
      <button onclick={run} disabled={busy || !model} class="btn-primary grow px-6 sm:grow-0">
        {busy ? "running…" : "run"}
      </button>
    </div>
  </div>

  {#if selectedCat}
    <div class="mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-[color:var(--color-faint)]">
      <span class="pill">{selectedCat.provider ?? "–"}</span>
      <span class="pill">ctx <b class="mono ml-1">{num(selectedCat.context_window)}</b></span>
      <span class="pill">out <b class="mono ml-1">{num(selectedCat.max_output)}</b></span>
      {#if selectedCat.metadata?.reasoning}<span class="pill good">reasoning</span>{/if}
      {#each selectedCat.metadata?.reasoning_efforts ?? [] as lv (lv)}
        <span class="mono rounded-full border border-[color:var(--color-edge)] px-2 py-0.5 text-[10px]">{lv}</span>
      {/each}
    </div>
  {/if}

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
