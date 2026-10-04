<script lang="ts">
  import Icon from "./Icon.svelte";

  let { hasKey, apiBase, onSettings, onCreateKey }: { hasKey: boolean; apiBase: string; onSettings: () => void; onCreateKey: () => Promise<string> } = $props();
  let copied = $state("");
  let newKey = $state("");
  let creating = $state(false);
  let keyError = $state("");
  let protocol = $state<"openai" | "anthropic">("openai");
  const code = $derived((protocol === "openai"
    ? [`curl ${apiBase.replace(/\/$/, "")}/chat/completions \\`, `  -H "Authorization: Bearer $ZENFLASH_API_KEY" \\`, `  -H "Content-Type: application/json" \\`, `  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'`]
    : [`curl ${apiBase.replace(/\/$/, "")}/messages \\`, `  -H "x-api-key: $ZENFLASH_API_KEY" \\`, `  -H "anthropic-version: 2023-06-01" \\`, `  -H "content-type: application/json" \\`, `  -d '{"model":"YOUR_MODEL_ID","max_tokens":128,"messages":[{"role":"user","content":"Hello"}]}'`]).join("\n"));

  async function copy(value: string, label: string) {
    try { await navigator.clipboard.writeText(value); copied = label; setTimeout(() => copied = "", 1600); }
    catch { copied = "Clipboard unavailable"; setTimeout(() => copied = "", 1800); }
  }
  async function createKey() {
    creating = true;
    keyError = "";
    try { newKey = await onCreateKey(); }
    catch (e) { keyError = String(e); }
    finally { creating = false; }
  }
</script>

<section class="quick-connect card fade-up" aria-labelledby="connect-title">
  <div class="connect-main">
    <div class="connect-heading">
      <span class="connect-icon"><Icon name="bolt" size={19}/></span>
      <div><div class="eyebrow">ONE KEY · TWO API STYLES</div><h2 id="connect-title">Connect your app</h2></div>
    </div>
    <p class="connect-desc">Use the same server key with OpenAI-compatible and Anthropic requests.</p>
    <div class="connect-meta">
      <div class="connect-value"><span>BASE URL</span><code>{apiBase || "loading…"}</code><button class="copy-btn" onclick={() => copy(apiBase, "base")} aria-label="Copy base URL"><Icon name={copied === "base" ? "check" : "copy"} size={15}/></button></div>
      <span class="key-status" class:configured={hasKey || !!newKey}><span class="status-dot" class:online={hasKey || !!newKey}></span>{hasKey || newKey ? "API key configured" : "No server key yet"}</span>
    </div>
    {#if newKey}
      <div class="new-key"><span>NEW KEY · COPY NOW</span><code>{newKey}</code><button class="copy-btn" onclick={() => copy(newKey, "key")} aria-label="Copy new API key"><Icon name={copied === "key" ? "check" : "copy"} size={15}/></button></div>
      <p class="key-once-note">This value is shown once. Store it in <code>ZENFLASH_API_KEY</code>.</p>
    {:else if !hasKey}
      <button class="inline-link" onclick={createKey} disabled={creating}>{creating ? "Creating key…" : "Create shared API key"} <Icon name="arrow" size={15}/></button>
      {#if keyError}<p class="key-error">{keyError} · <button class="inline-link" onclick={onSettings}>open Settings</button></p>{/if}
    {:else}
      <button class="inline-link" onclick={onSettings}>Manage server keys in Settings <Icon name="arrow" size={15}/></button>
    {/if}
  </div>
  <div class="connect-code">
    <div class="code-tabs"><div class="protocol-tabs" role="tablist" aria-label="API example">
      <button class:chosen={protocol === "openai"} role="tab" aria-selected={protocol === "openai"} onclick={() => protocol = "openai"}>OpenAI</button>
      <button class:chosen={protocol === "anthropic"} role="tab" aria-selected={protocol === "anthropic"} onclick={() => protocol = "anthropic"}>Anthropic</button>
    </div><button class="copy-btn code-copy" onclick={() => copy(code, "code")} aria-label="Copy example"><Icon name={copied === "code" ? "check" : "copy"} size={15}/><span>{copied === "code" ? "Copied" : "Copy"}</span></button></div>
    <pre><code>{code}</code></pre>
    <div class="code-foot">Set <code>ZENFLASH_API_KEY</code> to your server key. Keep it private.</div>
  </div>
</section>
