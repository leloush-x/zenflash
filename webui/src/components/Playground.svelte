<script lang="ts">
  import { onMount } from "svelte";
  import { api, post, ms, num } from "../lib";

  let data = $state<any>(null);
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
      const first = (data?.models ?? []).find((m: any) => m.anonymous_eligibility?.allowed);
      if (!model && first) model = first.model;
    } catch {}
  }

  onMount(load);

  const models = $derived((data?.models ?? []).filter((m: any) => m.anonymous_eligibility?.allowed));
  const keys = $derived(data?.keys ?? { zen: [], go: [] });
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
        <select bind:value={keyTier} class="w-auto" aria-label="tier"><option value="zen">zen</option><option value="go">go</option></select>
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
          <option value={m.model}>{m.model} · {m.native_protocol}</option>
        {/each}
      </select>
      <button onclick={run} disabled={busy || !model} class="btn-primary grow px-6 sm:grow-0">
        {busy ? "running…" : "run"}
      </button>
    </div>
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
        {#if out.route}
          <div class="mb-2 flex flex-wrap gap-1 text-[11px]">
            <span class="pill" class:good={out.ok} class:bad={!out.ok}>{out.ok ? "ok" : "http " + out.http_status}</span>
            <span class="pill">{out.route.tier || "–"}</span>
            <span class="pill">{out.route.channel || "–"}</span>
            <span class="pill">{ms(out.duration_ms)}</span>
            {#if out.key_test}<span class="pill" class:good={out.key_test === "usable"} class:bad={out.key_test === "rejected"}>{out.key_test}</span>{/if}
          </div>
        {/if}
        <pre class="max-h-96 overflow-auto">{JSON.stringify(out, null, 2)}</pre>
      {:else}
        <div class="empty min-h-24">
          <div class="text-2xl opacity-40">⌁</div>
          {busy ? "waiting for response…" : "run a request to see output"}
        </div>
      {/if}
    </div>
  </div>
</div>
